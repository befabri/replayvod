package storagehealth

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type fixture struct {
	ctx   context.Context
	repo  repository.Repository
	store *storage.LocalStorage
	root  string
	bus   *eventbus.Buses
	mon   *Monitor
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	root := filepath.Join(t.TempDir(), "data")
	store, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return fixture{
		ctx: context.Background(), repo: repo, store: store, root: root, bus: bus,
		mon: New(repo, store, bus, log, "local", root, WithInterval(10*time.Millisecond)),
	}
}

func (f fixture) storedID(t *testing.T) string {
	t.Helper()
	settings, err := f.repo.GetServerSettings(f.ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return settings.StorageID
}

func (f fixture) eventCount(t *testing.T, eventType string) int {
	t.Helper()
	rows, err := f.repo.ListEventLogs(f.ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.Domain == EventDomain && r.EventType == eventType {
			n++
		}
	}
	return n
}

func (f fixture) writeMarker(t *testing.T, id string) {
	t.Helper()
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, storage.MarkerPath), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAttachFirstBootRecordsIdentity(t *testing.T) {
	f := newFixture(t)
	status, err := f.mon.Attach(f.ctx)
	if err != nil || status.State != StateAttached {
		t.Fatalf("attach = %+v, %v", status, err)
	}
	if id := f.storedID(t); id == "" || id != status.StorageID {
		t.Fatalf("stored id %q, status id %q", id, status.StorageID)
	}
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("ready = %v", err)
	}
	if f.eventCount(t, EventAttached) != 0 || f.eventCount(t, EventUnattached) != 0 {
		t.Fatal("a healthy boot wrote an event row")
	}
}

func TestAttachAdoptsMarkerForRestoredDatabase(t *testing.T) {
	f := newFixture(t)
	id, _ := storage.NewStorageID()
	f.writeMarker(t, id)
	status, err := f.mon.Attach(f.ctx)
	if err != nil || status.State != StateAttached || f.storedID(t) != id {
		t.Fatalf("attach = %+v, %v, stored %q", status, err, f.storedID(t))
	}
}

func TestUnreachableAtBootInitializesOnceStorageAnswers(t *testing.T) {
	f := newFixture(t)
	if _, err := f.repo.SetStorageID(f.ctx, mustID(t)); err != nil {
		t.Fatal(err)
	}
	status, err := f.mon.Attach(f.ctx)
	if !errors.Is(err, storage.ErrUnreachable) || status.State != StateUnreachable {
		t.Fatalf("attach = %+v, %v", status, err)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("ready = %v", err)
	}
	if f.eventCount(t, EventUnattached) != 1 {
		t.Fatalf("unattached rows = %d, want 1", f.eventCount(t, EventUnattached))
	}
	// Same verdict again: no second row.
	f.mon.Check(f.ctx)
	if f.eventCount(t, EventUnattached) != 1 {
		t.Fatal("a repeated verdict wrote another row")
	}
	// The volume comes back with the expected marker.
	f.writeMarker(t, f.storedID(t))
	sub := f.bus.StorageStatus.Subscribe(f.ctx)
	if got := f.mon.Check(f.ctx); got.State != StateAttached {
		t.Fatalf("check after mount = %+v", got)
	}
	if f.eventCount(t, EventAttached) != 1 {
		t.Fatalf("attached rows = %d, want 1", f.eventCount(t, EventAttached))
	}
	select {
	case ev := <-sub:
		if ev.State != string(StateAttached) {
			t.Fatalf("bus event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no bus event on recovery")
	}
}

func TestCheckRetriesInitializationWhenNoIDRecorded(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.root, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, err := f.mon.Attach(f.ctx); err == nil || status.State == StateAttached {
		t.Fatalf("attach on a file = %+v, %v", status, err)
	}
	if f.storedID(t) != "" {
		t.Fatal("recorded an id without storage")
	}
	if err := os.Remove(f.root); err != nil {
		t.Fatal(err)
	}
	if got := f.mon.Check(f.ctx); got.State != StateAttached || f.storedID(t) == "" {
		t.Fatalf("check = %+v, stored %q", got, f.storedID(t))
	}
}

func TestForeignMarkerIsUnattachedUntilAdopted(t *testing.T) {
	f := newFixture(t)
	if _, err := f.repo.UpsertUser(f.ctx, &repository.User{ID: "owner-1", Login: "owner", DisplayName: "Owner", Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	mine := f.storedID(t)
	theirs, _ := storage.NewStorageID()
	f.writeMarker(t, theirs)
	if got := f.mon.Check(f.ctx); got.State != StateUnattached {
		t.Fatalf("check = %+v", got)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("ready = %v", err)
	}
	status, err := f.mon.Adopt(f.ctx, "owner-1")
	if err != nil || status.State != StateAttached {
		t.Fatalf("adopt = %+v, %v", status, err)
	}
	if f.storedID(t) != mine {
		t.Fatal("adopt changed the recorded id instead of the marker")
	}
	if id, err := storage.ReadMarker(f.ctx, f.store); err != nil || id != mine {
		t.Fatalf("marker after adopt = %q, %v", id, err)
	}
	if f.eventCount(t, EventAdopted) != 1 || f.eventCount(t, EventAttached) != 1 {
		t.Fatalf("rows: adopted %d attached %d", f.eventCount(t, EventAdopted), f.eventCount(t, EventAttached))
	}
	rows, _ := f.repo.ListEventLogs(f.ctx, 100, 0)
	for _, r := range rows {
		if r.EventType == EventAdopted && (r.ActorUserID == nil || *r.ActorUserID != "owner-1") {
			t.Fatalf("adopted row actor = %v", r.ActorUserID)
		}
	}
}

func TestAdoptRefusesUnreachableStorage(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mon.Adopt(f.ctx, "owner-1"); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("adopt = %v, want ErrUnreachable", err)
	}
	if _, err := os.Stat(f.root); err == nil {
		t.Fatal("adopt created the missing root")
	}
}

func TestAdoptWithoutRecordedIDInitializes(t *testing.T) {
	f := newFixture(t)
	status, err := f.mon.Adopt(f.ctx, "owner-1")
	if err != nil || status.State != StateAttached || f.storedID(t) == "" {
		t.Fatalf("adopt = %+v, %v, stored %q", status, err, f.storedID(t))
	}
}

func TestVerifyIsFreshAndReadyIsCached(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.root, storage.MarkerPath)); err != nil {
		t.Fatal(err)
	}
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("cached ready = %v, want nil before a fresh probe", err)
	}
	if err := f.mon.Verify(f.ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("verify = %v", err)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("cached ready after verify = %v", err)
	}
}

