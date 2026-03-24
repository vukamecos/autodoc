package gitlab

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
)

const (
	botLabel       = "autodoc-bot"
	defaultTimeout = 30 * time.Second
)

// Adapter implements domain.RepositoryPort and domain.MRCreatorPort via the GitLab REST API.
// No local clone is needed: all operations are performed through the API.
type Adapter struct {
	http      *http.Client
	baseURL   string // https://gitlab.example.com/api/v4
	projectID string // URL-path-encoded, e.g. "group%2Frepo"
	branch    string
	token     string
	gitCfg    config.GitConfig
	log       *slog.Logger
}

// New constructs a GitLab Adapter.
func New(cfg config.RepositoryConfig, gitCfg config.GitConfig, log *slog.Logger) *Adapter {
	return &Adapter{
		http:      &http.Client{Timeout: defaultTimeout},
		baseURL:   strings.TrimRight(cfg.URL, "/") + "/api/v4",
		projectID: url.PathEscape(cfg.ProjectID),
		branch:    cfg.DefaultBranch,
		token:     cfg.Token,
		gitCfg:    gitCfg,
		log:       log,
	}
}

// ---------------------------------------------------------------------------
// domain.RepositoryPort
// ---------------------------------------------------------------------------

// Fetch verifies that the project is reachable and the token is valid.
// Since we are API-only (no local clone), "fetch" is a connectivity check.
func (a *Adapter) Fetch(ctx context.Context) error {
	var project struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	path := fmt.Sprintf("/projects/%s", a.projectID)
	if err := a.get(ctx, path, nil, &project); err != nil {
		return fmt.Errorf("gitlab fetch: %w", err)
	}
	a.log.InfoContext(ctx, "gitlab: project reachable", slog.String("name", project.Name))
	return nil
}

// HeadSHA returns the HEAD commit SHA of the default branch.
func (a *Adapter) HeadSHA(ctx context.Context) (string, error) {
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	path := fmt.Sprintf("/projects/%s/repository/branches/%s",
		a.projectID, url.PathEscape(a.branch))
	if err := a.get(ctx, path, nil, &branch); err != nil {
		return "", fmt.Errorf("gitlab head sha: %w", err)
	}
	return branch.Commit.ID, nil
}

// Diff returns file diffs between fromSHA and toSHA using the GitLab compare API.
func (a *Adapter) Diff(ctx context.Context, fromSHA, toSHA string) ([]domain.FileDiff, error) {
	var result struct {
		Diffs []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			Diff        string `json:"diff"`
			NewFile     bool   `json:"new_file"`
			RenamedFile bool   `json:"renamed_file"`
			DeletedFile bool   `json:"deleted_file"`
		} `json:"diffs"`
	}

	q := url.Values{"from": {fromSHA}, "to": {toSHA}}
	path := fmt.Sprintf("/projects/%s/repository/compare", a.projectID)
	if err := a.get(ctx, path, q, &result); err != nil {
		return nil, fmt.Errorf("gitlab diff: %w", err)
	}

	diffs := make([]domain.FileDiff, 0, len(result.Diffs))
	for _, d := range result.Diffs {
		diffs = append(diffs, domain.FileDiff{
			Path:    d.NewPath,
			OldPath: d.OldPath,
			Status:  changeStatus(d.NewFile, d.RenamedFile, d.DeletedFile),
			Patch:   d.Diff,
		})
	}
	a.log.InfoContext(ctx, "gitlab: diff retrieved",
		slog.Int("files", len(diffs)),
		slog.String("from", fromSHA[:min(len(fromSHA), 8)]),
		slog.String("to", toSHA[:min(len(toSHA), 8)]),
	)
	return diffs, nil
}

// ---------------------------------------------------------------------------
// domain.MRCreatorPort
// ---------------------------------------------------------------------------

// CreateBranch creates a new branch from the default branch HEAD.
func (a *Adapter) CreateBranch(ctx context.Context, name string) error {
	body := map[string]string{
		"branch": name,
		"ref":    a.branch,
	}
	path := fmt.Sprintf("/projects/%s/repository/branches", a.projectID)
	if err := a.post(ctx, path, body, nil); err != nil {
		return fmt.Errorf("gitlab create branch %q: %w", name, err)
	}
	a.log.InfoContext(ctx, "gitlab: branch created", slog.String("branch", name))
	return nil
}

