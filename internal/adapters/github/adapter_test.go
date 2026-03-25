package github

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	testOwner = "myorg"
	testRepo  = "myrepo"
	testToken = "ghp-test-token"
	testBranch = "main"
)

func newTestAdapter(t *testing.T, handler http.Handler) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.RepositoryConfig{
		URL:           srv.URL, // GitHub Enterprise style
		ProjectID:     testOwner + "/" + testRepo,
		DefaultBranch: testBranch,
		Token:         testToken,
		MaxRetries:    0, // disable retries unless explicitly set
		RetryDelay:    time.Millisecond,
	}
	return New(cfg, config.GitConfig{BranchPrefix: "bot/docs-update/"}, slog.Default())
}

func newTestAdapterWithRetry(t *testing.T, handler http.Handler, maxRetries int) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.RepositoryConfig{
		URL:           srv.URL,
		ProjectID:     testOwner + "/" + testRepo,
		DefaultBranch: testBranch,
		Token:         testToken,
		MaxRetries:    maxRetries,
		RetryDelay:    5 * time.Millisecond,
	}
	return New(cfg, config.GitConfig{BranchPrefix: "bot/docs-update/"}, slog.Default())
}

// assertAuthHeader verifies the Authorization header is present and correct.
func assertAuthHeader(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("expected Authorization='Bearer %s', got %q", testToken, got)
	}
}

// assertAPIVersionHeader verifies the GitHub API version header.
func assertAPIVersionHeader(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-GitHub-Api-Version"); got != ghAPIVersion {
		t.Errorf("expected X-GitHub-Api-Version=%q, got %q", ghAPIVersion, got)
	}
}

// writeJSON encodes v as JSON into w with status 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var ctx = context.Background()

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func TestFetch_Success(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		assertAPIVersionHeader(t, r)
		if r.URL.Path != "/repos/"+testOwner+"/"+testRepo {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, map[string]any{"full_name": testOwner + "/" + testRepo, "id": 123})
	}))

	if err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
}

func TestFetch_Unauthorized(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"message": "Bad credentials"})
	}))

	if err := a.Fetch(ctx); err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// ---------------------------------------------------------------------------
// HeadSHA
// ---------------------------------------------------------------------------

func TestHeadSHA(t *testing.T) {
	const wantSHA = "abc123def456789"

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		expectedPath := "/repos/" + testOwner + "/" + testRepo + "/git/refs/heads/" + testBranch
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"object": map[string]string{"sha": wantSHA},
		})
	}))

	sha, err := a.HeadSHA(ctx)
	if err != nil {
		t.Fatalf("HeadSHA() error: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("expected SHA %q, got %q", wantSHA, sha)
	}
}

func TestHeadSHA_ServerError(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))

	if _, err := a.HeadSHA(ctx); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

func TestDiff(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		expectedPath := "/repos/" + testOwner + "/" + testRepo + "/compare/sha-from...sha-to"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"files": []map[string]any{
				{
					"filename":          "internal/foo.go",
					"previous_filename": "",
					"status":            "modified",
					"patch":             "@@ -1 +1 @@\n-old\n+new\n",
				},
				{
					"filename":          "internal/bar.go",
					"previous_filename": "",
					"status":            "added",
					"patch":             "@@ -0,0 +1 @@\n+new file\n",
				},
				{
					"filename":          "internal/old.go",
					"previous_filename": "",
					"status":            "removed",
					"patch":             "",
				},
				{
					"filename":          "internal/new.go",
					"previous_filename": "internal/old_name.go",
					"status":            "renamed",
					"patch":             "",
				},
			},
		})
	}))

	diffs, err := a.Diff(ctx, "sha-from", "sha-to")
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) != 4 {
		t.Fatalf("expected 4 diffs, got %d", len(diffs))
	}

	if diffs[0].Status != domain.ChangeStatusModified {
		t.Errorf("expected Modified, got %q", diffs[0].Status)
	}
	if diffs[1].Status != domain.ChangeStatusAdded {
		t.Errorf("expected Added, got %q", diffs[1].Status)
	}
	if diffs[2].Status != domain.ChangeStatusDeleted {
		t.Errorf("expected Deleted, got %q", diffs[2].Status)
	}
	if diffs[3].Status != domain.ChangeStatusRenamed {
		t.Errorf("expected Renamed, got %q", diffs[3].Status)
	}
	if diffs[3].OldPath != "internal/old_name.go" {
		t.Errorf("expected OldPath 'internal/old_name.go', got %q", diffs[3].OldPath)
	}
}

