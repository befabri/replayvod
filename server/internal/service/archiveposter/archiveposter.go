// Package archiveposter stores Twitch VOD posters as recording snapshots.
// Fetch and backfill share ownership; failed uploads retain their own keys.
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

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const (
	maxPosterBytes = 8 << 20
	fetchTimeout   = 30 * time.Second
	// Window limits retries for posters Twitch never renders after a VOD ends.
	Window = 24 * time.Hour
	// pageSize is the Helix limit of 100 video IDs per lookup.
	pageSize = 100
)

// Store fetches a poster image and attaches it to an archive.
type Store struct {
	repo    repository.Repository
	storage *mediastore.Store
	client  *http.Client
	log     *slog.Logger

	mu       sync.Mutex
	inFlight map[int64]struct{}
}

// NewStore creates shared poster ownership for downloader and backfill requests.
func NewStore(repo repository.Repository, store *mediastore.Store, client *http.Client, log *slog.Logger) *Store {
	return &Store{
		repo: repo, storage: store, client: client,
		log:      log.With("domain", "archiveposter"),
		inFlight: make(map[int64]struct{}),
	}
}

// Fetch stores a usable JPEG poster when the recording has none.
// Each upload uses a fresh snapshot position because prior uploads may finish late.
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
	if s.verify(ctx) != nil || !s.wantsPoster(ctx, videoID) {
		return false
	}
	data, ok := s.download(ctx, log, url)
	if !ok {
		return false
	}
	// A mount change during the fetch invalidates the earlier readiness verdict.
	if err := s.verify(ctx); err != nil {
		return false
	}
	owned, err := s.storage.Lock(ctx, videoID)
	if err != nil {
		return false
	}
	defer owned.Close()
	if !s.wantsPoster(ctx, videoID) {
		return false
	}
	// An uncertain upload may finish after a retry fetches different CDN bytes.
	index, err := owned.NextSnapshotIndex(ctx, filename, 0)
	if err != nil {
		log.Warn("reserve poster snapshot", "error", err)
		return false
	}
	key := storagekeys.Snapshot(filename, index)
	if err := owned.Save(ctx, key, bytes.NewReader(data)); err != nil {
		log.Warn("save poster", "error", err)
		return false
	}
	// A mount lost during upload must leave the reference unset for retry on trusted storage.
	if err := s.verify(ctx); err != nil {
		return false
	}
	var set bool
	err = owned.Commit(ctx, func(tx repository.Repository) error {
		var e error
		set, e = tx.SetVideoThumbnailIfMissing(ctx, videoID, key)
		return e
	})
	if err != nil {
		log.Warn("set thumbnail", "error", err)
	}
	if err == nil && set {
		return true
	}
	// An uncertain commit may have referenced this key; preserve it unless a fresh
	// read proves it unreferenced, including when that read fails.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if s.referencesPoster(cleanupCtx, videoID, key) {
		return false
	}
	if err := s.verify(cleanupCtx); err != nil {
		return false
	}
	if err := owned.Delete(cleanupCtx, key); err != nil {
		log.Warn("clean unreferenced poster", "error", err)
	}
	return false
}

func (s *Store) verify(ctx context.Context) error { return s.storage.Verify(ctx) }

// claim excludes concurrent fetches for the same recording.
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

// wantsPoster permits an unreadable row; publication rechecks eligibility transactionally.
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

	// resumeAfter advances past slow posters across deadlines and resets at the end.
	mu          sync.Mutex
	resumeAfter int64
}

// New shares poster ownership with the downloader through store.
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

// Report counts checked archives and posters stored during a backfill run.
type Report struct {
	Checked, Stored int
	// Complete is false when storage is unavailable or the run stopped with
	// archives left; the next run continues after the last one attempted.
	Complete bool
}

// Backfill visits recent archives in pages and stores available Twitch posters.
// Runs interrupted after progress resume beyond the last attempted archive.
func (s *Service) Backfill(ctx context.Context) (Report, error) {
	var report Report
	since := s.now().UTC().Add(-Window)
	after := s.resumePoint()
	attempted := 0
	for {
		if ctx.Err() != nil {
			return s.stopped(ctx, report, after, attempted)
		}
		if err := s.store.verify(ctx); err != nil {
			if ctx.Err() != nil {
				return s.stopped(ctx, report, after, attempted)
			}
			s.setResumePoint(after)
			s.log.Debug("poster backfill waiting for writable storage", "error", err)
			return report, nil
		}
		page, err := repository.NewBatchPage(after, s.pageSize)
		if err != nil {
			return report, fmt.Errorf("poster backfill: %w", err)
		}
		rows, err := s.repo.ListArchivesMissingPoster(ctx, since, page)
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

// stopped saves progress and surfaces cancellation or a deadline before any work.
func (s *Service) stopped(ctx context.Context, report Report, after int64, attempted int) (Report, error) {
	s.setResumePoint(after)
	if attempted == 0 || errors.Is(ctx.Err(), context.Canceled) {
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
