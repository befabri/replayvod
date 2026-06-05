// Package category owns the category domain: business logic (Service)
// and the tRPC adapter (Handler). Categories are populated by stream
// enrichment on stream.online and by on-demand Twitch category searches
// from scheduling; this surface is read-only from the UI's perspective.
package category

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type categoryRepo interface {
	GetCategory(ctx context.Context, id string) (*repository.Category, error)
	ListCategories(ctx context.Context) ([]repository.Category, error)
	ListCategoriesWithVideos(ctx context.Context) ([]repository.Category, error)
	ListCategoriesByIDs(ctx context.Context, ids []string) ([]repository.Category, error)
	SearchCategories(ctx context.Context, query string, limit int) ([]repository.Category, error)
	SearchCategoriesWithVideos(ctx context.Context, query string, limit int) ([]repository.Category, error)
	UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error)
	UpsertCategories(ctx context.Context, categories []repository.Category) ([]repository.Category, error)
	GetCategorySearchCache(ctx context.Context, normalizedQuery string) (*repository.CategorySearchCache, error)
	UpsertCategorySearchCache(ctx context.Context, input repository.CategorySearchCacheInput) (*repository.CategorySearchCache, error)
	TouchCategorySearchCache(ctx context.Context, normalizedQuery string, at time.Time) error
	DeleteExpiredCategorySearchCache(ctx context.Context, before time.Time) error
	PruneCategorySearchCache(ctx context.Context, maxRows int) error
}

type categorySearcher interface {
	SearchCategories(ctx context.Context, params *twitch.SearchCategoriesParams) ([]twitch.SearchCategory, twitch.Pagination, error)
}

const (
	defaultCategorySearchLimit   = 50
	maxCategorySearchLimit       = 200
	maxTwitchCategorySearchLimit = 100
	minRemoteCategorySearchRunes = 2
	categorySearchCacheTTL       = 24 * time.Hour
	// Negative (empty-result) hits expire quickly: a game that appears on Twitch
	// shortly after a fruitless search should become findable in minutes, not an
	// hour. The cache is never invalidated on category upsert, so this short TTL
	// is what bounds the "searched too early, stays empty" window.
	categorySearchNegativeCacheTTL = 5 * time.Minute
	categorySearchCacheMaxRows     = 1000
	// How often the expiry-delete + LRU-prune maintenance runs. Amortizes the
	// two extra writes that would otherwise fire on every cache miss; the table
	// is hard-capped at categorySearchCacheMaxRows, so skipping a sweep between
	// intervals can't grow it unbounded.
	categorySearchCacheSweepInterval = 10 * time.Minute
)

// Service is the category domain service.
type Service struct {
	repo   categoryRepo
	twitch categorySearcher
	log    *slog.Logger
	now    func() time.Time

	// cacheSweepMu guards lastCacheSweep, which time-gates the cache maintenance
	// (expiry delete + LRU prune) so it runs at most once per
	// categorySearchCacheSweepInterval rather than on every cache miss.
	cacheSweepMu   sync.Mutex
	lastCacheSweep time.Time
}

// shouldSweepCache reports whether enough time has elapsed to run the cache
// maintenance again, recording the sweep time when it returns true.
func (s *Service) shouldSweepCache(now time.Time) bool {
	s.cacheSweepMu.Lock()
	defer s.cacheSweepMu.Unlock()
	if !s.lastCacheSweep.IsZero() && now.Sub(s.lastCacheSweep) < categorySearchCacheSweepInterval {
		return false
	}
	s.lastCacheSweep = now
	return true
}

// Option customizes the category service. Tests use this to make cache
// expiry deterministic without sleeping.
type Option func(*Service)

// WithClock overrides the service clock.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the service.
func New(repo categoryRepo, tc categorySearcher, log *slog.Logger, opts ...Option) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{
		repo:   repo,
		twitch: tc,
		log:    log.With("domain", "category"),
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// GetByID returns a category by Twitch game_id, or ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id string) (*repository.Category, error) {
	return s.repo.GetCategory(ctx, id)
}

// List returns every mirrored category ordered by the repo's list query.
func (s *Service) List(ctx context.Context) ([]repository.Category, error) {
	return s.repo.ListCategories(ctx)
}