// ---------------------------------------------------------------------------
// CreateBranch
// ---------------------------------------------------------------------------

func TestCreateBranch(t *testing.T) {
	const headSHA = "headsha123"

	mux := http.NewServeMux()
	// HeadSHA endpoint
	mux.HandleFunc("/repos/"+testOwner+"/"+testRepo+"/git/refs/heads/"+testBranch, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"object": map[string]string{"sha": headSHA}})
	})
	// Create ref endpoint
	var capturedBody map[string]string
	mux.HandleFunc("/repos/"+testOwner+"/"+testRepo+"/git/refs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		writeJSON(w, map[string]any{"ref": "refs/heads/bot/docs-update/123", "object": map[string]string{"sha": headSHA}})
	})

	a := newTestAdapter(t, mux)

	if err := a.CreateBranch(ctx, "bot/docs-update/123"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}

	if capturedBody["ref"] != "refs/heads/bot/docs-update/123" {
		t.Errorf("expected ref 'refs/heads/bot/docs-update/123', got %q", capturedBody["ref"])
	}
	if capturedBody["sha"] != headSHA {
		t.Errorf("expected sha %q, got %q", headSHA, capturedBody["sha"])
	}
}

// ---------------------------------------------------------------------------
// CommitFiles
// ---------------------------------------------------------------------------

func TestCommitFiles(t *testing.T) {
	const (
		baseCommitSHA = "basecommit123"
		baseTreeSHA   = "basetree456"
		newBlobSHA    = "newblob789"
		newTreeSHA    = "newtreeabc"
		newCommitSHA  = "newcommitdef"
	)

	blobCount := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		method := r.Method

		switch {
		// Get ref (for branch)
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/refs/heads/feature-branch" && method == http.MethodGet:
			writeJSON(w, map[string]any{"object": map[string]string{"sha": baseCommitSHA}})

		// Update ref (PATCH request)
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/refs/heads/feature-branch" && method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)

		// Get commit (for tree SHA)
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/commits/"+baseCommitSHA:
			writeJSON(w, map[string]any{"tree": map[string]string{"sha": baseTreeSHA}})

		// Create blob
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/blobs":
			if method != http.MethodPost {
				t.Errorf("expected POST for blob, got %s", method)
			}
			blobCount++
			writeJSON(w, map[string]string{"sha": newBlobSHA + string(rune('0'+blobCount))})

		// Create tree
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/trees":
			if method != http.MethodPost {
				t.Errorf("expected POST for tree, got %s", method)
			}
			writeJSON(w, map[string]string{"sha": newTreeSHA})

		// Create commit
		case path == "/repos/"+testOwner+"/"+testRepo+"/git/commits":
			if method != http.MethodPost {
				t.Errorf("expected POST for commit, got %s", method)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["message"] != "docs: update documentation" {
				t.Errorf("expected message 'docs: update documentation', got %q", body["message"])
			}
			writeJSON(w, map[string]string{"sha": newCommitSHA})

		default:
			t.Errorf("unexpected request: %s %s", method, path)
			http.NotFound(w, r)
		}
	})

	a := newTestAdapter(t, handler)

	docs := []domain.Document{
		{Path: "README.md", Content: "# Updated README"},
		{Path: "docs/guide.md", Content: "# Guide"},
	}

	if err := a.CommitFiles(ctx, "feature-branch", docs, "docs: update documentation"); err != nil {
		t.Fatalf("CommitFiles() error: %v", err)
	}

	if blobCount != 2 {
		t.Errorf("expected 2 blobs created, got %d", blobCount)
	}
}

