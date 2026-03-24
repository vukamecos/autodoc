package scheduler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/robfig/cron/v3"
)

// Job is a unit of work that the scheduler can run periodically.
type Job interface {
	Run(ctx context.Context) error
}

// Scheduler wraps robfig/cron to run Jobs on a cron expression.
type Scheduler struct {
	cron *cron.Cron
	log  *slog.Logger
}

// New creates a new Scheduler.
func New(log *slog.Logger) *Scheduler {
	return &Scheduler{log: log}
}

// Register adds a job to run on the given cron expression.
// If the expression has 6 fields (with seconds), cron.WithSeconds() is used.
func (s *Scheduler) Register(expr string, job Job) error {
	fields := strings.Fields(expr)
	var c *cron.Cron
	if len(fields) == 6 {
		c = cron.New(cron.WithSeconds())
	} else {
		c = cron.New()
	}
	s.cron = c

	_, err := s.cron.AddFunc(expr, func() {
		ctx := context.Background()
		if err := job.Run(ctx); err != nil {
			s.log.Error("scheduler: job failed", "error", err)
		}
	})
	return err
}

// Start begins the scheduler's background loop.
func (s *Scheduler) Start() {
	s.cron.Start()
	s.log.Info("scheduler: started")
}

// Stop signals the cron runner to stop and returns a context that is Done
// when all running jobs have finished.
func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}
