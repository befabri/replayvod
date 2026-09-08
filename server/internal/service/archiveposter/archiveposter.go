// Package archiveposter gives a VOD archive its poster: the thumbnail Twitch
// renders for the VOD, stored as the recording's first snapshot. The
// downloader fetches it when an archive starts; the scheduler task revisits
// archives Twitch had not rendered a frame for at that time, since the
// placeholder it serves until then is never stored.
//
// Both paths share one Store and one storage key per archive, so ownership of
// that key is settled here: a fetch already in flight for an archive owns the
// key until it finishes, a row that already has a poster is never fetched
// for, and a lost update only ever deletes an object the row does not point
// at.
package archiveposter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const (
	maxPosterBytes = 8 << 20
	fetchTimeout   = 30 * time.Second
	// Window bounds how long after being queued an archive is still looked up
	// for a poster. Twitch renders a VOD thumbnail within minutes of the
	// stream ending; a VOD it never renders is not polled forever.
	Window = 24 * time.Hour
	// pageSize is one Helix lookup: at most 100 ids per call.
	pageSize = 100
)

// Store fetches a poster image and attaches it to an archive.
type Store struct {
	repo    repository.Repository
	storage storage.Storage
	client  *http.Client
	log     *slog.Logger

	mu       sync.Mutex
	inFlight map[int64]struct{}
}

func NewStore(repo repository.Repository, store storage.Storage, client *http.Client, log *slog.Logger) *Store {
	return &Store{
		repo: repo, storage: store, client: client,
		log:      log.With("domain", "archiveposter"),
		inFlight: make(map[int64]struct{}),
	}
}

// Fetch saves the image at url as the recording's first snapshot and makes it
// the row poster unless the row already has one. Best effort: the placeholder
// Twitch serves while it renders, a non-JPEG answer, or a failed download
// leave the row untouched. Reports whether a poster was stored.
func (s *Store) Fetch(parent context.Context, videoID int64, filename, url string) bool {
	if url == "" || twitch.IsVideoThumbnailPlaceholder(url) {
		return false
	}
	release, ok := s.claim(videoID)
	if !ok {
		return false
	}
	defer release()
	ctx, cancel := context.WithTimeout(parent, fetchTimeout)
	defer cancel()
	log := s.log.With("video_id", videoID)
	if !s.wantsPoster(ctx, videoID) {
		return false
	}
	data, ok := s.download(ctx, log, url)
	if !ok {
		return false
	}
	key := storagekeys.Snapshot(filename, 0)
	if err := s.storage.Save(ctx, key, bytes.NewReader(data)); err != nil {
		log.Warn("save poster", "error", err)
		return false
	}
	set, err := s.repo.SetVideoThumbnailIfMissing(ctx, videoID, key)
	if err != nil {
		log.Warn("set thumbnail", "error", err)
	}
	if err == nil && set {
		return true
	}
	// The row got a poster or left the library meanwhile. Another writer may
	// have referenced this very key, so only an object the row does not point
	// at is garbage; an unreadable row keeps it for the recording's purge.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if s.referencesPoster(cleanupCtx, videoID, key) {
		return false
	}
	if err := s.storage.Delete(cleanupCtx, key); err != nil {
		log.Warn("clean unreferenced poster", "error", err)
	}
	return false
}

// claim marks videoID as being fetched for. A second caller while one is in
// flight backs off: the first either stores the poster or leaves the row for
// the next backfill run.
func (s *Store) claim(videoID int64) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.inFlight[videoID]; busy {
		return nil, false
	}
	s.inFlight[videoID] = struct{}{}
	return func() {
		s.mu.Lock()
		delete(s.inFlight, videoID)
		s.mu.Unlock()
	}, true
}

// wantsPoster reads the row before spending a download: a poster already set
// or a removed row never needs one. An unreadable row is fetched for anyway;
// the conditional update decides.
func (s *Store) wantsPoster(ctx context.Context, videoID int64) bool {
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return !errors.Is(err, repository.ErrNotFound)
	}
	return v.Thumbnail == nil && v.DeletedAt == nil
}

