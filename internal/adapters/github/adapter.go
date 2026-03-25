package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/retry"
)

const (
	defaultBaseURL = "https://api.github.com"
	defaultTimeout = 30 * time.Second
	botLabelName   = "autodoc-bot"
	ghAPIVersion   = "2022-11-28"
	branchPrefix   = "bot/docs-update/"
)

// Adapter implements domain.RepositoryPort and domain.MRCreatorPort via the GitHub REST API.
// All operations are performed through the API — no local clone required.
type Adapter struct {
	http      *http.Client
	base      string // https://api.github.com
	owner     string
	repo      string
	branch    string
	token     string
	retryOpts retry.Options
	gitCfg    config.GitConfig
	log       *slog.Logger
}

// New constructs a GitHub Adapter.
// cfg.ProjectID must be in "owner/repo" format.
// cfg.URL overrides the default GitHub API base URL (useful for GitHub Enterprise).
func New(cfg config.RepositoryConfig, gitCfg config.GitConfig, log *slog.Logger) *Adapter {
	owner, repo, _ := strings.Cut(cfg.ProjectID, "/")
	base := defaultBaseURL
	if cfg.URL != "" {
		base = strings.TrimRight(cfg.URL, "/")
	}
	return &Adapter{
		http:      &http.Client{Timeout: defaultTimeout},
		base:      base,
		owner:     owner,
		repo:      repo,
		branch:    cfg.DefaultBranch,
		token:     cfg.Token,
		retryOpts: retry.Options{MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay},
		gitCfg:    gitCfg,
		log:       log,
	}
}

// ---------------------------------------------------------------------------
// domain.RepositoryPort
// ---------------------------------------------------------------------------

// Fetch verifies that the repository is reachable and the token is valid.
func (a *Adapter) Fetch(ctx context.Context) error {
	var repo struct {
		FullName string `json:"full_name"`
	}
	if err := a.get(ctx, a.repoPath(""), nil, &repo); err != nil {
		return fmt.Errorf("github fetch: %w", err)
	}
	a.log.InfoContext(ctx, "github: repository reachable", slog.String("repo", repo.FullName))
	return nil
}

// HeadSHA returns the HEAD commit SHA of the default branch.
func (a *Adapter) HeadSHA(ctx context.Context) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	path := fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", a.owner, a.repo, url.PathEscape(a.branch))
	if err := a.get(ctx, path, nil, &ref); err != nil {
		return "", fmt.Errorf("github head sha: %w", err)
	}
	return ref.Object.SHA, nil
}

// Diff returns file diffs between fromSHA and toSHA using the GitHub compare API.
func (a *Adapter) Diff(ctx context.Context, fromSHA, toSHA string) ([]domain.FileDiff, error) {
	var result struct {
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Status           string `json:"status"` // added, modified, removed, renamed, copied
			Patch            string `json:"patch"`
		} `json:"files"`
	}

	path := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", a.owner, a.repo, fromSHA, toSHA)
	if err := a.get(ctx, path, nil, &result); err != nil {
		return nil, fmt.Errorf("github diff: %w", err)
	}

	diffs := make([]domain.FileDiff, 0, len(result.Files))
	for _, f := range result.Files {
		diffs = append(diffs, domain.FileDiff{
			Path:    f.Filename,
			OldPath: f.PreviousFilename,
			Status:  githubChangeStatus(f.Status),
			Patch:   f.Patch,
		})
	}
	a.log.InfoContext(ctx, "github: diff retrieved",
		slog.Int("files", len(diffs)),
		slog.String("from", fromSHA[:min(len(fromSHA), 8)]),
		slog.String("to", toSHA[:min(len(toSHA), 8)]),
	)
	return diffs, nil
}

// ---------------------------------------------------------------------------
// domain.MRCreatorPort
// ---------------------------------------------------------------------------

