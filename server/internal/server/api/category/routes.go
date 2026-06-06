package category

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes constructs the category service + handler and
// registers the category.* tRPC procedures. All category reads are
// viewer-level — tags and categories are operational metadata the
// whole UI depends on.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, tc *twitch.Client, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	// Avoid a typed-nil interface: handing a nil *twitch.Client straight to New's
	// categorySearcher param would leave s.twitch != nil (an interface holding a
	// nil pointer) and panic on the first remote search. Convert to the interface
	// only when non-nil, mirroring router.go's twitchClient != nil guard so the
	// service's own nil check stays meaningful.
	var searcher categorySearcher
	if tc != nil {
		searcher = tc
	}
	h := NewHandler(New(repo, searcher, log), log)
	trpcgo.MustQuery(tr, "category.getById", h.GetByID, viewer)
	trpcgo.MustVoidQuery(tr, "category.list", h.List, viewer)
	trpcgo.MustVoidQuery(tr, "category.listWithVideos", h.ListWithVideos, viewer)
	trpcgo.MustQuery(tr, "category.listPage", h.ListPage, viewer)
	trpcgo.MustQuery(tr, "category.search", h.Search, viewer)
	trpcgo.MustQuery(tr, "category.searchWithVideos", h.SearchWithVideos, viewer)
}
