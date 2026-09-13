package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type schedulerStartupRepo struct {
	repository.Repository
	txErr          error
	afterCommit    func()
	transactions   atomic.Int32
	polls          atomic.Int32
	committed      atomic.Bool
	polledTooEarly atomic.Bool
}

func (r *schedulerStartupRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	r.transactions.Add(1)
	if r.txErr != nil {
		return r.txErr
	}
	if err := r.Repository.WithTx(ctx, fn); err != nil {
		return err
	}
	r.committed.Store(true)
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return nil
}

func (r *schedulerStartupRepo) ListDueTasks(ctx context.Context) ([]repository.Task, error) {
	r.polls.Add(1)
	if !r.committed.Load() {
		r.polledTooEarly.Store(true)
	}
	return r.Repository.ListDueTasks(ctx)
}

func TestStartSchedulerReconcilesBeforeExecutionEvenWhenDisabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			repo := &schedulerStartupRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t))}
			ctx := t.Context()
			if _, err := repo.UpsertTask(ctx, "previous-task", "previous description", 60); err != nil {
				t.Fatal(err)
			}
			if err := repo.ClaimTask(ctx, "previous-task", "previous-execution"); err != nil {
				t.Fatal(err)
			}
			if err := repo.SettleTask(ctx, "previous-task", "previous-execution", repository.TaskStatusSuccess, 42, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SetTaskEnabled(ctx, "previous-task", false); err != nil {
				t.Fatal(err)
			}
			before, err := repo.GetTask(ctx, "previous-task")
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: enabled}}}
			ran := make(chan struct{}, 1)
			svc, err := startScheduler(ctx, cfg, repo, scheduler.StandardTaskDeps{PlaybackCacheReconcile: func(context.Context) error {
				ran <- struct{}{}
				return nil
			}}, slog.New(slog.DiscardHandler), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(svc.Stop)
			if enabled {
				select {
				case <-ran:
				case <-time.After(2 * time.Second):
					t.Fatal("configured playback maintenance did not start")
				}
			}
			svc.Stop()
			if !enabled {
				select {
				case <-ran:
					t.Fatal("global disablement allowed maintenance to run")
				default:
				}
				if repo.polls.Load() != 0 {
					t.Fatal("disabled scheduler polled")
				}
			}
			if repo.transactions.Load() != 1 || repo.polledTooEarly.Load() {
				t.Fatalf("startup order: transactions=%d, polled before commit=%v", repo.transactions.Load(), repo.polledTooEarly.Load())
			}
			after, err := repo.GetTask(ctx, "previous-task")
			before.IsAvailable = false
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("obsolete task lost history or operator pause: before=%+v, after=%+v, error=%v", before, after, err)
			}
		})
	}
}

func TestStartSchedulerReconciliationFailureStartsNoWorker(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			boom := errors.New("database unavailable")
			repo := &schedulerStartupRepo{txErr: boom}
			cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: enabled, TokenCleanupIntervalMinutes: 60}}}
			svc, err := startScheduler(t.Context(), cfg, repo, scheduler.StandardTaskDeps{}, slog.New(slog.DiscardHandler), nil)
			if svc != nil {
				svc.Stop()
				t.Fatal("startup returned a scheduler after reconciliation failed")
			}
			if !errors.Is(err, boom) || repo.transactions.Load() != 1 || repo.polls.Load() != 0 {
				t.Fatalf("startup failure: error=%v transactions=%d polls=%d", err, repo.transactions.Load(), repo.polls.Load())
			}
		})
	}
}

func TestStartSchedulerValidatesBeforeDatabaseWrites(t *testing.T) {
	repo := &schedulerStartupRepo{txErr: errors.New("database must not be reached")}
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{
		Enabled: true, TokenCleanupIntervalMinutes: math.MaxInt32/60 + 1,
	}}}
	svc, err := startScheduler(t.Context(), cfg, repo, scheduler.StandardTaskDeps{}, slog.New(slog.DiscardHandler), nil)
	if svc != nil {
		svc.Stop()
		t.Fatal("invalid registry produced a scheduler")
	}
	if err == nil || repo.transactions.Load() != 0 {
		t.Fatalf("validation occurred after database access: error=%v transactions=%d", err, repo.transactions.Load())
	}
}

func TestStartSchedulerCancellationAfterReconciliationStartsNoWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repo := &schedulerStartupRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t)), afterCommit: cancel}
	cfg := &config.Config{App: config.AppConfig{Scheduler: config.SchedulerConfig{Enabled: true, TokenCleanupIntervalMinutes: 60}}}
	svc, err := startScheduler(ctx, cfg, repo, scheduler.StandardTaskDeps{}, slog.New(slog.DiscardHandler), nil)
	if svc != nil {
		svc.Stop()
		t.Fatal("cancelled startup returned a scheduler")
	}
	if !errors.Is(err, context.Canceled) || !repo.committed.Load() || repo.polls.Load() != 0 {
		t.Fatalf("cancelled startup: error=%v committed=%v polls=%d", err, repo.committed.Load(), repo.polls.Load())
	}
}