// CreateBranch creates a new branch pointing at the current HEAD of the default branch.
func (a *Adapter) CreateBranch(ctx context.Context, name string) error {
	headSHA, err := a.HeadSHA(ctx)
	if err != nil {
		return fmt.Errorf("github create branch: get head: %w", err)
	}

	body := map[string]string{
		"ref": "refs/heads/" + name,
		"sha": headSHA,
	}
	path := fmt.Sprintf("/repos/%s/%s/git/refs", a.owner, a.repo)
	if err := a.post(ctx, path, body, nil); err != nil {
		return fmt.Errorf("github create branch %q: %w", name, err)
	}
	a.log.InfoContext(ctx, "github: branch created", slog.String("branch", name))
	return nil
}

// CommitFiles commits the provided documents to branch in a single commit.
// It uses the GitHub Git Data API: blobs → tree → commit → ref update.
func (a *Adapter) CommitFiles(ctx context.Context, branch string, docs []domain.Document, message string) error {
	// 1. Resolve current branch ref → commit SHA.
	var ref struct {
		Object struct{ SHA string `json:"sha"` } `json:"object"`
	}
	refPath := fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", a.owner, a.repo, url.PathEscape(branch))
	if err := a.get(ctx, refPath, nil, &ref); err != nil {
		return fmt.Errorf("github commit: get ref: %w", err)
	}
	commitSHA := ref.Object.SHA

	// 2. Get the tree SHA from the commit.
	var commit struct {
		Tree struct{ SHA string `json:"sha"` } `json:"tree"`
	}
	commitPath := fmt.Sprintf("/repos/%s/%s/git/commits/%s", a.owner, a.repo, commitSHA)
	if err := a.get(ctx, commitPath, nil, &commit); err != nil {
		return fmt.Errorf("github commit: get commit: %w", err)
	}
	baseTreeSHA := commit.Tree.SHA

	// 3. Create a blob for each document.
	type treeEntry struct {
		Path string `json:"path"`
		Mode string `json:"mode"` // "100644" = regular file
		Type string `json:"type"` // "blob"
		SHA  string `json:"sha"`
	}

	blobPath := fmt.Sprintf("/repos/%s/%s/git/blobs", a.owner, a.repo)
	entries := make([]treeEntry, 0, len(docs))
	for _, doc := range docs {
		var blob struct{ SHA string `json:"sha"` }
		if err := a.post(ctx, blobPath, map[string]string{
			"content":  doc.Content,
			"encoding": "utf-8",
		}, &blob); err != nil {
			return fmt.Errorf("github commit: create blob %q: %w", doc.Path, err)
		}
		entries = append(entries, treeEntry{
			Path: doc.Path,
			Mode: "100644",
			Type: "blob",
			SHA:  blob.SHA,
		})
	}

	// 4. Create a new tree on top of the base tree.
	var newTree struct{ SHA string `json:"sha"` }
	treePath := fmt.Sprintf("/repos/%s/%s/git/trees", a.owner, a.repo)
	if err := a.post(ctx, treePath, map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	}, &newTree); err != nil {
		return fmt.Errorf("github commit: create tree: %w", err)
	}

	// 5. Create the commit.
	var newCommit struct{ SHA string `json:"sha"` }
	newCommitPath := fmt.Sprintf("/repos/%s/%s/git/commits", a.owner, a.repo)
	if err := a.post(ctx, newCommitPath, map[string]any{
		"message": message,
		"tree":    newTree.SHA,
		"parents": []string{commitSHA},
	}, &newCommit); err != nil {
		return fmt.Errorf("github commit: create commit: %w", err)
	}

	// 6. Update the branch ref to point to the new commit.
	if err := a.patch(ctx, refPath, map[string]any{
		"sha":   newCommit.SHA,
		"force": false,
	}, nil); err != nil {
		return fmt.Errorf("github commit: update ref: %w", err)
	}

	a.log.InfoContext(ctx, "github: files committed",
		slog.String("branch", branch),
		slog.Int("files", len(docs)),
		slog.String("sha", newCommit.SHA[:min(len(newCommit.SHA), 8)]),
	)
	return nil
}

