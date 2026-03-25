// Package scheduler wraps the robfig/cron library to run autodoc jobs on a
// cron schedule.
//
// [Scheduler] provides concurrent-run protection: only one job executes at a
// time. [Register] supports both 5-field (minute-level) and 6-field
// (second-level) cron expressions. [Stop] returns a context that is Done once
// all in-flight jobs have finished, enabling graceful shutdown.
package scheduler