// CommitFiles commits the provided documents to branch in a single commit.
// It detects whether each file should be created or updated via the files API.
func (a *Adapter) CommitFiles(ctx context.Context, branch string, docs []domain.Document, message string) error {
	type action struct {
		Action   string `json:"action"` // "create" | "update"
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"` // "text"
	}

	actions := make([]action, 0, len(docs))
	for _, doc := range docs {
		act := "update"
		if !a.fileExists(ctx, branch, doc.Path) {
			act = "create"
		}
		actions = append(actions, action{
			Action:   act,
			FilePath: doc.Path,
			Content:  doc.Content,
			Encoding: "text",
		})
	}

	body := map[string]any{
		"branch":         branch,
		"commit_message": message,
		"actions":        actions,
	}
	path := fmt.Sprintf("/projects/%s/repository/commits", a.projectID)
	if err := a.post(ctx, path, body, nil); err != nil {
		return fmt.Errorf("gitlab commit files: %w", err)
	}
	a.log.InfoContext(ctx, "gitlab: files committed",
		slog.String("branch", branch),
		slog.Int("files", len(docs)),
	)
	return nil
}

// CreateMR opens a merge request and returns its IID as a string.
func (a *Adapter) CreateMR(ctx context.Context, mr domain.MergeRequest) (string, error) {
	body := map[string]any{
		"source_branch": mr.Branch,
		"target_branch": a.branch,
		"title":         mr.Title,
		"description":   mr.Description,
		"labels":        botLabel,
	}

	var resp struct {
		IID int    `json:"iid"`
		URL string `json:"web_url"`
	}
	path := fmt.Sprintf("/projects/%s/merge_requests", a.projectID)
	if err := a.post(ctx, path, body, &resp); err != nil {
		return "", fmt.Errorf("gitlab create mr: %w", err)
	}
	a.log.InfoContext(ctx, "gitlab: MR created",
		slog.Int("iid", resp.IID),
		slog.String("url", resp.URL),
	)
	return fmt.Sprintf("%d", resp.IID), nil
}

// OpenBotMRs returns all open MRs that carry the bot label.
func (a *Adapter) OpenBotMRs(ctx context.Context) ([]domain.MergeRequest, error) {
	var page []struct {
		IID         int    `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		SourceBranch string `json:"source_branch"`
		WebURL      string `json:"web_url"`
	}

	q := url.Values{
		"state":  {"opened"},
		"labels": {botLabel},
	}
	path := fmt.Sprintf("/projects/%s/merge_requests", a.projectID)
	if err := a.get(ctx, path, q, &page); err != nil {
		return nil, fmt.Errorf("gitlab open bot mrs: %w", err)
	}

	mrs := make([]domain.MergeRequest, 0, len(page))
	for _, m := range page {
		mrs = append(mrs, domain.MergeRequest{
			ID:          fmt.Sprintf("%d", m.IID),
			Title:       m.Title,
			Description: m.Description,
			Branch:      m.SourceBranch,
			URL:         m.WebURL,
		})
	}
	return mrs, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (a *Adapter) get(ctx context.Context, path string, query url.Values, out any) error {
	u := a.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return a.do(req, out)
}

func (a *Adapter) post(ctx context.Context, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, out)
}

func (a *Adapter) do(req *http.Request, out any) error {
	req.Header.Set("PRIVATE-TOKEN", a.token)

	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("gitlab api %s %s: status %d: %s",
			req.Method, req.URL.Path, resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// fileExists checks whether a file exists on the given branch via the files API.
func (a *Adapter) fileExists(ctx context.Context, branch, filePath string) bool {
	q := url.Values{"ref": {branch}}
	path := fmt.Sprintf("/projects/%s/repository/files/%s",
		a.projectID, url.PathEscape(filePath))
	var dummy struct{}
	err := a.get(ctx, path, q, &dummy)
	return err == nil
}

func changeStatus(isNew, isRenamed, isDeleted bool) domain.ChangeStatus {
	switch {
	case isNew:
		return domain.ChangeStatusAdded
	case isDeleted:
		return domain.ChangeStatusDeleted
	case isRenamed:
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
