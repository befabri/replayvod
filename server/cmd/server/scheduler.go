package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/scheduler"
)

// startScheduler keeps registry construction, reconciliation, and execution in
// order. Even a globally disabled scheduler must reconcile previous task rows.
func startScheduler(ctx context.Context, cfg *config.Config, repo repository.Repository, deps scheduler.StandardTaskDeps, log *slog.Logger, bus *eventbus.Buses) (*scheduler.Service, error) {
	registry, err := scheduler.NewRegistry(scheduler.BuildStandardTasks(cfg, repo, deps, log))
	if err != nil {
		return nil, fmt.Errorf("build scheduler registry: %w", err)
	}
	if err := scheduler.ReconcileTasks(ctx, repo, registry); err != nil {
		return nil, err
	}
	svc := scheduler.NewService(repo, registry, log, 15*time.Second, bus)
	if err := svc.Start(ctx); err != nil {
		svc.Stop()
		return nil, fmt.Errorf("start scheduler: %w", err)
	}
	return svc, nil
}
