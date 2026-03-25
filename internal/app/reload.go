package app

import (
	"log/slog"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
)

// ConfigReloader watches the config file and applies safe updates at runtime.
type ConfigReloader struct {
	app  *App
	log  *slog.Logger
	mu   sync.Mutex
	done chan struct{}
}

// EnableConfigReload starts watching the config file for changes.
// When the file changes, reloadable settings are applied without restart.
// Settings that require restart (like ACP base URL) are logged as warnings.
func (a *App) EnableConfigReload() *ConfigReloader {
	cr := &ConfigReloader{
		app:  a,
		log:  a.log,
		done: make(chan struct{}),
	}

	viper.OnConfigChange(func(e fsnotify.Event) {
		cr.log.Info("config: file changed, reloading", "file", e.Name)
		cr.reload()
	})
	viper.WatchConfig()
	cr.log.Info("config: hot-reload enabled")

	return cr
}

// Stop stops watching for config changes.
func (cr *ConfigReloader) Stop() {
	close(cr.done)
}

func (cr *ConfigReloader) reload() {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	newCfg, err := config.LoadFromViper()
	if err != nil {
		cr.log.Error("config: reload failed, keeping current config", "error", err)
		return
	}

	oldCfg := cr.app.cfg
	var changes []string

	// Scheduler cron — can be updated at runtime by re-registering.
	if newCfg.Scheduler.Cron != oldCfg.Scheduler.Cron {
		if cr.app.scheduler != nil {
			if err := cr.app.scheduler.Register(newCfg.Scheduler.Cron, cr.app.useCase); err != nil {
				cr.log.Error("config: failed to update scheduler cron", "error", err)
			} else {
				changes = append(changes, "scheduler.cron")
			}
		}
	}

	// Validation settings — update in place.
	if newCfg.Validation != oldCfg.Validation {
		changes = append(changes, "validation")
	}

	// Documentation settings.
	if newCfg.Documentation.PrimaryLanguage != oldCfg.Documentation.PrimaryLanguage {
		changes = append(changes, "documentation.primary_language")
	}

	// ACP settings that require restart.
	if newCfg.ACP.BaseURL != oldCfg.ACP.BaseURL {
		cr.log.Warn("config: acp.base_url changed — restart required to apply")
	}
	if newCfg.ACP.Provider != oldCfg.ACP.Provider {
		cr.log.Warn("config: acp.provider changed — restart required to apply")
	}
	if newCfg.ACP.Token != oldCfg.ACP.Token {
		cr.log.Warn("config: acp.token changed — restart required to apply")
	}

	// Repository settings that require restart.
	if newCfg.Repository.URL != oldCfg.Repository.URL {
		cr.log.Warn("config: repository.url changed — restart required to apply")
	}
	if newCfg.Repository.Provider != oldCfg.Repository.Provider {
		cr.log.Warn("config: repository.provider changed — restart required to apply")
	}

	// ACP settings that can be hot-reloaded via the config pointer.
	if newCfg.ACP.Model != oldCfg.ACP.Model {
		changes = append(changes, "acp.model")
	}
	if newCfg.ACP.MaxContextBytes != oldCfg.ACP.MaxContextBytes {
		changes = append(changes, "acp.max_context_bytes")
	}
	if newCfg.ACP.MaxRetries != oldCfg.ACP.MaxRetries {
		changes = append(changes, "acp.max_retries")
	}

	// Git settings.
	if newCfg.Git.BranchPrefix != oldCfg.Git.BranchPrefix {
		changes = append(changes, "git.branch_prefix")
	}
	if newCfg.Git.CommitMessageTemplate != oldCfg.Git.CommitMessageTemplate {
		changes = append(changes, "git.commit_message_template")
	}

	// Apply the new config.
	*cr.app.cfg = *newCfg

	if len(changes) > 0 {
		cr.log.Info("config: reloaded settings", "changed", changes)
	} else {
		cr.log.Info("config: no reloadable settings changed")
	}
}