// CreateMR opens a pull request and returns its number as a string.
func (a *Adapter) CreateMR(ctx context.Context, mr domain.MergeRequest) (domain.MergeRequest, error) {
	body := map[string]any{
		"title": mr.Title,
		"body":  mr.Description,
		"head":  mr.Branch,
		"base":  a.branch,
	}

	var resp struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", a.owner, a.repo)
	if err := a.post(ctx, path, body, &resp); err != nil {
		return domain.MergeRequest{}, fmt.Errorf("github create pr: %w", err)
	}

	// Apply the bot label to the newly opened PR.
	_ = a.addLabel(ctx, resp.Number)

	created := domain.MergeRequest{
		ID:  fmt.Sprintf("%d", resp.Number),
		URL: resp.HTMLURL,
	}
	a.log.InfoContext(ctx, "github: PR created",
		slog.Int("number", resp.Number),
		slog.String("url", resp.HTMLURL),
	)
	return created, nil
}

// UpdateMR updates the title and body of an existing pull request.
// id is the PR number as a string.
func (a *Adapter) UpdateMR(ctx context.Context, id string, mr domain.MergeRequest) error {
	body := map[string]any{}
	if mr.Title != "" {
		body["title"] = mr.Title
	}
	if mr.Description != "" {
		body["body"] = mr.Description
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%s", a.owner, a.repo, id)
	if err := a.patch(ctx, path, body, nil); err != nil {
		return fmt.Errorf("github update pr %s: %w", id, err)
	}
	a.log.InfoContext(ctx, "github: PR updated", slog.String("number", id))
	return nil
}

// OpenBotMRs returns all open pull requests created by the bot
// (identified by the bot branch prefix).
func (a *Adapter) OpenBotMRs(ctx context.Context) ([]domain.MergeRequest, error) {
	var pulls []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
		HTMLURL string `json:"html_url"`
	}

	q := url.Values{"state": {"open"}, "per_page": {"100"}}
	path := fmt.Sprintf("/repos/%s/%s/pulls", a.owner, a.repo)
	if err := a.get(ctx, path, q, &pulls); err != nil {
		return nil, fmt.Errorf("github open bot prs: %w", err)
	}

	mrs := make([]domain.MergeRequest, 0)
	for _, p := range pulls {
		if !strings.HasPrefix(p.Head.Ref, branchPrefix) {
			continue
		}
		mrs = append(mrs, domain.MergeRequest{
			ID:          fmt.Sprintf("%d", p.Number),
			Title:       p.Title,
			Description: p.Body,
			Branch:      p.Head.Ref,
			URL:         p.HTMLURL,
		})
	}
	return mrs, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (a *Adapter) repoPath(suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", a.owner, a.repo, suffix)
}

func (a *Adapter) get(ctx context.Context, path string, query url.Values, out any) error {
	u := a.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	makeReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		a.setHeaders(req)
		return req, nil
	}
	return a.do(ctx, makeReq, out)
}

func (a *Adapter) post(ctx context.Context, path string, body, out any) error {
	return a.sendJSON(ctx, http.MethodPost, path, body, out)
}

func (a *Adapter) patch(ctx context.Context, path string, body, out any) error {
	return a.sendJSON(ctx, http.MethodPatch, path, body, out)
}

func (a *Adapter) sendJSON(ctx context.Context, method, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	makeReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, a.base+path, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		a.setHeaders(req)
		return req, nil
	}
	return a.do(ctx, makeReq, out)
}

func (a *Adapter) do(ctx context.Context, makeReq func() (*http.Request, error), out any) error {
	resp, err := retry.Do(ctx, a.http, a.retryOpts, makeReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("github api: status %d: %s", resp.StatusCode, b)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// setHeaders applies authentication and API version headers to a request.
func (a *Adapter) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", ghAPIVersion)
}

// addLabel adds the bot label to a pull request (best-effort, errors are ignored).
func (a *Adapter) addLabel(ctx context.Context, prNumber int) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", a.owner, a.repo, prNumber)
	return a.post(ctx, path, map[string]any{"labels": []string{botLabelName}}, nil)
}

func githubChangeStatus(s string) domain.ChangeStatus {
	switch s {
	case "added":
		return domain.ChangeStatusAdded
	case "removed":
		return domain.ChangeStatusDeleted
	case "renamed":
		return domain.ChangeStatusRenamed
	default:
		return domain.ChangeStatusModified
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
