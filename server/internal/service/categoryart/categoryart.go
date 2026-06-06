// Package categoryart owns the "fill Twitch game metadata on the categories
// table" side of the catalog. The sync is separated from the main
// category upsert path (see repository.UpsertCategory) because Helix
// returns game_id + game_name on /streams but NOT the box art — art
// and igdb_id live on /helix/games, which requires a second round-trip.
//
// Two entry points:
//
//   - Enrich(id): single-category, eager. Called by streammeta.Hydrator
//     right after the category is first observed live so the dashboard
//     card sees art within the same webhook handler.
//
//   - SyncMissing(): batch, scheduled. Fallback for categories that
//     were upserted while /helix/games was degraded, or categories
//     whose art or igdb_id Twitch has since nullified (rare).
//
// Both paths write through repository.UpdateCategoryGameMetadata — the
// dedicated "refresh Twitch metadata only" SQL query, separate from
// UpsertCategory. This separation lets the Hydrator's webhook-path
// UpsertCategory be safe to call without knowing the art or igdb_id values:
// it won't wipe anything.
package categoryart

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// Helix /helix/games accepts up to 100 IDs per request. Batching to
// this limit keeps the scheduled sync to the minimum number of Helix
// round trips.
const maxGamesPerHelixCall = 100

const gameMetadataRetryInterval = 7 * 24 * time.Hour

// gamesFetcher is the narrow slice of *twitch.Client we need. Kept
// as an interface so tests can supply a fake without httptest;
// *twitch.Client satisfies it in production.
type gamesFetcher interface {
	GetGames(ctx context.Context, params *twitch.GetGamesParams) ([]twitch.Game, error)
}

// Service owns the Twitch category metadata sync surface. Lightweight — one
// instance per process is enough.
type Service struct {
	repo   repository.Repository
	twitch gamesFetcher
	log    *slog.Logger
	now    func() time.Time
}

type SyncResult struct {
	Updated     int
	IGDBUpdated int
	Checked     int
}

// New builds a Service. tc may be nil; in that case Enrich and
// SyncMissing return nil without doing anything, mirroring the
// degraded-mode behavior elsewhere in the codebase.
func New(repo repository.Repository, tc gamesFetcher, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: tc, log: log.With("domain", "categoryart"), now: time.Now}
}

// Enrich fetches Twitch game metadata for a single category ID and writes it
// through UpdateCategoryGameMetadata. Returns the Helix or repo error
// directly — the caller decides whether to log, retry, or ignore.
// Callers on the webhook path (Hydrator) typically log-and-continue
// so the primary flow survives Helix hiccups.
//
// An unrecognized category ID (Twitch returned no match for a game
// that's been merged/removed) is NOT an error: Enrich returns nil
// with a Debug log, marking the row checked so it is not retried immediately.
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
		return s.repo.MarkCategoryGameMetadataChecked(ctx, categoryID)
	}
	return s.writeGameMetadata(ctx, games[0])
}

// SyncMissing walks every category in the DB with missing Twitch-side game
// metadata, batches them into /helix/games calls, and writes the returned
// values back. Returns counts for changed metadata, changed IGDB ids, and rows
// whose /games lookup was checked but produced no new values.
//
// Unlike Enrich, failures here abort the batch — if Helix is down
// the scheduler should see the error and mark the task failed. Per-
// game failures within a successful batch are logged and skipped.
func (s *Service) SyncMissing(ctx context.Context) (SyncResult, error) {
	if s == nil || s.twitch == nil {
		return SyncResult{}, nil
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	missing, err := s.repo.ListCategoriesMissingGameMetadata(ctx, now().Add(-gameMetadataRetryInterval))
	if err != nil {
		return SyncResult{}, fmt.Errorf("list categories missing game metadata: %w", err)
	}
	if len(missing) == 0 {
		return SyncResult{}, nil
	}

	ids := make([]string, 0, len(missing))
	categoriesByID := make(map[string]repository.Category, len(missing))
	for _, c := range missing {
		if c.ID != "" {
			ids = append(ids, c.ID)
			categoriesByID[c.ID] = c
		}
	}

	result := SyncResult{}
	for start := 0; start < len(ids); start += maxGamesPerHelixCall {
		end := min(start+maxGamesPerHelixCall, len(ids))
		batch := ids[start:end]
		games, err := s.twitch.GetGames(ctx, &twitch.GetGamesParams{ID: batch})
		if err != nil {
			return result, fmt.Errorf("helix get games (batch %d-%d): %w", start, end, err)
		}
		returned := make(map[string]struct{}, len(games))
		for i := range games {
			g := &games[i]
			if g.ID == "" {
				continue
			}
			returned[g.ID] = struct{}{}
			category, ok := categoriesByID[g.ID]
			if !ok {
				continue
			}
			updated, igdbUpdated := gameMetadataChanges(category, *g)
			if updated {
				if err := s.writeGameMetadata(ctx, *g); err != nil {
					s.log.Warn("update category game metadata",
						"category_id", g.ID, "error", err)
					continue
				}
				result.Updated++
				result.Checked++
				if igdbUpdated {
					result.IGDBUpdated++
				}
				continue
			}
			if err := s.repo.MarkCategoryGameMetadataChecked(ctx, g.ID); err != nil {
				s.log.Warn("update category game metadata",
					"category_id", g.ID, "error", err)
				continue
			}
			result.Checked++
		}
		for _, id := range batch {
			if _, ok := returned[id]; ok {
				continue
			}
			if err := s.repo.MarkCategoryGameMetadataChecked(ctx, id); err != nil {
				s.log.Warn("mark category game metadata checked",
					"category_id", id, "error", err)
				continue
			}
			result.Checked++
		}
	}
	return result, nil
}

// writeGameMetadata is the single point where we convert a Helix game payload
// into stored category metadata. Twitch returns box-art URLs with
// `{width}x{height}` placeholders; we keep them as-is so the frontend picks the
// display size.
func (s *Service) writeGameMetadata(ctx context.Context, game twitch.Game) error {
	if game.ID == "" {
		return nil
	}
	game.BoxArtURL = strings.TrimSpace(game.BoxArtURL)
	game.IGDBID = strings.TrimSpace(game.IGDBID)
	if game.BoxArtURL == "" && game.IGDBID == "" {
		return s.repo.MarkCategoryGameMetadataChecked(ctx, game.ID)
	}
	return s.repo.UpdateCategoryGameMetadata(ctx, game.ID, game.BoxArtURL, game.IGDBID)
}

func gameMetadataChanges(category repository.Category, game twitch.Game) (updated bool, igdbUpdated bool) {
	if boxArtURL := strings.TrimSpace(game.BoxArtURL); boxArtURL != "" {
		if category.BoxArtURL == nil || *category.BoxArtURL != boxArtURL {
			updated = true
		}
	}
	if igdbID := strings.TrimSpace(game.IGDBID); igdbID != "" {
		if category.IGDBID == nil || *category.IGDBID != igdbID {
			updated = true
			igdbUpdated = true
		}
	}
	return updated, igdbUpdated
}
