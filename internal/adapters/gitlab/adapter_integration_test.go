package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
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

const testProjectID = "42"
const testToken = "secret-token"
const testBranch = "main"

// projectPrefix is the URL prefix for all project-scoped API paths.
const projectPrefix = "/api/v4/projects/" + testProjectID

func newTestAdapter(t *testing.T, handler http.Handler) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.RepositoryConfig{
		URL:           srv.URL,
		ProjectID:     testProjectID,
		DefaultBranch: testBranch,
		Token:         testToken,
		MaxRetries:    0, // disable retries unless the test explicitly sets them
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
		ProjectID:     testProjectID,
		DefaultBranch: testBranch,
		Token:         testToken,
		MaxRetries:    maxRetries,
		RetryDelay:    5 * time.Millisecond,
	}
	return New(cfg, config.GitConfig{BranchPrefix: "bot/docs-update/"}, slog.Default())
}

// assertToken verifies the PRIVATE-TOKEN header is present and correct.
func assertToken(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("PRIVATE-TOKEN"); got != testToken {
		t.Errorf("expected PRIVATE-TOKEN=%q, got %q", testToken, got)
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
		assertToken(t, r)
		if r.URL.Path != projectPrefix {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"id": 42, "name": "myrepo"})
	}))

	if err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
}

func TestFetch_Unauthorized(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))

	if err := a.Fetch(ctx); err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// ---------------------------------------------------------------------------
// HeadSHA
// ---------------------------------------------------------------------------

func TestHeadSHA(t *testing.T) {
	const wantSHA = "abc123def456"

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertToken(t, r)
		writeJSON(w, map[string]any{
			"commit": map[string]string{"id": wantSHA},
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
		assertToken(t, r)
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if from == "" || to == "" {
			t.Errorf("expected from/to query params, got from=%q to=%q", from, to)
		}
		writeJSON(w, map[string]any{
			"diffs": []map[string]any{
				{
					"old_path":     "internal/foo.go",
					"new_path":     "internal/foo.go",
					"diff":         "@@ -1 +1 @@\n-old\n+new\n",
					"new_file":     false,
					"renamed_file": false,
					"deleted_file": false,
				},
				{
					"old_path":     "",
					"new_path":     "internal/bar.go",
					"diff":         "@@ -0,0 +1 @@\n+new file\n",
					"new_file":     true,
					"renamed_file": false,
					"deleted_file": false,
				},
			},
		})
	}))

	diffs, err := a.Diff(ctx, "sha-from", "sha-to")
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}

	modified := diffs[0]
	if modified.Status != domain.ChangeStatusModified {
		t.Errorf("expected Modified, got %q", modified.Status)
	}
	if modified.Path != "internal/foo.go" {
		t.Errorf("expected 'internal/foo.go', got %q", modified.Path)
	}

	added := diffs[1]
	if added.Status != domain.ChangeStatusAdded {
		t.Errorf("expected Added, got %q", added.Status)
	}
}

func TestDiff_DeletedAndRenamed(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"diffs": []map[string]any{
				{"old_path": "old.go", "new_path": "old.go", "diff": "", "new_file": false, "renamed_file": false, "deleted_file": true},
				{"old_path": "a.go", "new_path": "b.go", "diff": "", "new_file": false, "renamed_file": true, "deleted_file": false},
			},
		})
	}))

	diffs, _ := a.Diff(ctx, "a", "b")
	if diffs[0].Status != domain.ChangeStatusDeleted {
		t.Errorf("expected Deleted, got %q", diffs[0].Status)
	}
	if diffs[1].Status != domain.ChangeStatusRenamed {
		t.Errorf("expected Renamed, got %q", diffs[1].Status)
	}
}

// ---------------------------------------------------------------------------
// CreateBranch
// ---------------------------------------------------------------------------

func TestCreateBranch(t *testing.T) {
	var called bool

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		assertToken(t, r)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["branch"] == "" || body["ref"] != testBranch {
			t.Errorf("unexpected body: %v", body)
		}
		called = true
		writeJSON(w, map[string]string{"name": body["branch"]})
	}))

	if err := a.CreateBranch(ctx, "bot/docs-update/12345"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	if !called {
		t.Error("expected POST handler to be called")
	}
}

// ---------------------------------------------------------------------------
// CommitFiles
// ---------------------------------------------------------------------------