// ---------------------------------------------------------------------------
// CreateMR (Pull Request)
// ---------------------------------------------------------------------------

func TestCreateMR(t *testing.T) {
	var capturedBody map[string]any

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		method := r.Method

		switch {
		// Create PR endpoint
		case path == "/repos/"+testOwner+"/"+testRepo+"/pulls" && method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeJSON(w, map[string]any{
				"number":   42,
				"html_url": "https://github.com/" + testOwner + "/" + testRepo + "/pull/42",
			})

		// Add label endpoint (best-effort, can be ignored)
		case strings.HasPrefix(path, "/repos/"+testOwner+"/"+testRepo+"/issues/"):
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected request: %s %s", method, path)
			http.NotFound(w, r)
		}
	})

	a := newTestAdapter(t, handler)

	mr := domain.MergeRequest{
		Title:       "Docs: update README",
		Description: "Automated documentation update",
		Branch:      "bot/docs-update/123",
	}
	created, err := a.CreateMR(ctx, mr)
	if err != nil {
		t.Fatalf("CreateMR() error: %v", err)
	}
	if created.ID != "42" {
		t.Errorf("expected id '42', got %q", created.ID)
	}
	if created.URL != "https://github.com/"+testOwner+"/"+testRepo+"/pull/42" {
		t.Errorf("unexpected url %q", created.URL)
	}

	if capturedBody["title"] != mr.Title {
		t.Errorf("expected title %q, got %q", mr.Title, capturedBody["title"])
	}
	if capturedBody["head"] != mr.Branch {
		t.Errorf("expected head %q, got %q", mr.Branch, capturedBody["head"])
	}
	if capturedBody["base"] != testBranch {
		t.Errorf("expected base %q, got %q", testBranch, capturedBody["base"])
	}
}

// ---------------------------------------------------------------------------
// OpenBotMRs
// ---------------------------------------------------------------------------

func TestOpenBotMRs_ReturnsBotMRs(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+testOwner+"/"+testRepo+"/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		state := r.URL.Query().Get("state")
		if state != "open" {
			t.Errorf("expected state=open, got %q", state)
		}

		writeJSON(w, []map[string]any{
			{
				"number": 1,
				"title":  "Docs: update",
				"body":   "auto",
				"head":   map[string]string{"ref": "bot/docs-update/111"},
				"html_url": "https://github.com/pr/1",
			},
			{
				"number": 2,
				"title":  "Feature: login",
				"body":   "manual",
				"head":   map[string]string{"ref": "feature/login"},
				"html_url": "https://github.com/pr/2",
			},
			{
				"number": 3,
				"title":  "Docs: fix",
				"body":   "auto",
				"head":   map[string]string{"ref": "bot/docs-update/222"},
				"html_url": "https://github.com/pr/3",
			},
		})
	}))

	mrs, err := a.OpenBotMRs(ctx)
	if err != nil {
		t.Fatalf("OpenBotMRs() error: %v", err)
	}
	if len(mrs) != 2 {
		t.Fatalf("expected 2 bot MRs, got %d", len(mrs))
	}
	if mrs[0].ID != "1" || mrs[0].Branch != "bot/docs-update/111" {
		t.Errorf("unexpected first MR: %+v", mrs[0])
	}
	if mrs[1].ID != "3" || mrs[1].Branch != "bot/docs-update/222" {
		t.Errorf("unexpected second MR: %+v", mrs[1])
	}
}

func TestOpenBotMRs_EmptyList(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []any{})
	}))

	mrs, err := a.OpenBotMRs(ctx)
	if err != nil {
		t.Fatalf("OpenBotMRs() error: %v", err)
	}
	if len(mrs) != 0 {
		t.Errorf("expected 0 MRs, got %d", len(mrs))
	}
}

// ---------------------------------------------------------------------------
// UpdateMR (Pull Request)
// ---------------------------------------------------------------------------

