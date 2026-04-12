package main

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// fetchLogRecorder bridges the twitch client to the repository's fetch log table.
// Lives in main to avoid an import cycle between twitch and repository.
type fetchLogRecorder struct {
	repo repository.Repository
	log  *slog.Logger
}

func (r *fetchLogRecorder) RecordFetch(ctx context.Context, entry twitch.FetchLogEntry) {
	var errStr *string
	if entry.Error != "" {
		errStr = &entry.Error
	}
	if err := r.repo.CreateFetchLog(ctx, &repository.FetchLogInput{
		UserID:        entry.UserID,
		FetchType:     entry.FetchType,
		BroadcasterID: entry.BroadcasterID,
		Status:        entry.Status,
		Error:         errStr,
		DurationMs:    entry.DurationMs,
	}); err != nil {
		r.log.Warn("failed to record fetch log", "error", err)
	}
}
