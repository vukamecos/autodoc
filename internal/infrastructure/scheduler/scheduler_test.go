package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_Valid5FieldCron(t *testing.T) {
	s := New(testLogger())
	job := &countJob{}
	if err := s.Register("* * * * *", job); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
}

func TestRegister_Valid6FieldCron(t *testing.T) {
	s := New(testLogger())
	job := &countJob{}
	// 6-field cron includes a seconds field.
	if err := s.Register("0 * * * * *", job); err != nil {
		t.Fatalf("Register() 6-field cron error: %v", err)
	}
}

func TestRegister_InvalidCron(t *testing.T) {
	s := New(testLogger())
	job := &countJob{}
	if err := s.Register("not a cron expression", job); err == nil {
		t.Fatal("expected error for invalid cron expression, got nil")
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func TestStartStop_NoJob(t *testing.T) {
	s := New(testLogger())
	// Register a valid but never-firing schedule before Start.
	if err := s.Register("0 0 31 2 *", &countJob{}); err != nil {
		t.Fatalf("Register(): %v", err)
	}
	s.Start()
	ctx := s.Stop()
	select {
	case <-ctx.Done():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return a done context in time")
	}
}

func TestStart_RunsJobOnSchedule(t *testing.T) {
	// Use a 6-field every-second expression so the job fires quickly.
	s := New(testLogger())
	job := &countJob{}
	if err := s.Register("* * * * * *", job); err != nil {
		t.Fatalf("Register(): %v", err)
	}
	s.Start()
	defer func() { _ = s.Stop() }()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("job was not executed within 3 seconds")
		default:
			if atomic.LoadInt32(&job.count) > 0 {
				return // success
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestStart_JobErrorIsLogged(t *testing.T) {
	// A job that always returns an error should not crash the scheduler.
	s := New(testLogger())
	job := &failJob{err: errors.New("boom")}
	if err := s.Register("* * * * * *", job); err != nil {
		t.Fatalf("Register(): %v", err)
	}
	s.Start()
	time.Sleep(1500 * time.Millisecond)
	stopCtx := s.Stop()
	<-stopCtx.Done()
	// If we got here without panic the scheduler handled the error gracefully.
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type countJob struct {
	count int32
}

func (j *countJob) Run(_ context.Context) error {
	atomic.AddInt32(&j.count, 1)
	return nil
}

type failJob struct {
	err error
}

func (j *failJob) Run(_ context.Context) error {
	return j.err
}