// ListWithVideos returns only categories that have at least one visible
// recording. This is the browse-page surface; category.search may mirror
// Twitch-only rows into categories, but those rows do not belong in the local
// recording library until a video is linked to them.
func (s *Service) ListWithVideos(ctx context.Context) ([]repository.Category, error) {
	return s.repo.ListCategoriesWithVideos(ctx)
}

// SearchWithVideos returns only categories that have at least one visible
// recording. It never calls Twitch and never returns catalog-only rows, so it is
// the category surface for global/library search.
func (s *Service) SearchWithVideos(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	return s.repo.SearchCategoriesWithVideos(ctx, normalizeCategorySearchQuery(query), normalizeCategorySearchLimit(limit))
}

// Search returns categories matching query (empty matches local rows), ranked
// by match quality and capped at limit. Non-empty searches first check a
// normalized-query Twitch search cache, then refresh from Twitch when needed
// so schedule filters can be created before a category has appeared locally.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	query = normalizeCategorySearchQuery(query)
	limit = normalizeCategorySearchLimit(limit)

	local, err := s.repo.SearchCategories(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if query == "" || s.twitch == nil || !canRemoteCategorySearch(query) {
		return mergeCategorySearchResults(query, limit, local), nil
	}

	now := s.now()
	cached, cachedRows, err := s.readCategorySearchCache(ctx, query)
	switch {
	case err != nil && cached != nil && cached.ExpiresAt.After(now):
		// Deref failure on a still-valid entry. A flapping DB must not turn every
		// hot-cache hit into a Helix call, so serve the local fallback and skip
		// the remote refresh — the entry is still valid, so the next request
		// retries the deref rather than re-querying Twitch.
		s.log.Warn("deref category search cache; serving local fallback without remote refresh",
			"query", query, "error", err)
		return mergeCategorySearchResults(query, limit, local), nil
	case err != nil:
		s.log.Warn("read category search cache; refreshing from Twitch", "query", query, "error", err)
	case cached != nil && cached.ExpiresAt.After(now):
		if err := s.repo.TouchCategorySearchCache(context.WithoutCancel(ctx), query, now); err != nil {
			s.log.Warn("touch category search cache", "query", query, "error", err)
		}
		return mergeCategorySearchResults(query, limit, local, cachedRows), nil
	}

	remote, _, err := s.twitch.SearchCategories(ctx, &twitch.SearchCategoriesParams{
		Query: query,
		First: remoteCategorySearchLimit(limit),
	})
	if err != nil {
		if hasCategorySearchFallback(local, cachedRows) {
			s.log.Warn("search Twitch categories; returning fallback matches", "query", query, "error", err)
			return mergeCategorySearchResults(query, limit, local, cachedRows), nil
		}
		return nil, fmt.Errorf("search Twitch categories %q: %w", query, err)
	}

	remoteRows, remoteIDs, err := s.upsertRemoteCategories(ctx, remote)
	if err != nil {
		return nil, err
	}
	s.writeCategorySearchCache(ctx, query, remoteIDs, now)
	return mergeCategorySearchResults(query, limit, local, remoteRows), nil
}

func (s *Service) upsertRemoteCategories(ctx context.Context, remote []twitch.SearchCategory) ([]repository.Category, []string, error) {
	categories := []repository.Category{}
	ids := []string{}
	seen := map[string]struct{}{}
	for _, r := range remote {
		if r.ID == "" || r.Name == "" {
			continue
		}
		if _, ok := seen[r.ID]; ok {
			continue
		}
		cat := repository.Category{
			ID:        r.ID,
			Name:      r.Name,
			BoxArtURL: ptr.StringOrNil(r.BoxArtURL),
		}
		seen[cat.ID] = struct{}{}
		categories = append(categories, cat)
		ids = append(ids, cat.ID)
	}
	if len(categories) == 0 {
		return []repository.Category{}, ids, nil
	}
	rows, err := s.repo.UpsertCategories(ctx, categories)
	if err != nil {
		return nil, nil, fmt.Errorf("cache twitch categories: %w", err)
	}
	return repository.OrderCategoriesByIDs(rows, ids), ids, nil
}

func (s *Service) readCategorySearchCache(ctx context.Context, query string) (*repository.CategorySearchCache, []repository.Category, error) {
	cached, err := s.repo.GetCategorySearchCache(ctx, query)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	rows, err := s.categoriesByCachedIDs(ctx, cached.CategoryIDs)
	if err != nil {
		return cached, nil, err
	}
	return cached, rows, nil
}

