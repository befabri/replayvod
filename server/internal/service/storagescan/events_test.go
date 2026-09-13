package storagescan

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

func (f fixture) scanSummaries(t *testing.T) []repository.EventLog {
	t.Helper()
	rows, err := f.repo.ListEventLogs(f.ctx, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	var summaries []repository.EventLog
	for _, row := range rows {
		if row.Domain == EventDomain && row.EventType == EventScanReconciled {
			summaries = append(summaries, row)
		}
	}
	// SQLite event timestamps can tie within a second; IDs determine the newest event.
	slices.SortFunc(summaries, func(a, b repository.EventLog) int { return cmp.Compare(b.ID, a.ID) })
	return summaries
}

func assertScanSummary(t *testing.T, row repository.EventLog, report Report, failed bool) {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(row.Data, &data); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"scanned": float64(report.Scanned), "missing": float64(report.Missing), "partial": float64(report.Partial),
		"tombstoned": float64(report.Tombstoned), "restored": float64(report.Restored),
		"complete": report.Complete, "failed": failed,
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("summary = %v, want %v", data, want)
	}
	severity := repository.EventLogSeverityInfo
	if failed {
		severity = repository.EventLogSeverityWarn
	}
	if row.Severity != severity {
		t.Fatalf("severity = %q, want %q", row.Severity, severity)
	}
}

func nextScanEvent(t *testing.T, events <-chan eventbus.EventLogEvent, kind string) eventbus.EventLogEvent {
	t.Helper()
	select {
	case ev := <-events:
		if ev.Domain != EventDomain || ev.EventType != kind {
			t.Fatalf("event = %+v, want storage.%s", ev, kind)
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("missing scan bus event")
		return eventbus.EventLogEvent{}
	}
}

func noScanEvent(t *testing.T, events <-chan eventbus.EventLogEvent) {
	t.Helper()
	// Publication is synchronous with the operation's return.
	select {
	case ev := <-events:
		t.Fatalf("unexpected additional event: %+v", ev)
	default:
	}
}

func TestSweepSummarizesMultiplePagesAndRestoresWithoutEventFlood(t *testing.T) {
	f := newFixture(t)
	bus := eventbus.New()
	events := bus.EventLogs.Subscribe(t.Context())
	svc := New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog(), WithEventBus(bus))
	const total = scanPageSize + 3
	for i := range total {
		f.seed(t, fmt.Sprintf("bulk-%d", i), 1)
	}
	f.seed(t, "present", 1, 1)
	f.seed(t, "partial", 2, 1)

	report, err := svc.Sweep(f.ctx)
	if err != nil || report.Tombstoned != total || report.Partial != 1 || !report.Complete {
		t.Fatalf("sweep = %+v, %v", report, err)
	}
	rows := f.scanSummaries(t)
	if len(rows) != 1 || f.missingEvents(t) != 0 {
		t.Fatalf("summaries = %d, individual events = %d", len(rows), f.missingEvents(t))
	}
	assertScanSummary(t, rows[0], report, false)
	if ev := nextScanEvent(t, events, EventScanReconciled); ev.ID != rows[0].ID {
		t.Fatalf("SSE event did not mirror the summary row: %+v", ev)
	}
	noScanEvent(t, events)

	for i := range total {
		f.save(t, fmt.Sprintf("videos/bulk-%d-part01.mp4", i))
	}
	report, err = svc.Sweep(f.ctx)
	if err != nil || report.Restored != total || !report.Complete {
		t.Fatalf("restore sweep = %+v, %v", report, err)
	}
	rows = f.scanSummaries(t)
	if len(rows) != 2 || f.restoredEvents(t) != 0 {
		t.Fatalf("summaries = %d, individual restores = %d", len(rows), f.restoredEvents(t))
	}
	assertScanSummary(t, rows[0], report, false)
	nextScanEvent(t, events, EventScanReconciled)
	noScanEvent(t, events)

	if _, err := svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.scanSummaries(t)) != 2 {
		t.Fatal("unchanged sweep emitted another summary")
	}
	noScanEvent(t, events)
}