func TestUpdateMR(t *testing.T) {
	var capturedBody map[string]any
	var capturedMethod string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+testOwner+"/"+testRepo+"/pulls/42" {
			capturedMethod = r.Method
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeJSON(w, map[string]any{"number": 42, "html_url": "https://github.com/pr/42"})
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
		http.NotFound(w, r)
	})

	a := newTestAdapter(t, handler)
	err := a.UpdateMR(ctx, "42", domain.MergeRequest{
		Title:       "Updated title",
		Description: "Updated description",
	})
	if err != nil {
		t.Fatalf("UpdateMR() error: %v", err)
	}
	if capturedMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", capturedMethod)
	}
	if capturedBody["title"] != "Updated title" {
		t.Errorf("expected title 'Updated title', got %q", capturedBody["title"])
	}
	if capturedBody["body"] != "Updated description" {
		t.Errorf("expected body 'Updated description', got %q", capturedBody["body"])
	}
}

func TestUpdateMR_ServerError(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	if err := a.UpdateMR(ctx, "99", domain.MergeRequest{Title: "x"}); err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// ---------------------------------------------------------------------------
// Retry behaviour
// ---------------------------------------------------------------------------

func TestFetch_RetriesOn503(t *testing.T) {
	var calls atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"full_name": "test/repo", "id": 1})
	})

	a := newTestAdapterWithRetry(t, handler, 3)
	if err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch() should succeed after retries, got: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls (2×503 + 1×200), got %d", calls.Load())
	}
}

func TestFetch_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	})

	a := newTestAdapterWithRetry(t, handler, 2)
	if err := a.Fetch(ctx); err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if calls.Load() != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 total calls, got %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Auth header validation
// ---------------------------------------------------------------------------

func TestAdapter_SendsAuthorizationHeader(t *testing.T) {
	var gotToken string

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		writeJSON(w, map[string]any{"full_name": "test/repo"})
	}))

	_ = a.Fetch(ctx)

	if gotToken != "Bearer "+testToken {
		t.Errorf("expected Authorization='Bearer %s', got %q", testToken, gotToken)
	}
}

// ---------------------------------------------------------------------------
// githubChangeStatus helper
// ---------------------------------------------------------------------------

func TestGithubChangeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  domain.ChangeStatus
	}{
		{"added", domain.ChangeStatusAdded},
		{"removed", domain.ChangeStatusDeleted},
		{"renamed", domain.ChangeStatusRenamed},
		{"modified", domain.ChangeStatusModified},
		{"unknown", domain.ChangeStatusModified}, // default case
		{"", domain.ChangeStatusModified},        // default case
	}
	for _, tc := range tests {
		got := githubChangeStatus(tc.input)
		if got != tc.want {
			t.Errorf("githubChangeStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew_WithDefaultBaseURL(t *testing.T) {
	cfg := config.RepositoryConfig{
		ProjectID:     "owner/repo",
		DefaultBranch: "main",
		Token:         "token",
	}

	a := New(cfg, config.GitConfig{}, slog.Default())
	if a.base != defaultBaseURL {
		t.Errorf("expected default base URL %q, got %q", defaultBaseURL, a.base)
	}
	if a.owner != "owner" {
		t.Errorf("expected owner 'owner', got %q", a.owner)
	}
	if a.repo != "repo" {
		t.Errorf("expected repo 'repo', got %q", a.repo)
	}
}

func TestNew_WithCustomBaseURL(t *testing.T) {
	cfg := config.RepositoryConfig{
		URL:           "https://github.enterprise.com",
		ProjectID:     "myorg/myrepo",
		DefaultBranch: "develop",
		Token:         "ghe-token",
	}

	a := New(cfg, config.GitConfig{}, slog.Default())
	if a.base != "https://github.enterprise.com" {
		t.Errorf("expected custom base URL, got %q", a.base)
	}
	if a.branch != "develop" {
		t.Errorf("expected branch 'develop', got %q", a.branch)
	}
}
