package categorymeta

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/igdb"
	"github.com/befabri/replayvod/server/internal/repository"
)

// IGDB accepts large limits, but keeping the batch size aligned with the
// existing Twitch /games sync makes scheduler behavior predictable and keeps
// request bodies small.
const maxGamesPerIGDBCall = 100

const (
	descriptionRetryInterval = 7 * 24 * time.Hour
	igdbBatchDelay           = 250 * time.Millisecond
)

type gamesFetcher interface {
	GetGames(ctx context.Context, igdbIDs []int64) ([]igdb.Game, error)
}

type Service struct {
	repo       repository.Repository
	igdb       gamesFetcher
	log        *slog.Logger
	batchDelay time.Duration
}

func New(repo repository.Repository, igdbClient gamesFetcher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, igdb: igdbClient, log: log.With("domain", "categorymeta"), batchDelay: igdbBatchDelay}
}

// SyncMissing fills category descriptions from IGDB for rows that already have
// a Twitch-provided igdb_id. It returns the number of categories updated.
func (s *Service) SyncMissing(ctx context.Context) (int, error) {
	if s == nil || s.igdb == nil {
		return 0, nil
	}
	checkedBefore := time.Now().Add(-descriptionRetryInterval)
	missing, err := s.repo.ListCategoriesMissingDescription(ctx, checkedBefore)
	if err != nil {
		return 0, fmt.Errorf("list categories missing description: %w", err)
	}
	if len(missing) == 0 {
		return 0, nil
	}

	ids, categoriesByIGDBID, invalid := s.parseIGDBIDs(missing)
	for _, category := range invalid {
		s.markDescriptionChecked(ctx, category, 0)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	synced := 0
	for start := 0; start < len(ids); start += maxGamesPerIGDBCall {
		if start > 0 {
			if err := sleepContext(ctx, s.batchDelay); err != nil {
				return synced, err
			}
		}
		end := min(start+maxGamesPerIGDBCall, len(ids))
		batch := ids[start:end]
		games, err := s.igdb.GetGames(ctx, batch)
		if err != nil {
			return synced, fmt.Errorf("igdb get games (batch %d-%d): %w", start, end, err)
		}
		returned := make(map[int64]struct{}, len(games))
		for _, game := range games {
			returned[game.ID] = struct{}{}
			categories := categoriesByIGDBID[game.ID]
			if len(categories) == 0 {
				continue
			}
			description := descriptionFor(game)
			if description == "" {
				for _, category := range categories {
					s.markDescriptionChecked(ctx, category, game.ID)
				}
				continue
			}
			for _, category := range categories {
				if err := s.repo.UpdateCategoryDescription(ctx, category.ID, description); err != nil {
					s.log.Warn("update category description",
						"category_id", category.ID,
						"igdb_id", game.ID,
						"error", err)
					continue
				}
				synced++
			}
		}
		for _, id := range batch {
			if _, ok := returned[id]; ok {
				continue
			}
			for _, category := range categoriesByIGDBID[id] {
				s.markDescriptionChecked(ctx, category, id)
			}
		}
	}
	return synced, nil
}

func (s *Service) parseIGDBIDs(categories []repository.Category) ([]int64, map[int64][]repository.Category, []repository.Category) {
	ids := make([]int64, 0, len(categories))
	categoriesByIGDBID := make(map[int64][]repository.Category, len(categories))
	invalid := make([]repository.Category, 0)
	seen := make(map[int64]struct{}, len(categories))
	for _, category := range categories {
		if category.IGDBID == nil {
			continue
		}
		raw := strings.TrimSpace(*category.IGDBID)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			s.log.Warn("skip category with invalid igdb_id", "category_id", category.ID, "igdb_id", raw)
			invalid = append(invalid, category)
			continue
		}
		categoriesByIGDBID[id] = append(categoriesByIGDBID[id], category)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, categoriesByIGDBID, invalid
}

func (s *Service) markDescriptionChecked(ctx context.Context, category repository.Category, igdbID int64) {
	if err := s.repo.MarkCategoryDescriptionChecked(ctx, category.ID); err != nil {
		s.log.Warn("mark category description checked",
			"category_id", category.ID,
			"igdb_id", igdbID,
			"error", err)
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func descriptionFor(game igdb.Game) string {
	if summary := strings.TrimSpace(game.Summary); summary != "" {
		return summary
	}
	return strings.TrimSpace(game.Storyline)
}
