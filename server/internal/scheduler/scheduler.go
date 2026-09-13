// Package scheduler executes due tasks with ownership held through settlement.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/eventlog"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/google/uuid"
)

// Service executes reconciled task handlers and retains ownership through durable settlement.
type Service struct {
	repo     repository.Repository
	registry Registry
	log      *slog.Logger
	bus      *eventbus.Buses

	mu        sync.Mutex
	runner    *background.Runner
	poll      time.Duration
	stopped   bool
	started   bool
	wg        sync.WaitGroup
	runCtx    context.Context
	cancelRun context.CancelFunc
}

// NewService binds a registry to repo; callers must reconcile that registry
// before Start and call Stop before releasing its dependencies.
func NewService(repo repository.Repository, registry Registry, log *slog.Logger, pollInterval time.Duration, bus *eventbus.Buses) *Service {
	if pollInterval <= 0 {
		pollInterval = 15 * time.Second
	}
	return &Service{
		repo:     repo,
		registry: registry,
		log:      log.With("domain", "scheduler"),
		bus:      bus,
		runner:   background.New(nil),
		poll:     pollInterval,
	}
}

// Start launches execution without writing task definitions. ReconcileTasks
// must have succeeded with the same registry first. It returns immediately;
// cancellation stops polling and interrupts jobs, and Stop waits for shutdown.
// An empty registry starts no goroutine.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.started {
		return fmt.Errorf("scheduler: already started or stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.runCtx, s.cancelRun = context.WithCancel(ctx)
	s.started = true
	if s.registry.Len() == 0 {
		return nil
	}
	s.wg.Add(1)
	go s.loop()
	return nil
}

// Stop closes admission, cancels execution, and joins every task and settlement.
// Concurrent callers all wait for the same shutdown.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		if s.cancelRun != nil {
			s.cancelRun()
		}
		s.runner.Stop()
	}
	s.mu.Unlock()
	// Every caller waits, including a concurrent second Stop.
	s.wg.Wait()
	s.runner.Stop()
	_ = s.runner.Wait(context.Background())
}

func (s *Service) loop() {
	defer s.wg.Done()
	defer s.runner.Stop()

	// Run overdue tasks immediately after startup, without waiting for the poll interval.
	s.tick()

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()

	for {
		select {
		case <-s.runCtx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Service) tick() {
	if s.runCtx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.runCtx, 30*time.Second)
	defer cancel()

	due, err := s.repo.ListDueTasks(ctx)
	if err != nil {
		if s.runCtx.Err() != nil {
			return
		}
		s.log.Error("list due tasks", "error", err)
		return
	}

	for i := range due {
		name := due[i].Name
		if s.runCtx.Err() != nil {
			return
		}
		t, known := s.registry.tasks[name]
		if !known {
			// Database rows inserted after reconciliation can have no local handler.
			s.log.Warn("due task has no registered runner; skipping", "name", name)
			continue
		}
		if err := s.runner.Start("task", name, func(runCtx context.Context) error {
			s.runOne(runCtx, t)
			return nil
		}, nil); err != nil && !errors.Is(err, background.ErrBusy) && !errors.Is(err, background.ErrStopped) {
			s.log.Error("admit task", "name", name, "error", err)
		}
	}
}

func (s *Service) runOne(runCtx context.Context, task Task) {
	executionID := uuid.NewString()
	ctx, cancel := context.WithTimeout(runCtx, 10*time.Minute)
	defer cancel()
	// A failed claim starts no effects. The same token resolves lost responses
	// without clearing a run-now request received after the first claim committed.
	if err := s.persist(runCtx, func(writeCtx context.Context) error { return s.repo.ClaimTask(writeCtx, task.Name, executionID) }); err != nil {
		s.log.Error("claim task", "name", task.Name, "error", err)
		return
	}
	s.publishStatus(task.Name, repository.TaskStatusRunning, 0, "")
	start := time.Now()
	runErr := background.Call(ctx, task.Run)
	status, message, severity := repository.TaskStatusSuccess, "", repository.EventLogSeverityInfo
	if runErr != nil {
		status, message, severity = repository.TaskStatusFailed, runErr.Error(), repository.EventLogSeverityError
		if runCtx.Err() != nil && errors.Is(runErr, context.Canceled) {
			status, message, severity = repository.TaskStatusInterrupted, "", repository.EventLogSeverityInfo
		}
	}
	duration := time.Since(start).Milliseconds()
	if err := s.persist(runCtx, func(writeCtx context.Context) error {
		return s.repo.SettleTask(writeCtx, task.Name, executionID, status, duration, message)
	}); err != nil {
		s.log.Error("task settlement unresolved", "name", task.Name, "execution_id", executionID, "error", err)
		return
	}
	s.publishStatus(task.Name, status, duration, message)
	logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer logCancel()
	eventlog.Emit(logCtx, s.repo, s.bus, s.log, "task", "run_"+status, severity,
		fmt.Sprintf("task %s %s in %dms", task.Name, status, duration),
		map[string]any{"task": task.Name, "duration_ms": duration, "error": message})
}

func (s *Service) persist(ctx context.Context, write func(context.Context) error) error {
	delay := 100 * time.Millisecond
	for {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := write(writeCtx)
		cancel()
		if err == nil || errors.Is(err, repository.ErrStaleExecution) || errors.Is(err, repository.ErrNotFound) || ctx.Err() != nil {
			return err
		}
		s.log.Warn("task persistence deferred", "error", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		delay = min(2*delay, 5*time.Second)
	}
}

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