func (s *Store) download(ctx context.Context, log *slog.Logger, url string) ([]byte, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Debug("build request", "error", err)
		return nil, false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Debug("fetch", "error", err)
		return nil, false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug("close response", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/jpeg") {
		log.Debug("unusable response", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPosterBytes+1))
	if err != nil || len(data) > maxPosterBytes {
		log.Debug("unreadable or oversized image", "error", err)
		return nil, false
	}
	return data, true
}

func (s *Store) referencesPoster(ctx context.Context, videoID int64, key string) bool {
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return true
	}
	return v.Thumbnail != nil && *v.Thumbnail == key
}

// Service revisits archives that still have no poster.
type Service struct {
	store    *Store
	repo     repository.Repository
	helix    twitch.VideoLookup
	log      *slog.Logger
	now      func() time.Time
	pageSize int

	// resumeAfter is where the next run starts: the last archive a run cut
	// short by its deadline attempted, so slow posters never pin the start of
	// the list. A run that reaches the end resets it.
	mu          sync.Mutex
	resumeAfter int64
}

// New builds the backfill over the Store the downloader shares, so a start
// and a backfill for the same archive can never race each other.
func New(store *Store, repo repository.Repository, helix twitch.VideoLookup, log *slog.Logger) *Service {
	return &Service{
		store:    store,
		repo:     repo,
		helix:    helix,
		log:      log.With("domain", "archiveposter"),
		now:      time.Now,
		pageSize: pageSize,
	}
}

type Report struct {
	Checked, Stored int
	// Complete is false when the run stopped at its deadline with archives
	// left; the next run continues after the last one attempted.
	Complete bool
}

// Backfill visits every recent poster-less archive, one Helix lookup per page,
// and stores the poster of each whose thumbnail is no longer the placeholder.
// Paging by id means a placeholder that persists never shadows the archives
// queued after it, and a run cut short by its deadline is progress, not a
// failure: the next run picks up after the last archive this one attempted,
// so a few slow poster hosts cannot pin the start of the list run after run.
func (s *Service) Backfill(ctx context.Context) (Report, error) {
	var report Report
	since := s.now().UTC().Add(-Window)
	after := s.resumePoint()
	attempted := 0
	for {
		if ctx.Err() != nil {
			return s.stopped(ctx, report, after, attempted)
		}
		rows, err := s.repo.ListArchivesMissingPoster(ctx, since, after, s.pageSize)
		if err != nil {
			if ctx.Err() != nil {
				return s.stopped(ctx, report, after, attempted)
			}
			return report, fmt.Errorf("list archives missing poster: %w", err)
		}
		if len(rows) == 0 {
			s.setResumePoint(0)
			report.Complete = true
			return report, nil
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, *row.TwitchVideoID)
		}
		found, err := twitch.LookupVideosByID(ctx, s.helix, ids)
		if err != nil {
			if ctx.Err() != nil {
				return s.stopped(ctx, report, after, attempted)
			}
			return report, err
		}
		for _, row := range rows {
			if ctx.Err() != nil {
				return s.stopped(ctx, report, after, attempted)
			}
			attempted++
			after = row.ID
			v, ok := found[*row.TwitchVideoID]
			if !ok {
				continue
			}
			report.Checked++
			if s.store.Fetch(ctx, row.ID, row.Filename, twitch.VideoThumbnailURL(v.ThumbnailURL, 640, 360)) {
				report.Stored++
			}
		}
	}
}

// stopped ends a run cut short by its context. Archives attempted so far are
// done for this run, so the resume point moves past them and the run counts
// as progress; a run that attempted nothing surfaces the deadline instead.
func (s *Service) stopped(ctx context.Context, report Report, after int64, attempted int) (Report, error) {
	s.setResumePoint(after)
	if attempted == 0 {
		return report, ctx.Err()
	}
	return report, nil
}

func (s *Service) resumePoint() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeAfter
}

func (s *Service) setResumePoint(after int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resumeAfter = after
}
