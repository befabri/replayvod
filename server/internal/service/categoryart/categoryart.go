// Package categoryart owns the "fill box_art_url on the categories
// table" side of the catalog. The sync is separated from the main
// category upsert path (see repository.UpsertCategory) because Helix
// returns game_id + game_name on /streams but NOT the box art — art
// lives on /helix/games, which requires a second round-trip.
//
// Two entry points:
//
//   - Enrich(id): single-category, eager. Called by streammeta.Hydrator
//     right after the category is first observed live so the dashboard
//     card sees art within the same webhook handler.
//
//   - SyncMissing(): batch, scheduled. Fallback for categories that
//     were upserted while /helix/games was degraded, or categories
//     whose art Twitch has since nullified (rare).
//
// Both paths write through repository.UpdateCategoryBoxArt — the
// dedicated "refresh art only" SQL query, separate from UpsertCategory
// which only touches the name on conflict. This separation is what
// lets the Hydrator's webhook-path UpsertCategory be safe to call
// without knowing the art value: it won't wipe anything.
package categoryart

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// Helix /helix/games accepts up to 100 IDs per request. Batching to
// this limit keeps the scheduled sync to the minimum number of Helix
// round trips.
const maxGamesPerHelixCall = 100

// gamesFetcher is the narrow slice of *twitch.Client we need. Kept
// as an interface so tests can supply a fake without httptest;
// *twitch.Client satisfies it in production.
type gamesFetcher interface {
	GetGames(ctx context.Context, params *twitch.GetGamesParams) ([]twitch.Game, error)
}

// Service owns the box-art sync surface. Lightweight — one instance
// per process is enough.
type Service struct {
	repo   repository.Repository
	twitch gamesFetcher
	log    *slog.Logger
}

// New builds a Service. tc may be nil; in that case Enrich and
// SyncMissing return nil without doing anything, mirroring the
// degraded-mode behavior elsewhere in the codebase.
func New(repo repository.Repository, tc gamesFetcher, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: tc, log: log.With("domain", "categoryart")}
}

// Enrich fetches the box art for a single category ID and writes it
// through UpdateCategoryBoxArt. Returns the Helix or repo error
// directly — the caller decides whether to log, retry, or ignore.
// Callers on the webhook path (Hydrator) typically log-and-continue
// so the primary flow survives Helix hiccups.
//
// An unrecognized category ID (Twitch returned no match for a game
// that's been merged/removed) is NOT an error: Enrich returns nil
// with a Debug log, leaving the row untouched so the scheduled
// backfill doesn't keep retrying a known-dead ID.
//
// Callers that already know the category has box art should skip
// this call rather than always-fetch; the Hydrator checks the
// returned row from UpsertCategory before invoking.
func (s *Service) Enrich(ctx context.Context, categoryID string) error {
	if s == nil || s.twitch == nil || categoryID == "" {
		return nil
	}
	games, err := s.twitch.GetGames(ctx, &twitch.GetGamesParams{ID: []string{categoryID}})
	if err != nil {
		return fmt.Errorf("helix get games: %w", err)
	}
	if len(games) == 0 {
		s.log.Debug("helix returned no game for category", "category_id", categoryID)
		return nil
	}
	return s.writeBoxArt(ctx, categoryID, games[0].BoxArtURL)
}

// SyncMissing walks every category in the DB with a NULL or empty
// box_art_url, batches them into /helix/games calls, and writes the
// returned URLs back. Returns the number of categories updated so
// the scheduled task can log a useful summary.
//
// Unlike Enrich, failures here abort the batch — if Helix is down
// the scheduler should see the error and mark the task failed. Per-
// game failures within a successful batch are logged and skipped.
func (s *Service) SyncMissing(ctx context.Context) (int, error) {
	if s == nil || s.twitch == nil {
		return 0, nil
	}
	missing, err := s.repo.ListCategoriesMissingBoxArt(ctx)
	if err != nil {
		return 0, fmt.Errorf("list categories missing box art: %w", err)
	}
	if len(missing) == 0 {
		return 0, nil
	}

	ids := make([]string, 0, len(missing))
	for _, c := range missing {
		if c.ID != "" {
			ids = append(ids, c.ID)
		}
	}

	synced := 0
	for start := 0; start < len(ids); start += maxGamesPerHelixCall {
		end := min(start+maxGamesPerHelixCall, len(ids))
		batch := ids[start:end]
		games, err := s.twitch.GetGames(ctx, &twitch.GetGamesParams{ID: batch})
		if err != nil {
			return synced, fmt.Errorf("helix get games (batch %d-%d): %w", start, end, err)
		}
		for i := range games {
			g := &games[i]
			if g.BoxArtURL == "" {
				continue
			}
			if err := s.writeBoxArt(ctx, g.ID, g.BoxArtURL); err != nil {
				s.log.Warn("update category box art",
					"category_id", g.ID, "error", err)
				continue
			}
			synced++
		}
	}
	return synced, nil
}

// writeBoxArt is the single point where we convert a Helix box-art
// URL template into the stored value and call the repo. Twitch
// returns URLs with `{width}x{height}` placeholders — we keep them
// as-is so the frontend picks the display size.
func (s *Service) writeBoxArt(ctx context.Context, categoryID, boxArtURL string) error {
	if boxArtURL == "" {
		return nil
	}
	return s.repo.UpdateCategoryBoxArt(ctx, categoryID, boxArtURL)
}
