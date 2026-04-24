package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// fakeFollowedStreamsSource is a narrow test double for the subset of
// *twitch.Client that Service needs. Each call pops the next page off
// `pages`; if errAt is set to a page index, that call returns err
// instead. Records every invocation so tests can assert call count
// (specifically: the maxFollowedPages cap).
type fakeFollowedStreamsSource struct {
	pages [][]twitch.Stream
	errAt int // -1 = never
	err   error
	calls int
}

func newFakeSource(pages ...[]twitch.Stream) *fakeFollowedStreamsSource {
	return &fakeFollowedStreamsSource{pages: pages, errAt: -1}
}

func (f *fakeFollowedStreamsSource) GetFollowedStreams(_ context.Context, _ *twitch.GetFollowedStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	f.calls++
	if f.errAt >= 0 && f.calls-1 == f.errAt {
		return nil, twitch.Pagination{}, f.err
	}
	idx := f.calls - 1
	if idx >= len(f.pages) {
		return nil, twitch.Pagination{}, nil
	}
	page := f.pages[idx]
	cursor := ""
	if idx+1 < len(f.pages) {
		cursor = "next"
	}
	return page, twitch.Pagination{Cursor: cursor}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newServiceWithRepo builds a Service backed by a real SQLite adapter
// (so UpsertStream + ListChannelsByIDs exercise actual SQL) and a
// fake Twitch source. Returns both so tests can seed channels + assert
// mirrored state.
func newServiceWithRepo(t *testing.T, src followedStreamsSource) (*Service, streamRepo) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	svc := New(repo, src, discardLogger())
	return svc, repo
}

// stream returns a twitch.Stream with enough fields populated that
// UpsertStream's required columns (id, broadcaster_id, type, language,
// started_at) come through non-empty.
func stream(id, broadcasterID string, viewers int) twitch.Stream {
	return twitch.Stream{
		ID:          id,
		UserID:      broadcasterID,
		UserLogin:   "login-" + broadcasterID,
		UserName:    "Name " + broadcasterID,
		Type:        "live",
		Language:    "en",
		ViewerCount: viewers,
		StartedAt:   time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
}

// TestFollowed_EmptyHelixResult is the null case — no calls to repo,
// no error, empty result. Pins the happy path for a user who follows
// no one who's currently live.
func TestFollowed_EmptyHelixResult(t *testing.T) {
	ctx := context.Background()
	src := newFakeSource([]twitch.Stream{}) // one page, empty
	svc, _ := newServiceWithRepo(t, src)

	got, err := svc.Followed(ctx, FollowedInput{UserID: "me"})
	if err != nil {
		t.Fatalf("followed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 streams, got %d", len(got))
	}
	if src.calls != 1 {
		t.Errorf("expected 1 Helix call, got %d", src.calls)
	}
}

// TestFollowed_MirrorsOnlyKnownBroadcasters pins the review-fix
// behavior: the mirror side effect filters to locally-synced
// broadcasters, so unsynced ones don't trigger FK violations (no
// warn-log spam) and are skipped silently.
func TestFollowed_MirrorsOnlyKnownBroadcasters(t *testing.T) {
	ctx := context.Background()
	src := newFakeSource([]twitch.Stream{
		stream("s-known", "bc-known", 10),
		stream("s-unknown", "bc-unknown", 20),
	})
	svc, repo := newServiceWithRepo(t, src)

	// Seed only one of the two broadcasters locally.
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-known", BroadcasterLogin: "k", BroadcasterName: "K",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	got, err := svc.Followed(ctx, FollowedInput{UserID: "me"})
	if err != nil {
		t.Fatalf("followed: %v", err)
	}
	// Helix payload: both streams flow through unchanged.
	if len(got) != 2 {
		t.Fatalf("helix result: expected 2 streams, got %d", len(got))
	}

	// Mirror: only the known broadcaster's stream landed in the DB.
	active, err := repo.ListActiveStreams(ctx)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("mirror: expected 1 row, got %d", len(active))
	}
	if active[0].ID != "s-known" {
		t.Errorf("mirrored stream: want s-known, got %s", active[0].ID)
	}
}

