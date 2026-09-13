package playbackcache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

func compatibleParts() []repository.VideoPart {
	fps := 60.0
	return []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
		{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
	}
}

func savePartFiles(t *testing.T, ctx context.Context, store *storage.LocalStorage, parts []repository.VideoPart) {
	t.Helper()
	for _, p := range parts {
		if err := store.Save(ctx, storagekeys.Video(p.Filename), bytes.NewReader([]byte("part"))); err != nil {
			t.Fatalf("save %s: %v", p.Filename, err)
		}
	}
}

func cacheMedia(t *testing.T, repo repository.Repository, raw storage.Storage, gate mediastore.Gate, locks *recordinglock.Locks) *mediastore.Store {
	t.Helper()
	if fake, ok := repo.(*fakeRepo); ok && fake.Repository == nil {
		fake.Repository = sqliteadapter.New(testdb.NewSQLiteDB(t))
	}
	return mediatest.New(t, repo, raw, gate, locks)
}

type cacheTx struct {
	repository.Repository
	video *repository.Video
	fake  *fakeRepo
}

func (r cacheTx) GetVideoForUpdate(context.Context, int64) (*repository.Video, error) {
	if r.video == nil {
		return nil, repository.ErrNotFound
	}
	return r.video, nil
}

func (r *fakeRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(cacheTx{tx, r.video, r}) })
}

func (r cacheTx) UpsertVideoPlaybackAsset(ctx context.Context, in *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	return r.fake.UpsertVideoPlaybackAsset(ctx, in)
}

type publicationRepo struct {
	repository.Repository
	beforeReady    func()
	afterListReady func()
}

func (r *publicationRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&publicationRepo{Repository: tx, beforeReady: r.beforeReady})
	})
}

func (r *publicationRepo) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	return &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100}, nil
}

func (r *publicationRepo) UpsertVideoPlaybackAsset(ctx context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if input.Status == repository.PlaybackAssetStatusReady && r.beforeReady != nil {
		r.beforeReady()
	}
	return r.Repository.UpsertVideoPlaybackAsset(ctx, input)
}

func (r *publicationRepo) ListReadyVideoPlaybackAssets(ctx context.Context, after repository.PlaybackAssetCursor, limit int) ([]repository.VideoPlaybackAsset, error) {
	entries, err := r.Repository.ListReadyVideoPlaybackAssets(ctx, after, limit)
	if err == nil && r.afterListReady != nil {
		r.afterListReady()
	}
	return entries, err
}

func publicationFixture(t *testing.T) (*Service, *publicationRepo, *storage.LocalStorage, *retention.Service, *repository.Video) {
	t.Helper()
	ctx := t.Context()
	repo := &publicationRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t))}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b", BroadcasterLogin: "b", BroadcasterName: "B"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "job", Filename: "vod-42", DisplayName: "B", BroadcasterID: "b", Status: repository.VideoStatusDone, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range compatibleParts() {
		row, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: part.PartIndex, Filename: part.Filename, Quality: part.Quality, FPS: part.FPS, Codec: part.Codec, SegmentFormat: repository.SegmentFormatFMP4})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: row.ID, DurationSeconds: part.DurationSeconds, SizeBytes: part.SizeBytes, EndMediaSeq: 1}); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, storagekeys.Video(part.Filename), strings.NewReader("part")); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.DiscardHandler)
	locks := &recordinglock.Locks{}
	gate := gateFunc(func() error { return nil })
	deleter := retention.New(repo, cacheMedia(t, repo, store, gate, locks), log)
	svc := New(repo, cacheMedia(t, repo, store, gate, locks), "", log)
	svc.SetRunner(&fakeRunner{body: []byte("playback")})
	t.Cleanup(svc.Close)
	return svc, repo, store, deleter, v
}

func assertPurged(t *testing.T, repo repository.Repository, store storage.Storage, videoID int64) {
	t.Helper()
	v, err := repo.GetVideo(t.Context(), videoID)
	if err != nil || v.DeletedAt == nil {
		t.Fatalf("recording was not deleted: %+v, %v", v, err)
	}
	if asset, err := repo.GetVideoPlaybackAsset(t.Context(), videoID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("publication recreated asset after retention: %+v, %v", asset, err)
	}
	if local, ok := store.(*storage.LocalStorage); ok {
		files, err := filepath.Glob(filepath.Join(local.Root, "videos", "*-playback-*"))
		if err != nil || len(files) != 0 {
			t.Fatalf("publication artifacts survived purge: %v %v", files, err)
		}
	}
}

type fakeRepo struct {
	repository.Repository
	settings            *repository.ServerSettings
	video               *repository.Video
	parts               []repository.VideoPart
	asset               *repository.VideoPlaybackAsset
	ready               []repository.VideoPlaybackAsset
	readyErr            error
	statsTotal          int64
	recheckErr          error
	deleteAssetErr      error
	deleteAssetErrForID int64 // 0 = apply deleteAssetErr to all videoIDs
	getVideoCalls       int
	events              []string
}