func TestSweepSummarySurvivesCancellationAndResumesWithoutDoubleCounting(t *testing.T) {
	f := newFixture(t)
	bus := eventbus.New()
	events := bus.EventLogs.Subscribe(t.Context())
	for i := range scanPageSize + 1 {
		f.seed(t, fmt.Sprintf("interrupted-%d", i), 1)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &pageTrackingRepo{Repository: f.repo, cancelOn: 2, cancel: cancel}
	report, err := New(repo, mediatest.New(t, repo, f.store, f.mon, nil), discardLog(), WithEventBus(bus)).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Tombstoned != scanPageSize {
		t.Fatalf("interrupted sweep = %+v, %v", report, err)
	}
	rows := f.scanSummaries(t)
	if len(rows) != 1 {
		t.Fatalf("committed page lost its summary: %d rows", len(rows))
	}
	assertScanSummary(t, rows[0], report, false)
	nextScanEvent(t, events, EventScanReconciled)
	noScanEvent(t, events)

	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog(), WithEventBus(bus)).Sweep(f.ctx)
	if err != nil || !report.Complete || report.Tombstoned != 1 {
		t.Fatalf("resumed sweep = %+v, %v", report, err)
	}
	rows = f.scanSummaries(t)
	if len(rows) != 2 {
		t.Fatalf("resume summaries = %d", len(rows))
	}
	assertScanSummary(t, rows[0], report, false)
	nextScanEvent(t, events, EventScanReconciled)
	noScanEvent(t, events)
}

type failTombstoneRepo struct {
	repository.Repository
	id  int64
	err error
}

func (r *failTombstoneRepo) TombstoneMissingVideo(ctx context.Context, id int64) (bool, error) {
	if id == r.id {
		return false, r.err
	}
	return r.Repository.TombstoneMissingVideo(ctx, id)
}