func TestCancelledProbeKeepsLastVerdict(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if got := f.mon.Check(ctx); got.State != StateAttached {
		t.Fatalf("check with a cancelled ctx = %+v", got)
	}
	if f.eventCount(t, EventUnattached) != 0 {
		t.Fatal("a cancelled probe wrote a transition")
	}
}

func TestReadyBeforeAnyCheckFailsClosed(t *testing.T) {
	f := newFixture(t)
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("ready = %v", err)
	}
	if f.mon.Status().Readable() {
		t.Fatal("unchecked storage reads as readable")
	}
}

func TestRunRechecksOnIntervalAndStopsOnCancel(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() {
		f.mon.Run(ctx)
		close(done)
	}()
	if err := os.Remove(filepath.Join(f.root, storage.MarkerPath)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.mon.Status().State != StateUnattached {
		if time.Now().After(deadline) {
			t.Fatalf("run never noticed the missing marker: %+v", f.mon.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not stop on cancel")
	}
}

func mustID(t *testing.T) string {
	t.Helper()
	id, err := storage.NewStorageID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestReplacedVolumeCanBeAdoptedWithoutRestart pins the operator's swap: the
// data directory is replaced by another one under the running process, the
// monitor reports it unattached, and Adopt claims the new directory in place.
func TestReplacedVolumeCanBeAdoptedWithoutRestart(t *testing.T) {
	f := newFixture(t)
	if _, err := f.repo.UpsertUser(f.ctx, &repository.User{ID: "owner-1", Login: "owner", DisplayName: "Owner", Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.root, f.root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := f.mon.Check(f.ctx); got.State != StateUnattached {
		t.Fatalf("swapped directory = %+v, want unattached", got)
	}
	status, err := f.mon.Adopt(f.ctx, "owner-1")
	if err != nil || status.State != StateAttached {
		t.Fatalf("adopt of the swapped directory = %+v, %v", status, err)
	}
	if id, err := storage.ReadMarker(f.ctx, f.store); err != nil || id != f.storedID(t) {
		t.Fatalf("marker in the new directory = %q, %v", id, err)
	}
	if got := f.mon.Check(f.ctx); got.State != StateAttached {
		t.Fatalf("re-check after adopt = %+v", got)
	}
}

// seedFinishedRecording puts one DONE recording with a part row in the
// database; present decides whether its media is written under root.
func (f fixture) seedFinishedRecording(t *testing.T, jobID string, present bool) {
	t.Helper()
	if _, err := f.repo.UpsertChannel(f.ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatal(err)
	}
	v, err := f.repo.CreateVideo(f.ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: "B1",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CreateVideoPart(f.ctx, &repository.VideoPartInput{
		VideoID: v.ID, PartIndex: 1, Filename: jobID + "-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.MarkVideoDone(f.ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if present {
		if err := os.MkdirAll(filepath.Join(f.root, "videos"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.root, "videos", jobID+"-part01.mp4"), []byte("media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFirstAttachRefusesAnEmptyRootWhenTheLibraryHasRecordings pins the upgrade
// case: a database that never recorded a storage id boots against an empty
// mount point. Initializing it would mark the whole library missing, so the
// attach refuses, writes nothing, and the operator either mounts the volume
// or adopts the empty one on purpose.
func TestFirstAttachRefusesAnEmptyRootWhenTheLibraryHasRecordings(t *testing.T) {
	f := newFixture(t)
	f.seedFinishedRecording(t, "rec-1", false)
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	status, err := f.mon.Attach(f.ctx)
	if !errors.Is(err, storage.ErrUnattached) || status.State != StateUnattached {
		t.Fatalf("attach to an empty root = %+v, %v; want unattached", status, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, storage.MarkerPath)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a refused first attach wrote the marker")
	}
	if f.storedID(t) != "" {
		t.Fatal("a refused first attach recorded an id")
	}
	if got := f.mon.Check(f.ctx); got.State != StateUnattached {
		t.Fatalf("re-check = %+v, want still unattached", got)
	}
	if f.eventCount(t, EventUnattached) != 1 {
		t.Fatalf("unattached rows = %d, want 1", f.eventCount(t, EventUnattached))
	}

	// The real volume appears: its media is the witness, so the attach initializes.
	if err := os.WriteFile(filepath.Join(f.root, "rec-1-part01.mp4"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.root, "videos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "videos", "rec-1-part01.mp4"), []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := f.mon.Check(f.ctx); got.State != StateAttached || f.storedID(t) == "" {
		t.Fatalf("check once the media is there = %+v, stored %q; want initialized", got, f.storedID(t))
	}
}

func TestAdoptInitializesAnEmptyRootDespiteTheLibrary(t *testing.T) {
	f := newFixture(t)
	if _, err := f.repo.UpsertUser(f.ctx, &repository.User{ID: "owner-1", Login: "owner", DisplayName: "Owner", Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	f.seedFinishedRecording(t, "rec-1", false)
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mon.Attach(f.ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("attach = %v, want ErrUnattached", err)
	}
	status, err := f.mon.Adopt(f.ctx, "owner-1")
	if err != nil || status.State != StateAttached || f.storedID(t) == "" {
		t.Fatalf("adopt = %+v, %v, stored %q; want initialized", status, err, f.storedID(t))
	}
}

func TestFirstAttachInitializesWhenARecordingIsPresent(t *testing.T) {
	f := newFixture(t)
	f.seedFinishedRecording(t, "rec-1", true)
	status, err := f.mon.Attach(f.ctx)
	if err != nil || status.State != StateAttached || f.storedID(t) == "" {
		t.Fatalf("attach with media present = %+v, %v", status, err)
	}
}

func TestFirstAttachProtectsRecordingsExcludedFromScanning(t *testing.T) {
	for _, state := range []string{"running", "queued_delete", "missing"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			f.seedFinishedRecording(t, "recording", false)
			v, err := f.repo.GetVideoByJobID(f.ctx, "recording")
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "running":
				err = f.repo.UpdateVideoStatus(f.ctx, v.ID, repository.VideoStatusRunning)
			case "queued_delete":
				_, err = f.repo.RequestVideoDelete(f.ctx, v.ID)
			case "missing":
				_, err = f.repo.TombstoneMissingVideo(f.ctx, v.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.mon.Attach(f.ctx); !errors.Is(err, storage.ErrUnattached) {
				t.Fatalf("attach over missing recording = %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.root, storage.MarkerPath)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("refused attach wrote a marker: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(f.root, "videos"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.root, "videos/recording-part01.mp4"), []byte("media"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := f.mon.Attach(f.ctx); err != nil {
				t.Fatalf("attach once recording returns: %v", err)
			}
		})
	}
}

func TestAdoptRepairsMalformedMarkerOnlyOnExplicitRequest(t *testing.T) {
	for _, marker := range []string{"", "not-an-identity"} {
		t.Run(marker, func(t *testing.T) {
			f := newFixture(t)
			f.writeMarker(t, marker)
			st, err := f.mon.Attach(f.ctx)
			if !errors.Is(err, storage.ErrUnattached) || st.Readable() || f.storedID(t) != "" {
				t.Fatalf("boot accepted malformed marker: %+v %v", st, err)
			}
			waitProbeIdle(t, f.mon)
			st, err = f.mon.Adopt(f.ctx, "")
			if err != nil || st.State != StateAttached || st.StorageID == "" {
				t.Fatalf("explicit adoption: %+v %v", st, err)
			}
			id, err := storage.ReadMarker(f.ctx, f.store)
			if err != nil || id != f.storedID(t) || id != st.StorageID {
				t.Fatalf("marker and database disagree: %q %+v %v", id, st, err)
			}
		})
	}
}

func TestFirstAttachRemembersReadOnlyIdentityAndRecovers(t *testing.T) {
	f := newFixture(t)
	id, err := storage.NewStorageID()
	if err != nil {
		t.Fatal(err)
	}
	f.writeMarker(t, id)
	f.mon.store = readOnlyProbe{f.store}
	events := f.bus.StorageStatus.Subscribe(t.Context())
	st, err := f.mon.Attach(f.ctx)
	if !errors.Is(err, storage.ErrReadOnly) || st.State != StateReadOnly || !st.Readable() || f.storedID(t) != id {
		t.Fatalf("read-only attach: %+v %v", st, err)
	}
	if !errors.Is(f.mon.Ready(), storage.ErrReadOnly) {
		t.Fatal("read-only attach opened write gate")
	}
	if ev := receiveProbe(t, events); ev.State != string(StateReadOnly) {
		t.Fatalf("event: %+v", ev)
	}
	logs, err := f.repo.ListEventLogs(f.ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].EventType != EventReadOnly || logs[0].Severity != repository.EventLogSeverityWarn {
		t.Fatalf("read-only audit: %+v", logs)
	}
	waitProbeIdle(t, f.mon)
	// Explicit adoption of this same read-only volume also needs no marker write.
	if st, err = f.mon.Adopt(f.ctx, ""); err != nil || st.State != StateReadOnly {
		t.Fatalf("read-only adoption: %+v %v", st, err)
	}
	waitProbeIdle(t, f.mon)
	f.mon.store = f.store
	if st = f.mon.Check(f.ctx); st.State != StateAttached || f.mon.Ready() != nil {
		t.Fatalf("recovery: %+v", st)
	}
	if ev := receiveProbe(t, events); ev.State != string(StateAttached) {
		t.Fatalf("recovery event: %+v", ev)
	}
}

func TestAdoptCannotTrustForeignReadOnlyStorage(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	waitProbeIdle(t, f.mon)
	other, err := storage.NewStorageID()
	if err != nil {
		t.Fatal(err)
	}
	f.writeMarker(t, other)
	f.mon.store = readOnlyProbe{f.store}
	st, err := f.mon.Adopt(f.ctx, "")
	if err == nil || st.Readable() || f.mon.Ready() == nil {
		t.Fatalf("failed marker repair trusted foreign storage: %+v %v", st, err)
	}
	if id, err := storage.ReadMarker(f.ctx, f.store); err != nil || id != other {
		t.Fatalf("read-only marker changed: %q %v", id, err)
	}
}

type withoutIdentity struct{ storage.Storage }

func TestUnsupportedBackendStaysUnavailable(t *testing.T) {
	f := newFixture(t)
	mon := New(f.repo, withoutIdentity{f.store}, f.bus, slog.New(slog.DiscardHandler), "custom", "test")
	st, err := mon.Attach(f.ctx)
	if !errors.Is(err, storage.ErrUnreachable) || st.Readable() || mon.Ready() == nil {
		t.Fatalf("unsupported backend: %+v %v", st, err)
	}
	if st.Reason == "" {
		t.Fatal("unsupported backend has no explanation")
	}
	if st, err = mon.Adopt(f.ctx, ""); err == nil || st.Readable() {
		t.Fatalf("unsupported adopt: %+v %v", st, err)
	}
}