func (r *fakeRepo) VideoStatsTotals(context.Context, string) (*repository.VideoStatsTotals, error) {
	return &repository.VideoStatsTotals{TotalSize: r.statsTotal}, nil
}

func (r *fakeRepo) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	if r.settings == nil {
		return nil, repository.ErrNotFound
	}
	return r.settings, nil
}

func (r *fakeRepo) GetVideo(context.Context, int64) (*repository.Video, error) {
	r.getVideoCalls++
	// The second video lookup is the publication check after concat.
	if r.getVideoCalls >= 2 && r.recheckErr != nil {
		return nil, r.recheckErr
	}
	if r.video == nil {
		return nil, repository.ErrNotFound
	}
	return r.video, nil
}

func (r *fakeRepo) ListVideoParts(context.Context, int64) ([]repository.VideoPart, error) {
	return r.parts, nil
}

func (r *fakeRepo) GetVideoPlaybackAsset(_ context.Context, videoID int64) (*repository.VideoPlaybackAsset, error) {
	if r.asset != nil && (r.asset.VideoID == videoID || r.asset.VideoID == 0) {
		return r.asset, nil
	}
	for i := range r.ready {
		if r.ready[i].VideoID == videoID {
			return &r.ready[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeRepo) UpsertVideoPlaybackAsset(_ context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if input.Status == repository.PlaybackAssetStatusReady && r.readyErr != nil {
		err := r.readyErr
		r.readyErr = nil
		return nil, err
	}
	r.events = append(r.events, input.Status)
	r.asset = &repository.VideoPlaybackAsset{
		VideoID:         input.VideoID,
		Status:          input.Status,
		Filename:        input.Filename,
		MimeType:        input.MimeType,
		DurationSeconds: input.DurationSeconds,
		SizeBytes:       input.SizeBytes,
		Error:           input.Error,
		GeneratedAt:     input.GeneratedAt,
		LastAccessedAt:  input.LastAccessedAt,
		UpdatedAt:       time.Now(),
	}
	return r.asset, nil
}

func (r *fakeRepo) ListReadyVideoPlaybackAssets(_ context.Context, after repository.PlaybackAssetCursor, limit int) ([]repository.VideoPlaybackAsset, error) {
	out := []repository.VideoPlaybackAsset{}
	started := after.VideoID == 0
	for _, v := range r.ready {
		if started {
			out = append(out, v)
			if len(out) == limit {
				break
			}
		} else if v.VideoID == after.VideoID {
			started = true
		}
	}
	return out, nil
}

func (r *fakeRepo) DeleteVideoPlaybackAsset(_ context.Context, videoID int64) error {
	if r.deleteAssetErr != nil && (r.deleteAssetErrForID == 0 || r.deleteAssetErrForID == videoID) {
		return r.deleteAssetErr
	}
	if r.asset != nil && r.asset.VideoID == videoID {
		r.asset = nil
	}
	filtered := r.ready[:0]
	for _, entry := range r.ready {
		if entry.VideoID != videoID {
			filtered = append(filtered, entry)
		}
	}
	r.ready = filtered
	return nil
}

type fakeRunner struct {
	calls       int
	lists       []string
	body        []byte
	err         error
	beforeWrite func()
}

func (r *fakeRunner) Concat(_ context.Context, listPath, outputPath string) error {
	r.calls++
	data, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	r.lists = append(r.lists, string(data))
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	if err := os.WriteFile(outputPath, r.body, 0o644); err != nil {
		return err
	}
	return r.err
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func (r *fakeRepo) SumReadyPlaybackBytes(context.Context) (int64, error) {
	var n int64
	for _, v := range r.ready {
		if v.SizeBytes != nil {
			n += *v.SizeBytes
		}
	}
	return n, nil
}

type gateFunc func() error

func (f gateFunc) Ready() error { return f() }

func (f gateFunc) Verify(context.Context) error { return f() }

func cacheFixture(t *testing.T, gate mediastore.Gate) (*Service, *fakeRepo, *storage.LocalStorage, *fakeRunner) {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, t.Context(), store, repo.parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, cacheMedia(t, repo, store, gate, nil), "", nil)
	svc.SetRunner(runner)
	t.Cleanup(svc.Close)
	return svc, repo, store, runner
}

// assertPlaybackFiles checks both legacy and unique artifact names. Rejected
// builds must also release their copied inputs and concat output from scratch.
func assertPlaybackFiles(t *testing.T, svc *Service, store *storage.LocalStorage, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.Root, "videos"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "-playback") {
			got = append(got, entry.Name())
		}
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("playback artifacts = %v, want %v", got, want)
	}
	entries, err = os.ReadDir(svc.store.Scratch().Root())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("build left scratch files: %v", entries)
	}
}
