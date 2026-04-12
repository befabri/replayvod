// Package scheduler runs registered background jobs on an interval.
//
// Model: each registered Task has a name, description, interval, and a
// Run func. On startup the scheduler upserts the task into the `tasks`
// DB table so description/interval stay in sync with code, preserving
// runtime state (last_run_at, last_status, next_run_at). A single
// ticker goroutine wakes every pollInterval, asks the repo for
// "due" tasks (next_run_at <= now AND is_enabled), and runs them
// concurrency-1 per task (one task can't run in parallel with itself)
// but many tasks can run simultaneously across the process.
//
// An operator flipping is_enabled in the dashboard pauses a task without
// a restart; bumping next_run_at to now (via SetTaskNextRun) schedules a
// one-shot immediate run.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

// RunFunc is the body of a scheduled task. Returning an error marks
// the run as failed; returning nil marks it success. The error message
// is stored on tasks.last_error for the dashboard.
type RunFunc func(ctx context.Context) error

// Task is a scheduler registration: a unique name, the cadence in
// seconds (0 = manual-only), a human-readable description, and the
// body.
type Task struct {
	Name            string
	Description     string
	IntervalSeconds int64
	Run             RunFunc
}

// Service is the scheduler runtime. Safe for concurrent registration
// before Start, and safe for concurrent Stop while tasks are running.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
	// bus is optional — nil means task status transitions don't fan
	// out to SSE subscribers. Useful for tests that build a scheduler
	// without constructing the full bus graph.
	bus *eventbus.Buses

	mu      sync.Mutex
	tasks   map[string]*Task
	running map[string]struct{} // in-flight task names
	poll    time.Duration
	stopCh  chan struct{}
	stopped bool
	wg      sync.WaitGroup
}

// NewService builds a scheduler. pollInterval is how often the ticker
// checks for due tasks — 15s is a reasonable default; a task with a
// 1-minute interval will slip up to pollInterval on its firing time.
// bus may be nil in tests or degraded modes.
func NewService(repo repository.Repository, log *slog.Logger, pollInterval time.Duration, bus *eventbus.Buses) *Service {
	if pollInterval <= 0 {
		pollInterval = 15 * time.Second
	}
	return &Service{
		repo:    repo,
		log:     log.With("domain", "scheduler"),
		bus:     bus,
		tasks:   make(map[string]*Task),
		running: make(map[string]struct{}),
		poll:    pollInterval,
		stopCh:  make(chan struct{}),
	}
}

// Register adds a task to the scheduler. Call Register before Start;
// registering after Start is allowed but the new task's interval won't
// be reflected in the DB until the next startup.
func (s *Service) Register(t Task) error {
	if t.Name == "" {
		return fmt.Errorf("scheduler: task name required")
	}
	if t.Run == nil {
		return fmt.Errorf("scheduler: task %q has no Run func", t.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[t.Name]; exists {
		return fmt.Errorf("scheduler: task %q already registered", t.Name)
	}
	s.tasks[t.Name] = &t
	return nil
}

// Start persists every registered task's metadata to the DB (UpsertTask
// preserves runtime state) and launches the ticker loop. Start returns
// after the initial persistence so callers can surface any DB errors;
// the loop runs in a goroutine.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	for _, t := range s.tasks {
		if _, err := s.repo.UpsertTask(ctx, t.Name, t.Description, t.IntervalSeconds); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("scheduler: register task %q: %w", t.Name, err)
		}
	}
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop()
	return nil
}