func TestCommitFiles_CreatesAndUpdates(t *testing.T) {
	// README.md exists → "update"; docs/new.md does not → "create"
	var capturedActions []map[string]any

	mux := http.NewServeMux()

	// fileExists for README.md → 200
	mux.HandleFunc("/api/v4/projects/42/repository/files/README.md", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"file_name": "README.md"})
	})
	// fileExists for docs/new.md → 404
	mux.HandleFunc("/api/v4/projects/42/repository/files/docs%2Fnew.md", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	// commit endpoint
	mux.HandleFunc("/api/v4/projects/42/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Actions []map[string]any `json:"actions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedActions = body.Actions
		writeJSON(w, map[string]string{"id": "commitsha"})
	})

	a := newTestAdapter(t, mux)

	docs := []domain.Document{
		{Path: "README.md", Content: "# Updated"},
		{Path: "docs/new.md", Content: "# New doc"},
	}
	if err := a.CommitFiles(ctx, "bot/docs-update/1", docs, "docs: update"); err != nil {
		t.Fatalf("CommitFiles() error: %v", err)
	}

	if len(capturedActions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(capturedActions))
	}

	// Find actions by file_path
	actionFor := func(path string) string {
		for _, a := range capturedActions {
			if a["file_path"] == path {
				return a["action"].(string)
			}
		}
		return ""
	}

	if got := actionFor("README.md"); got != "update" {
		t.Errorf("README.md: expected action 'update', got %q", got)
	}
	if got := actionFor("docs/new.md"); got != "create" {
		t.Errorf("docs/new.md: expected action 'create', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// CreateMR
// ---------------------------------------------------------------------------

func TestCreateMR(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		assertToken(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["labels"] != botLabel {
			t.Errorf("expected label %q, got %q", botLabel, body["labels"])
		}
		writeJSON(w, map[string]any{"iid": 7, "web_url": "https://gitlab.example.com/mr/7"})
	}))

	mr := domain.MergeRequest{
		Title:       "Docs: update",
		Description: "automated update",
		Branch:      "bot/docs-update/1",
	}
	created, err := a.CreateMR(ctx, mr)
	if err != nil {
		t.Fatalf("CreateMR() error: %v", err)
	}
	if created.ID != "7" {
		t.Errorf("expected id '7', got %q", created.ID)
	}
	if created.URL != "https://gitlab.example.com/mr/7" {
		t.Errorf("expected url 'https://gitlab.example.com/mr/7', got %q", created.URL)
	}
}

// ---------------------------------------------------------------------------
// UpdateMR
// ---------------------------------------------------------------------------

func TestUpdateMR(t *testing.T) {
	var capturedMethod string
	var capturedBody map[string]any

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertToken(t, r)
		expectedPath := projectPrefix + "/merge_requests/7"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		capturedMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		writeJSON(w, map[string]any{"iid": 7, "web_url": "https://gitlab.example.com/mr/7"})
	}))

	err := a.UpdateMR(ctx, "7", domain.MergeRequest{
		Title:       "Updated title",
		Description: "Updated description",
	})
	if err != nil {
		t.Fatalf("UpdateMR() error: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", capturedMethod)
	}
	if capturedBody["title"] != "Updated title" {
		t.Errorf("expected title 'Updated title', got %q", capturedBody["title"])
	}
	if capturedBody["description"] != "Updated description" {
		t.Errorf("expected description 'Updated description', got %q", capturedBody["description"])
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
// OpenBotMRs
// ---------------------------------------------------------------------------

func TestOpenBotMRs_ReturnsBotMRs(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "opened" || r.URL.Query().Get("labels") != botLabel {
			t.Errorf("unexpected query params: %v", r.URL.RawQuery)
		}
		writeJSON(w, []map[string]any{
			{
				"iid":           1,
				"title":         "Docs: update",
				"description":   "body",
				"source_branch": "bot/docs-update/111",
				"web_url":       "https://gitlab.example.com/mr/1",
			},
		})
	}))

	mrs, err := a.OpenBotMRs(ctx)
	if err != nil {
		t.Fatalf("OpenBotMRs() error: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("expected 1 MR, got %d", len(mrs))
	}
	if mrs[0].ID != "1" || mrs[0].Branch != "bot/docs-update/111" {
		t.Errorf("unexpected MR: %+v", mrs[0])
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
		writeJSON(w, map[string]any{"id": 1, "name": "repo"})
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

func TestAdapter_SendsPrivateTokenHeader(t *testing.T) {
	var gotToken string

	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		writeJSON(w, map[string]any{"id": 1, "name": "repo"})
	}))

	_ = a.Fetch(ctx)

	if gotToken != testToken {
		t.Errorf("expected PRIVATE-TOKEN=%q, got %q", testToken, gotToken)
	}
}

// ---------------------------------------------------------------------------
// changeStatus helper
// ---------------------------------------------------------------------------

