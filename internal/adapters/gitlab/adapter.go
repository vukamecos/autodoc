package gitlab

import (
	"context"
	"log/slog"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// Adapter implements domain.RepositoryPort and domain.MRCreatorPort via the GitLab API.
type Adapter struct {
	// TODO: use github.com/xanzy/go-gitlab client
	cfg    config.RepositoryConfig
	gitCfg config.GitConfig
	log    *slog.Logger
}

// New constructs a new GitLab Adapter.
func New(cfg config.RepositoryConfig, gitCfg config.GitConfig, log *slog.Logger) *Adapter {
	return &Adapter{
		cfg:    cfg,
		gitCfg: gitCfg,
		log:    log,
	}
}

// Fetch fetches the latest state of the remote repository.
func (a *Adapter) Fetch(ctx context.Context) error {
	// TODO: implement
	return nil
}

// Diff returns the list of file diffs between fromSHA and toSHA.
func (a *Adapter) Diff(ctx context.Context, fromSHA, toSHA string) ([]domain.FileDiff, error) {
	// TODO: implement
	return nil, nil
}

// HeadSHA returns the current HEAD SHA of the default branch.
func (a *Adapter) HeadSHA(ctx context.Context) (string, error) {
	// TODO: implement
	return "", nil
}

// CreateBranch creates a new branch with the given name from the default branch.
func (a *Adapter) CreateBranch(ctx context.Context, name string) error {
	// TODO: implement
	return nil
}

// CommitFiles commits the provided documents to the given branch.
func (a *Adapter) CommitFiles(ctx context.Context, branch string, docs []domain.Document, message string) error {
	// TODO: implement
	return nil
}

// CreateMR creates a merge request and returns its ID.
func (a *Adapter) CreateMR(ctx context.Context, mr domain.MergeRequest) (string, error) {
	// TODO: implement
	return "", nil
}

// OpenBotMRs returns all currently open merge requests created by the bot.
func (a *Adapter) OpenBotMRs(ctx context.Context) ([]domain.MergeRequest, error) {
	// TODO: implement
	return nil, nil
}