// Stop signals the scheduler to exit and blocks until in-flight tasks
// return. Safe to call more than once; subsequent calls are no-ops.
func (s *Service) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()

	// Tick once at startup so a long poll interval doesn't delay the
	// first run of a "due" task (useful after an outage where every
	// task is overdue).
	s.tick()

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Service) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	due, err := s.repo.ListDueTasks(ctx)
	if err != nil {
		s.log.Error("list due tasks", "error", err)
		return
	}

	for i := range due {
		name := due[i].Name
		s.mu.Lock()
		t, known := s.tasks[name]
		if !known {
			// Unknown task name in the DB — probably an old task whose
			// code got removed. Leave the row alone so a redeploy of
			// the prior code picks it back up; skip running.
			s.mu.Unlock()
			s.log.Warn("due task has no registered runner; skipping", "name", name)
			continue
		}
		if _, busy := s.running[name]; busy {
			s.mu.Unlock()
			continue // already running, wait for next tick
		}
		s.running[name] = struct{}{}
		s.mu.Unlock()

		s.wg.Add(1)
		go s.runOne(t)
	}
}

func (s *Service) runOne(t *Task) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.running, t.Name)
		s.mu.Unlock()
	}()

	// Each run gets its own context so a slow task can't block the
	// next tick. 10-minute ceiling; tasks that reliably exceed this
	// should override via their own time.WithTimeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := s.repo.MarkTaskRunning(ctx, t.Name); err != nil {
		s.log.Error("mark task running", "name", t.Name, "error", err)
	}
	s.publishStatus(t.Name, repository.TaskStatusRunning, 0, "")
	start := time.Now()

	defer func() {
		if r := recover(); r != nil {
			s.log.Error("task panicked", "name", t.Name, "panic", r)
			s.markFailed(t.Name, start, fmt.Errorf("panic: %v", r))
		}
	}()

	err := t.Run(ctx)
	if err != nil {
		s.log.Warn("task run failed", "name", t.Name, "error", err)
		s.markFailed(t.Name, start, err)
		return
	}
	s.markSuccess(t.Name, start)
}

// publishStatus fans a task lifecycle change onto the SSE bus. No-op
// when the scheduler was constructed without a bus (tests).
func (s *Service) publishStatus(name, status string, durationMs int64, errMsg string) {
	if s.bus == nil {
		return
	}
	s.bus.TaskStatus.Publish(eventbus.TaskStatusEvent{
		Name:           name,
		Status:         status,
		DurationMs:     durationMs,
		Error:          errMsg,
		TransitionedAt: time.Now().UTC(),
	})
}

func (s *Service) markSuccess(name string, start time.Time) {
	// Use a short detached context — we want this write to land even
	// if the parent ctx is cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dur := time.Since(start).Milliseconds()
	s.publishStatus(name, repository.TaskStatusSuccess, dur, "")
	if err := s.repo.MarkTaskSuccess(ctx, name, dur); err != nil {
		s.log.Error("mark task success", "name", name, "error", err)
	}
	// Successful runs land as info event_logs for operator visibility
	// on the events page. Keeps the dashboard's task-activity feed
	// useful without spamming slog: the retention task prunes
	// debug/info rows so the volume is bounded.
	EmitEventLog(ctx, s.repo, s.bus, s.log,
		"task", "run_success", repository.EventLogSeverityInfo,
		fmt.Sprintf("task %s completed in %dms", name, dur),
		map[string]any{"task": name, "duration_ms": dur},
	)
}

func (s *Service) markFailed(name string, start time.Time, runErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dur := time.Since(start).Milliseconds()
	errMsg := runErr.Error()
	s.publishStatus(name, repository.TaskStatusFailed, dur, errMsg)
	if err := s.repo.MarkTaskFailed(ctx, name, dur, errMsg); err != nil {
		s.log.Error("mark task failed", "name", name, "error", err)
	}
	// Failure writes an error event_log — these survive the info-
	// retention sweep and are the first thing operators look at on
	// the dashboard's events page during an incident.
	EmitEventLog(ctx, s.repo, s.bus, s.log,
		"task", "run_failed", repository.EventLogSeverityError,
		fmt.Sprintf("task %s failed: %s", name, errMsg),
		map[string]any{"task": name, "duration_ms": dur, "error": errMsg},
	)
}