func TestChangeStatus(t *testing.T) {
	tests := []struct {
		isNew, isRenamed, isDeleted bool
		want                        domain.ChangeStatus
	}{
		{true, false, false, domain.ChangeStatusAdded},
		{false, false, true, domain.ChangeStatusDeleted},
		{false, true, false, domain.ChangeStatusRenamed},
		{false, false, false, domain.ChangeStatusModified},
	}
	for _, tc := range tests {
		got := changeStatus(tc.isNew, tc.isRenamed, tc.isDeleted)
		if got != tc.want {
			t.Errorf("changeStatus(%v,%v,%v) = %q, want %q",
				tc.isNew, tc.isRenamed, tc.isDeleted, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// URL encoding
// ---------------------------------------------------------------------------

func TestDiff_PassesQueryParams(t *testing.T) {
	const from = "aaabbb"
	const to = "cccddd"

	var gotFrom, gotTo string
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFrom = r.URL.Query().Get("from")
		gotTo = r.URL.Query().Get("to")
		writeJSON(w, map[string]any{"diffs": []any{}})
	}))

	_, _ = a.Diff(ctx, from, to)

	if gotFrom != from || gotTo != to {
		t.Errorf("expected from=%q to=%q, got from=%q to=%q", from, to, gotFrom, gotTo)
	}
}


// ---------------------------------------------------------------------------
// Full workflow integration tests
// ---------------------------------------------------------------------------

func TestFullWorkflow_CreateBranchCommitAndMR(t *testing.T) {
	mux := http.NewServeMux()
	
	// Project info
	mux.HandleFunc(projectPrefix, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 42, "name": "test-repo"})
	})
	
	// HeadSHA
	mux.HandleFunc(projectPrefix+"/repository/branches/"+testBranch, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"commit": map[string]string{"id": "abc123"}})
	})
	
	// Create branch
	mux.HandleFunc(projectPrefix+"/repository/branches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, map[string]any{"name": body["branch"], "commit": map[string]string{"id": "abc123"}})
		}
	})
	
	// Commit files
	mux.HandleFunc(projectPrefix+"/repository/files/", func(w http.ResponseWriter, r *http.Request) {
		// File exists check
		writeJSON(w, map[string]string{"file_name": "README.md"})
	})
	
	var commitCalled bool
	mux.HandleFunc(projectPrefix+"/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			commitCalled = true
			writeJSON(w, map[string]string{"id": "commit123"})
		}
	})
	
	// Create MR
	var mrCalled bool
	mux.HandleFunc(projectPrefix+"/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mrCalled = true
			writeJSON(w, map[string]any{"iid": 42, "web_url": "https://gitlab.example.com/mr/42"})
		}
	})
	
	a := newTestAdapter(t, mux)
	
	// Step 1: Fetch
	if err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	
	// Step 2: Create branch
	if err := a.CreateBranch(ctx, "bot/docs-update/123"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	
	// Step 3: Commit files
	docs := []domain.Document{{Path: "README.md", Content: "# Updated"}}
	if err := a.CommitFiles(ctx, "bot/docs-update/123", docs, "docs: update"); err != nil {
		t.Fatalf("CommitFiles() error: %v", err)
	}
	if !commitCalled {
		t.Error("expected commit endpoint to be called")
	}
	
	// Step 4: Create MR
	mr := domain.MergeRequest{
		Title:       "Docs: update",
		Description: "Automated update",
		Branch:      "bot/docs-update/123",
	}
	createdMR, err := a.CreateMR(ctx, mr)
	if err != nil {
		t.Fatalf("CreateMR() error: %v", err)
	}
	if !mrCalled {
		t.Error("expected MR endpoint to be called")
	}
	if createdMR.ID != "42" {
		t.Errorf("expected MR ID 42, got %q", createdMR.ID)
	}
}

func TestDiff_WithLargeFileList(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertToken(t, r)
		
		// Simulate 100 files changed
		diffs := make([]map[string]any, 100)
		for i := range diffs {
			diffs[i] = map[string]any{
				"old_path":     fmt.Sprintf("internal/file%d.go", i),
				"new_path":     fmt.Sprintf("internal/file%d.go", i),
				"diff":         fmt.Sprintf("@@ -1 +1 @@\n-old%d\n+new%d\n", i, i),
				"new_file":     false,
				"renamed_file": false,
				"deleted_file": false,
			}
		}
		writeJSON(w, map[string]any{"diffs": diffs})
	}))
	
	diffs, err := a.Diff(ctx, "sha1", "sha2")
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) != 100 {
		t.Errorf("expected 100 diffs, got %d", len(diffs))
	}
}