// TestFollowed_EnrichesProfileImageURL pins the avatar-denormalization
// fix: locally-known broadcasters get their profile_image_url attached
// to the response; unknown ones come back with nil. This eliminates
// the frontend's need for a second useChannels() fetch just to render
// avatars on the dashboard "Just went live" card.
func TestFollowed_EnrichesProfileImageURL(t *testing.T) {
	ctx := context.Background()
	src := newFakeSource([]twitch.Stream{
		stream("s-known", "bc-known", 10),
		stream("s-unknown", "bc-unknown", 20),
	})
	svc, repo := newServiceWithRepo(t, src)

	knownProfile := "https://example.com/known.png"
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "bc-known",
		BroadcasterLogin: "k",
		BroadcasterName:  "K",
		ProfileImageURL:  &knownProfile,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.Followed(ctx, FollowedInput{UserID: "me"})
	if err != nil {
		t.Fatalf("followed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	byID := map[string]FollowedStream{}
	for _, f := range got {
		byID[f.Stream.UserID] = f
	}
	if known := byID["bc-known"]; known.ProfileImageURL == nil || *known.ProfileImageURL != knownProfile {
		t.Errorf("known broadcaster ProfileImageURL: got %v, want %q", known.ProfileImageURL, knownProfile)
	}
	if unknown := byID["bc-unknown"]; unknown.ProfileImageURL != nil {
		t.Errorf("unknown broadcaster ProfileImageURL: got %v, want nil", unknown.ProfileImageURL)
	}
}

// TestFollowed_MirrorsStreamFields verifies the Helix → StreamInput
// field mapping (viewer count cast, thumbnail nil→*string pointer).
// Without this, a refactor that swaps ThumbnailURL for ThumbnailUrl
// could silently drop the field.
func TestFollowed_MirrorsStreamFields(t *testing.T) {
	ctx := context.Background()
	s := stream("s-1", "bc-1", 42)
	s.ThumbnailURL = "https://example.com/thumb.jpg"
	src := newFakeSource([]twitch.Stream{s})
	svc, repo := newServiceWithRepo(t, src)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "one", BroadcasterName: "One",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Followed(ctx, FollowedInput{UserID: "me"}); err != nil {
		t.Fatalf("followed: %v", err)
	}

	got, err := repo.GetStream(ctx, "s-1")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	if got.BroadcasterID != "bc-1" {
		t.Errorf("BroadcasterID: got %q", got.BroadcasterID)
	}
	if got.ViewerCount != 42 {
		t.Errorf("ViewerCount: got %d", got.ViewerCount)
	}
	if got.ThumbnailURL == nil || *got.ThumbnailURL != "https://example.com/thumb.jpg" {
		t.Errorf("ThumbnailURL mirror: got %v", got.ThumbnailURL)
	}
	if !got.StartedAt.Equal(s.StartedAt) {
		t.Errorf("StartedAt: want %v got %v", s.StartedAt, got.StartedAt)
	}
}

// TestFollowed_PaginationCap stops walking cursors at
// maxFollowedPages. Feed 12 pages (2 beyond the cap); verify exactly
// `maxFollowedPages` Helix calls happen and the result truncates to
// the first maxFollowedPages × page-size rows.
func TestFollowed_PaginationCap(t *testing.T) {
	ctx := context.Background()
	pages := make([][]twitch.Stream, maxFollowedPages+2)
	for i := range pages {
		pages[i] = []twitch.Stream{stream("s-"+string(rune('a'+i)), "bc-"+string(rune('a'+i)), i)}
	}
	src := newFakeSource(pages...)
	svc, _ := newServiceWithRepo(t, src)

	got, err := svc.Followed(ctx, FollowedInput{UserID: "me"})
	if err != nil {
		t.Fatalf("followed: %v", err)
	}
	if src.calls != maxFollowedPages {
		t.Errorf("expected %d Helix calls (cap), got %d", maxFollowedPages, src.calls)
	}
	if len(got) != maxFollowedPages {
		t.Errorf("expected %d streams returned (one per capped page), got %d", maxFollowedPages, len(got))
	}
}

// TestFollowed_HelixErrorPropagates asserts Helix errors short-circuit
// without mirroring any rows. A partial mirror on a failed call would
// leave the DB in a state the next-successful call can't reason about.
func TestFollowed_HelixErrorPropagates(t *testing.T) {
	ctx := context.Background()
	src := newFakeSource(
		[]twitch.Stream{stream("s-1", "bc-1", 1)}, // this page never gets consumed
	)
	src.errAt = 0
	src.err = errors.New("helix 500")
	svc, repo := newServiceWithRepo(t, src)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "one", BroadcasterName: "One",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := svc.Followed(ctx, FollowedInput{UserID: "me"})
	if err == nil {
		t.Fatal("expected error from Helix, got nil")
	}
	active, err := repo.ListActiveStreams(ctx)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected no mirrored streams after Helix error, got %d", len(active))
	}
}

// Note: the user-token ctx plumbing (twitch.WithUserToken /
// WithUserID) is not black-box testable from this package without
// widening the twitch API to expose a getter. Leaving it to live
// integration signal — a regression that drops the ctx wiring would
// surface as Helix 401 on the first real call, which is loud enough.