func (s *Service) categoriesByCachedIDs(ctx context.Context, ids []string) ([]repository.Category, error) {
	if len(ids) == 0 {
		return []repository.Category{}, nil
	}
	return s.repo.ListCategoriesByIDs(ctx, ids)
}

func (s *Service) writeCategorySearchCache(ctx context.Context, query string, categoryIDs []string, now time.Time) {
	ctx = context.WithoutCancel(ctx)
	ttl := categorySearchCacheTTL
	if len(categoryIDs) == 0 {
		ttl = categorySearchNegativeCacheTTL
	}
	if _, err := s.repo.UpsertCategorySearchCache(ctx, repository.CategorySearchCacheInput{
		NormalizedQuery: query,
		CategoryIDs:     categoryIDs,
		ExpiresAt:       now.Add(ttl),
		LastAccessedAt:  now,
	}); err != nil {
		s.log.Warn("write category search cache", "query", query, "error", err)
		return
	}
	// Maintenance is amortized: only sweep occasionally, not on every miss.
	if !s.shouldSweepCache(now) {
		return
	}
	if err := s.repo.DeleteExpiredCategorySearchCache(ctx, now); err != nil {
		s.log.Warn("delete expired category search cache rows", "error", err)
	}
	if err := s.repo.PruneCategorySearchCache(ctx, categorySearchCacheMaxRows); err != nil {
		s.log.Warn("prune category search cache rows", "max_rows", categorySearchCacheMaxRows, "error", err)
	}
}

type rankedCategory struct {
	category repository.Category
	score    int
	name     string
	id       string
	source   int
}

func mergeCategorySearchResults(query string, limit int, sources ...[]repository.Category) []repository.Category {
	byID := map[string]rankedCategory{}
	for sourceIndex, source := range sources {
		for _, category := range source {
			if category.ID == "" || category.Name == "" {
				continue
			}
			candidate := rankedCategory{
				category: category,
				score:    categorySearchScore(query, category.Name),
				name:     normalizeCategorySearchQuery(category.Name),
				id:       category.ID,
				source:   sourceIndex,
			}
			current, ok := byID[category.ID]
			if !ok || categorySearchDedupeBetter(candidate, current) {
				byID[category.ID] = candidate
			}
		}
	}

	ranked := make([]rankedCategory, 0, len(byID))
	for _, category := range byID {
		ranked = append(ranked, category)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return categorySearchRankLess(ranked[i], ranked[j])
	})

	out := make([]repository.Category, 0, len(ranked))
	for _, row := range ranked {
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, row.category)
	}
	return out
}

func hasCategorySearchFallback(local, cachedRows []repository.Category) bool {
	return len(local) > 0 || len(cachedRows) > 0
}

func categorySearchDedupeBetter(a, b rankedCategory) bool {
	if a.id == b.id {
		if a.score != b.score {
			return a.score < b.score
		}
		if a.source != b.source {
			return a.source > b.source
		}
	}
	return categorySearchRankLess(a, b)
}

func categorySearchRankLess(a, b rankedCategory) bool {
	if a.score != b.score {
		return a.score < b.score
	}
	if a.name != b.name {
		return a.name < b.name
	}
	return a.id < b.id
}

func categorySearchScore(query, name string) int {
	query = normalizeCategorySearchQuery(query)
	name = normalizeCategorySearchQuery(name)
	if query == "" {
		return 3
	}
	if name == query {
		return 0
	}
	if strings.HasPrefix(name, query) {
		return 1
	}
	if strings.Contains(name, query) {
		return 2
	}
	return 3
}

func normalizeCategorySearchQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func normalizeCategorySearchLimit(limit int) int {
	if limit <= 0 {
		return defaultCategorySearchLimit
	}
	if limit > maxCategorySearchLimit {
		return maxCategorySearchLimit
	}
	return limit
}

func remoteCategorySearchLimit(limit int) int {
	if limit <= 0 {
		return defaultCategorySearchLimit
	}
	if limit > maxTwitchCategorySearchLimit {
		return maxTwitchCategorySearchLimit
	}
	return limit
}

func canRemoteCategorySearch(query string) bool {
	return utf8.RuneCountInString(query) >= minRemoteCategorySearchRunes
}