func TestCommitFiles_MultipleFilesWithMixedActions(t *testing.T) {
	var capturedActions []map[string]any
	
	mux := http.NewServeMux()
	
	// File exists checks
	mux.HandleFunc(projectPrefix+"/repository/files/README.md", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"file_name": "README.md"}) // exists
	})
	mux.HandleFunc(projectPrefix+"/repository/files/docs%2Fnew.md", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // doesn't exist
	})
	mux.HandleFunc(projectPrefix+"/repository/files/docs%2Fexisting.md", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"file_name": "existing.md"}) // exists
	})
	
	mux.HandleFunc(projectPrefix+"/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Actions []map[string]any `json:"actions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			capturedActions = body.Actions
			writeJSON(w, map[string]string{"id": "commit123"})
		}
	})
	
	a := newTestAdapter(t, mux)
	
	docs := []domain.Document{
		{Path: "README.md", Content: "# Updated README"},
		{Path: "docs/new.md", Content: "# New doc"},
		{Path: "docs/existing.md", Content: "# Updated existing"},
	}
	
	if err := a.CommitFiles(ctx, "bot/test", docs, "docs: update"); err != nil {
		t.Fatalf("CommitFiles() error: %v", err)
	}
	
	if len(capturedActions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(capturedActions))
	}
	
	// Verify correct actions
	actionMap := make(map[string]string)
	for _, a := range capturedActions {
		actionMap[a["file_path"].(string)] = a["action"].(string)
	}
	
	if actionMap["README.md"] != "update" {
		t.Errorf("README.md: expected 'update', got %q", actionMap["README.md"])
	}
	if actionMap["docs/new.md"] != "create" {
		t.Errorf("docs/new.md: expected 'create', got %q", actionMap["docs/new.md"])
	}
	if actionMap["docs/existing.md"] != "update" {
		t.Errorf("docs/existing.md: expected 'update', got %q", actionMap["docs/existing.md"])
	}
}

func TestOpenBotMRs_LabelFiltering(t *testing.T) {
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertToken(t, r)
		
		state := r.URL.Query().Get("state")
		label := r.URL.Query().Get("labels")
		
		if state != "opened" || label != botLabel {
			t.Errorf("expected state=opened and labels=%s, got state=%s labels=%s", botLabel, state, label)
		}
		
		// GitLab API returns only MRs with the specified label
		writeJSON(w, []map[string]any{
			{
				"iid": 1, "title": "Bot: docs", "description": "auto",
				"source_branch": "bot/docs-update/1", "web_url": "https://example.com/1",
			},
			{
				"iid": 3, "title": "Bot: more docs", "description": "auto",
				"source_branch": "bot/docs-update/3", "web_url": "https://example.com/3",
			},
		})
	}))
	
	mrs, err := a.OpenBotMRs(ctx)
	if err != nil {
		t.Fatalf("OpenBotMRs() error: %v", err)
	}
	
	// All returned MRs should have bot label (GitLab filters by label server-side)
	if len(mrs) != 2 {
		t.Errorf("expected 2 bot MRs, got %d", len(mrs))
	}
	
	for _, mr := range mrs {
		if !strings.HasPrefix(mr.Branch, "bot/") {
			t.Errorf("expected bot branch, got %q", mr.Branch)
		}
	}
}

func TestCreateMR_WithSpecialCharacters(t *testing.T) {
	var capturedBody map[string]any
	
	a := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeJSON(w, map[string]any{"iid": 99, "web_url": "https://example.com/99"})
		}
	}))
	
	mr := domain.MergeRequest{
		Title:       "Docs: update README with \"special\" chars <>&",
		Description: "Line 1\nLine 2\n\n- Item 1\n- Item 2\n",
		Branch:      "bot/docs-update/" + fmt.Sprintf("%d", time.Now().Unix()),
	}
	
	created, err := a.CreateMR(ctx, mr)
	if err != nil {
		t.Fatalf("CreateMR() error: %v", err)
	}

	if created.ID != "99" {
		t.Errorf("expected id 99, got %q", created.ID)
	}
	
	if capturedBody["title"] != mr.Title {
		t.Errorf("title mismatch: expected %q, got %q", mr.Title, capturedBody["title"])
	}
}

func TestAdapter_NumericProjectID(t *testing.T) {
	// Test with numeric project ID (42)
	var requestPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		writeJSON(w, map[string]any{"id": 42, "name": "test"})
	}))
	defer srv.Close()
	
	cfg := config.RepositoryConfig{
		URL:           srv.URL,
		ProjectID:     "42",
		DefaultBranch: "main",
		Token:         "token",
	}
	a := New(cfg, config.GitConfig{}, slog.Default())
	
	if err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	
	// Project ID should be in the path
	if !strings.Contains(requestPath, "/projects/42") {
		t.Errorf("expected project ID in path, got %s", requestPath)
	}
}