func TestSweepSummaryCountsOnlyCommittedChangesWhenOneUpdateFails(t *testing.T) {
	f := newFixture(t)
	f.seed(t, "gone-1", 1)
	failed := f.seed(t, "failed-update", 1)
	f.seed(t, "gone-2", 1)
	failure := errors.New("injected tombstone failure")
	repo := &failTombstoneRepo{Repository: f.repo, id: failed.ID, err: failure}
	report, err := New(repo, mediatest.New(t, repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if !errors.Is(err, failure) || report.Tombstoned != 2 || report.Missing != 3 {
		t.Fatalf("partly failed sweep = %+v, %v", report, err)
	}
	f.assertLive(t, failed.ID)
	rows := f.scanSummaries(t)
	if len(rows) != 1 {
		t.Fatalf("summary count = %d", len(rows))
	}
	assertScanSummary(t, rows[0], report, true)
}

type cancelAfterRestoreRepo struct {
	repository.Repository
	cancel context.CancelFunc
}

func (r *cancelAfterRestoreRepo) RestoreMissingVideo(ctx context.Context, id int64) error {
	err := r.Repository.RestoreMissingVideo(ctx, id)
	if err == nil {
		r.cancel()
	}
	return err
}

func TestSweepSummaryKeepsRestoreCommittedJustBeforeCancellation(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "returned", 1)
	f.tombstoneMissing(t, v.ID)
	f.save(t, "videos/returned-part01.mp4")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &cancelAfterRestoreRepo{Repository: f.repo, cancel: cancel}
	report, err := New(repo, mediatest.New(t, repo, f.store, f.mon, nil), discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Restored != 1 {
		t.Fatalf("interrupted restore = %+v, %v", report, err)
	}
	f.assertLive(t, v.ID)
	rows := f.scanSummaries(t)
	if len(rows) != 2 || f.restoredEvents(t) != 0 {
		t.Fatal("interrupted restore lost its summary or emitted an individual event")
	}
	assertScanSummary(t, rows[0], report, false)
}

type deleteBeforeTombstoneRepo struct{ repository.Repository }

func (r deleteBeforeTombstoneRepo) TombstoneMissingVideo(ctx context.Context, id int64) (bool, error) {
	if _, err := r.RequestVideoDelete(ctx, id); err != nil {
		return false, err
	}
	return r.Repository.TombstoneMissingVideo(ctx, id)
}

func TestSweepDoesNotAnnounceTombstoneWhenManualDeleteWins(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "manual-wins", 1)
	bus := eventbus.New()
	events := bus.EventLogs.Subscribe(t.Context())
	report, err := New(deleteBeforeTombstoneRepo{f.repo}, mediatest.New(t, deleteBeforeTombstoneRepo{f.repo}, f.store, f.mon, nil), discardLog(), WithEventBus(bus)).Sweep(f.ctx)
	if err != nil || report.Missing != 1 || report.Tombstoned != 0 {
		t.Fatalf("sweep = %+v, %v", report, err)
	}
	f.assertLive(t, v.ID)
	if len(f.scanSummaries(t)) != 0 || f.missingEvents(t) != 0 {
		t.Fatal("announced a tombstone whose update was refused")
	}
	noScanEvent(t, events)
}

type failScanAuditRepo struct{ repository.Repository }

func (r failScanAuditRepo) CreateEventLog(context.Context, *repository.EventLogInput) (*repository.EventLog, error) {
	return nil, errors.New("injected audit failure")
}

type deleteBeforeRestoreRepo struct {
	repository.Repository
	id int64
}

func (r deleteBeforeRestoreRepo) RestoreMissingVideo(ctx context.Context, id int64) error {
	if id == r.id {
		if _, err := r.RequestVideoDelete(ctx, id); err != nil {
			return err
		}
	}
	return r.Repository.RestoreMissingVideo(ctx, id)
}

func TestSweepSummaryDoesNotCountRestoreWhenManualDeleteWins(t *testing.T) {
	f := newFixture(t)
	deleted := f.seed(t, "delete-wins", 1)
	returned := f.seed(t, "returned", 1)
	f.tombstoneMissing(t, deleted.ID)
	f.save(t, "videos/delete-wins-part01.mp4")
	f.save(t, "videos/returned-part01.mp4")
	repo := deleteBeforeRestoreRepo{Repository: f.repo, id: deleted.ID}
	report, err := New(repo, mediatest.New(t, repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if !errors.Is(err, ErrNotRestorable) || report.Restored != 1 {
		t.Fatalf("restore race = %+v, %v", report, err)
	}
	f.assertLive(t, returned.ID)
	f.assertTombstonedMissing(t, deleted.ID)
	got, err := f.repo.GetVideo(f.ctx, deleted.ID)
	if err != nil || got.DeleteRequestedAt == nil {
		t.Fatalf("restore lost the manual delete: %+v, %v", got, err)
	}
	rows := f.scanSummaries(t)
	if len(rows) != 2 || f.restoredEvents(t) != 0 {
		t.Fatal("sweep did not produce one summary for the successful restore")
	}
	assertScanSummary(t, rows[0], report, true)
}

func TestSweepAuditFailureDoesNotUndoReconciliationOrPublishPhantomRow(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "gone", 1)
	bus := eventbus.New()
	events := bus.EventLogs.Subscribe(t.Context())
	report, err := New(failScanAuditRepo{f.repo}, mediatest.New(t, failScanAuditRepo{f.repo}, f.store, f.mon, nil), discardLog(), WithEventBus(bus)).Sweep(f.ctx)
	if err != nil || report.Tombstoned != 1 {
		t.Fatalf("sweep after audit failure = %+v, %v", report, err)
	}
	f.assertTombstonedMissing(t, v.ID)
	if len(f.scanSummaries(t)) != 0 {
		t.Fatal("failed audit created a row")
	}
	noScanEvent(t, events)
}

func TestPlaybackMissingAndManualRestoreKeepIndividualEvents(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "individual", 1)
	bus := eventbus.New()
	events := bus.EventLogs.Subscribe(t.Context())
	svc := New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog(), WithEventBus(bus))
	if changed, err := svc.MarkMissing(f.ctx, v.ID); err != nil || !changed {
		t.Fatalf("MarkMissing = %v, %v", changed, err)
	}
	nextScanEvent(t, events, EventRecordingMissing)
	if changed, err := svc.MarkMissing(f.ctx, v.ID); err != nil || changed {
		t.Fatalf("repeated MarkMissing = %v, %v", changed, err)
	}
	if err := svc.Restore(f.ctx, v.ID); !errors.Is(err, ErrStillMissing) {
		t.Fatalf("premature restore = %v", err)
	}
	noScanEvent(t, events)
	f.save(t, "videos/individual-part01.mp4")
	if err := svc.Restore(f.ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	nextScanEvent(t, events, EventRecordingRestored)
	if err := svc.Restore(f.ctx, v.ID); !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("repeated restore = %v", err)
	}
	if f.missingEvents(t) != 1 || f.restoredEvents(t) != 1 || len(f.scanSummaries(t)) != 0 {
		t.Fatal("individual actions lost their events or emitted a sweep summary")
	}
	noScanEvent(t, events)
}

func TestRemovalNotificationsCoverScanAndRestore(t *testing.T) {
	f := newFixture(t)
	bus := eventbus.New()
	events := bus.VideoChanges.Subscribe(t.Context())
	svc := New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog(), WithEventBus(bus))
	v := f.seed(t, "notify", 1)
	if report, err := svc.Sweep(f.ctx); err != nil || report.Tombstoned != 1 {
		t.Fatalf("scan: %+v %v", report, err)
	}
	select {
	case <-events:
	default:
		t.Fatal("missing scan invalidation")
	}
	f.assertTombstonedMissing(t, v.ID)
	if err := svc.Restore(f.ctx, v.ID); err == nil {
		t.Fatal("expected missing-media error")
	}
	select {
	case <-events:
		t.Fatal("failed restore published invalidation")
	default:
	}
	f.save(t, "videos/notify-part01.mp4")
	if err := svc.Restore(f.ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	default:
		t.Fatal("missing restore invalidation")
	}
	f.assertLive(t, v.ID)
}
