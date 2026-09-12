// Package storagehealth owns the server's notion of storage readiness: whether
// the configured backend is reachable and carries the identity the database
// recorded. Recording, scanning and the playback tombstone consult it before
// trusting what storage says.
package storagehealth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/eventlog"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

const (
	DefaultInterval     = 30 * time.Second
	DefaultProbeTimeout = 10 * time.Second
	announcementTimeout = 5 * time.Second
	// witnessSample is how many known recordings a first attach looks for before
	// initializing a markerless root.
	witnessSample = 64

	EventDomain     = "storage"
	EventAttached   = "attached"
	EventUnattached = "unattached"
	EventReadOnly   = "read_only"
	EventFull       = "full"
	EventAdopted    = "adopted"
)

// State is the readiness verdict of the last check.
type State string

const (
	// StateAttached means storage is reachable, carries the expected identity
	// and accepts writes.
	StateAttached State = "attached"
	// StateReadOnly means storage is attached but refuses writes: playback
	// works, recording does not.
	StateReadOnly State = "read_only"
	// StateFull preserves playback and cleanup while writes wait for capacity.
	StateFull State = "full"
	// StateUnattached means storage answered but is not this install's: the
	// marker is missing, malformed or belongs to another database.
	StateUnattached State = "unattached"
	// StateUnreachable means the root, bucket or marker could not be read.
	StateUnreachable State = "unreachable"
)

type Status struct {
	State     State
	Reason    string
	Backend   string
	Location  string
	StorageID string
	CheckedAt time.Time
}

// Readable reports whether what storage says about its objects can be
// trusted: media may be served and missing files may be tombstoned.
func (s Status) Readable() bool {
	return s.State == StateAttached || s.State == StateReadOnly || s.State == StateFull
}

type Option func(*Monitor)

func WithInterval(d time.Duration) Option {
	return func(m *Monitor) {
		if d > 0 {
			m.interval = d
		}
	}
}

// WithProbeTimeout bounds storage I/O, including waiting for another probe.
// A successful cached verdict expires after one interval plus this timeout.
func WithProbeTimeout(d time.Duration) Option {
	return func(m *Monitor) {
		if d > 0 {
			m.probeTimeout = d
		}
	}
}

// verdict is immutable after publication. expires retains the monotonic clock;
// the public CheckedAt is UTC for display and serialization.
type verdict struct {
	status  Status
	err     error
	expires time.Time
}

// Monitor attaches storage at boot, re-checks it on an interval and on
// demand, and records every transition as an event log row and a bus event.
type Monitor struct {
	repo         repository.Repository
	store        storage.Identity
	bus          *eventbus.Buses
	log          *slog.Logger
	backend      string
	location     string
	interval     time.Duration
	probeTimeout time.Duration

	// The gate stays occupied until both the caller and its I/O worker finish.
	// Even a filesystem syscall that ignores cancellation can strand only one
	// worker. expected and loaded are owned exclusively by that worker.
	probeGate chan struct{}
	expected  string
	loaded    bool
	cache     atomic.Pointer[verdict]
}

func New(repo repository.Repository, store storage.Storage, bus *eventbus.Buses, log *slog.Logger, backend, location string, opts ...Option) *Monitor {
	identity, _ := store.(storage.Identity)
	m := &Monitor{
		repo: repo, store: identity, bus: bus,
		log:          log.With("domain", "storagehealth"),
		backend:      backend,
		location:     location,
		interval:     DefaultInterval,
		probeTimeout: DefaultProbeTimeout,
		probeGate:    make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Attach establishes the identity at boot. A database without a recorded id
// takes the marker storage carries or writes a fresh one and records it. The
// error is the typed readiness error; the status is recorded either way.
func (m *Monitor) Attach(ctx context.Context) (Status, error) {
	return m.probe(ctx, func(ctx context.Context) error { return m.attachLocked(ctx) })
}

// attachLocked establishes the identity. Without a recorded id and without a
// marker, the storage is initialized only when it is plainly ours: a library
// that knows recordings must find at least one of them there, or the volume
// is an empty mount point standing in for the real one and initializing it
// would hand the scan the whole library to tombstone. Explicit adoption bypasses
// this witness.
func (m *Monitor) attachLocked(ctx context.Context) error {
	if err := m.loadExpectedLocked(ctx); err != nil {
		return err
	}
	if m.expected == "" {
		if err := m.witnessLibraryLocked(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	res, err := storage.Attach(ctx, m.store, m.expected)
	return m.rememberIdentity(ctx, res, err)
}

// Read-only and full storage still have a verified identity. Persist
// that identity before publishing the verdict, without ever opening the write gate.
func (m *Monitor) rememberIdentity(ctx context.Context, res storage.AttachResult, verdictErr error) error {
	if !storage.CanRead(verdictErr) {
		return verdictErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if res.ID != "" && res.ID != m.expected {
		if _, err := m.repo.SetStorageID(ctx, res.ID); err != nil {
			return fmt.Errorf("record storage id: %w", err)
		}
		m.expected = res.ID
		m.log.Info("storage identity recorded", "outcome", res.Outcome, "backend", m.backend, "location", m.location)
	}
	return verdictErr
}

// witnessLibraryLocked refuses to initialize storage that carries no marker
// and holds none of the recordings the database knows. A library without
// recordings, or one with at least one recording present, may be initialized;
// storage that cannot be read is reported as such.
func (m *Monitor) witnessLibraryLocked(ctx context.Context) error {
	if _, err := storage.ReadMarker(ctx, m.store); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	candidates, err := m.repo.ListVideosForStorageWitness(ctx, witnessSample)
	if err != nil {
		return fmt.Errorf("list recordings for storage witness: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	ids := make([]int64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.VideoID
	}
	parts, err := m.repo.ListVideoPartsForVideos(ctx, ids)
	if err != nil {
		return fmt.Errorf("list parts for storage witness: %w", err)
	}
	byVideo := make(map[int64][]repository.VideoPart, len(candidates))
	for _, p := range parts {
		byVideo[p.VideoID] = append(byVideo[p.VideoID], p)
	}
	for _, c := range candidates {
		for _, key := range storagekeys.MediaPaths(c.Filename, c.Status, byVideo[c.VideoID]) {
			if err := ctx.Err(); err != nil {
				return err
			}
			ok, err := m.store.Exists(ctx, key)
			if err != nil {
				return fmt.Errorf("%w: %w", storage.ErrUnreachable, err)
			}
			if ok {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: storage carries no identity marker and none of the library's recordings; mount the data volume, or adopt this storage to start over", storage.ErrUnattached)
}

func (m *Monitor) loadExpectedLocked(ctx context.Context) error {
	if m.loaded {
		return nil
	}
	settings, err := m.repo.GetServerSettings(ctx)
	switch {
	case errors.Is(err, repository.ErrNotFound):
	case err != nil:
		return fmt.Errorf("load storage id: %w", err)
	default:
		m.expected = settings.StorageID
	}
	m.loaded = true
	return nil
}

// Check runs a fresh readiness probe and records the outcome. Until an
// identity is recorded it retries the attach instead, so storage that was
// unreachable at boot is initialized as soon as it answers.
func (m *Monitor) Check(ctx context.Context) Status {
	status, err := m.probe(ctx, m.checkLocked)
	if err != nil && ctx.Err() == nil {
		// A previous caller may have cancelled while its filesystem operation
		// stayed stuck. Even without admission to the probe gate, the monitor
		// must announce expiry so the dashboard does not retain a green banner.
		return m.expire(ctx)
	}
	return status
}

func (m *Monitor) checkLocked(ctx context.Context) error {
	if !m.loaded || m.expected == "" {
		return m.attachLocked(ctx)
	}
	return storage.Ready(ctx, m.store, m.expected)
}

// Verify is Check for callers that need the typed error of a fresh probe.
func (m *Monitor) Verify(ctx context.Context) error {
	_, err := m.probe(ctx, m.checkLocked)
	return err
}

// Ready reads only the cache. A successful verdict has a finite lease, so a
// stalled monitor can never leave recording or /health trusting it forever.
func (m *Monitor) Ready() error {
	return m.cached().err
}

func (m *Monitor) Status() Status {
	return m.cached().status
}

func (m *Monitor) cached() verdict {
	v := m.cache.Load()
	if v == nil {
		err := fmt.Errorf("%w: storage not checked yet", storage.ErrUnreachable)
		return verdict{status: Status{State: StateUnreachable, Reason: err.Error(), Backend: m.backend, Location: m.location}, err: err}
	}
	copy := *v
	if copy.status.Readable() && !time.Now().Before(copy.expires) {
		copy.err = fmt.Errorf("%w: storage readiness check expired; waiting for a fresh probe", storage.ErrUnreachable)
		copy.status.State = StateUnreachable
		copy.status.Reason = copy.err.Error()
	}
	return copy
}

// expire records an expired lease exactly once. Compare-and-swap prevents a
// timeout waiting for the gate from overwriting a concurrent fresh verdict.
func (m *Monitor) expire(ctx context.Context) Status {
	for {
		prev := m.cache.Load()
		if prev == nil || !prev.status.Readable() || time.Now().Before(prev.expires) {
			return m.Status()
		}
		stale := *prev
		stale.err = fmt.Errorf("%w: storage readiness check expired; waiting for a fresh probe", storage.ErrUnreachable)
		stale.status.State, stale.status.Reason = StateUnreachable, stale.err.Error()
		if m.cache.CompareAndSwap(prev, &stale) {
			m.announceBounded(ctx, stale.status, false)
			return stale.status
		}
	}
}

// probe serializes identity changes and checks without making cached readers
// wait for I/O. Only this caller publishes the result; a worker that returns
// after its deadline cannot overwrite a timeout with a late success.
func (m *Monitor) probe(ctx context.Context, action func(context.Context) error) (Status, error) {
	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
	defer cancel()
	if err := probeCtx.Err(); err != nil {
		return m.Status(), probeError(err)
	}
	select {
	case m.probeGate <- struct{}{}:
	case <-probeCtx.Done():
		return m.Status(), probeError(probeCtx.Err())
	}
	// select may have admitted an already-cancelled caller. In particular an
	// abandoned adopt must not start writing a marker after waiting its turn.
	if err := probeCtx.Err(); err != nil {
		<-m.probeGate
		return m.Status(), probeError(err)
	}
	type result struct {
		id  string
		err error
	}
	results := make(chan result, 1)
	published := make(chan struct{})
	defer close(published)
	go func() {
		defer func() {
			<-published
			<-m.probeGate
		}()
		err := probeCtx.Err()
		if err == nil {
			if m.store == nil {
				err = fmt.Errorf("%w: storage backend does not support identity checks; configure a supported backend", storage.ErrUnreachable)
			} else {
				err = action(probeCtx)
			}
		}
		results <- result{id: m.expected, err: err}
	}()
	var res result
	select {
	case res = <-results:
	case <-probeCtx.Done():
	}
	// A cancelled request does not change shared readiness, but Verify must
	// return its error instead of reusing an earlier successful verdict.
	if err := ctx.Err(); err != nil {
		return m.Status(), probeError(err)
	}
	if err := probeCtx.Err(); err != nil {
		res = result{id: m.Status().StorageID, err: probeError(err)}
	}
	if stateOf(res.err) == StateUnreachable && !errors.Is(res.err, storage.ErrUnreachable) {
		res.err = fmt.Errorf("%w: %w", storage.ErrUnreachable, res.err)
	}
	return m.record(ctx, res.id, res.err), res.err
}

func probeError(err error) error {
	return fmt.Errorf("%w: storage probe did not finish: %w", storage.ErrUnreachable, err)
}

// Run re-checks storage on the interval until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Check(ctx)
		}
	}
}

// Adopt claims the reachable storage for this database: the marker is
// rewritten with the recorded id (or, for a database without one, taken or
// created as at boot, without the library witness). Callers confirm with the
// operator first; adopting an empty volume means the next scan tombstones
// every recording it lacks.
func (m *Monitor) Adopt(ctx context.Context, actorUserID string) (Status, error) {
	status, err := m.probe(ctx, m.adoptLocked)
	if !storage.CanRead(err) {
		return status, err
	}
	actor := actorUserID
	eventlog.EmitBy(ctx, m.repo, m.bus, m.log, &actor, EventDomain, EventAdopted, repository.EventLogSeverityInfo,
		fmt.Sprintf("storage adopted by %s: %s %s", actorUserID, m.backend, m.location),
		map[string]any{"backend": m.backend, "location": m.location, "storage_id": status.StorageID})
	return status, nil
}

func (m *Monitor) adoptLocked(ctx context.Context) error {
	if err := m.loadExpectedLocked(ctx); err != nil {
		return err
	}
	res, err := storage.Adopt(ctx, m.store, m.expected)
	return m.rememberIdentity(ctx, res, err)
}

// record publishes before any audit I/O. Expiry can be recorded by callers
// waiting for the probe gate, so publication uses compare-and-swap too.
// Readers use an immutable snapshot and never wait for the audit database.
func (m *Monitor) record(ctx context.Context, id string, err error) Status {
	for {
		state := stateOf(err)
		prev := m.cache.Load()
		first := prev == nil
		now := time.Now()
		status := Status{
			State:     state,
			Backend:   m.backend,
			Location:  m.location,
			StorageID: id,
			CheckedAt: now.UTC(),
		}
		if err != nil {
			status.Reason = err.Error()
		}
		next := &verdict{status: status, err: err, expires: now.Add(m.interval + m.probeTimeout)}
		if !m.cache.CompareAndSwap(prev, next) {
			continue
		}
		if !first && prev.status.State == state && (!status.Readable() || now.Before(prev.expires)) {
			return status
		}
		m.announceBounded(ctx, status, first)
		return status
	}
}

func (m *Monitor) announceBounded(ctx context.Context, status Status, first bool) {
	// Internal probe deadlines still need to announce an outage. Keep this
	// bounded and independent from the expired I/O context.
	announceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), announcementTimeout)
	defer cancel()
	m.announce(announceCtx, status, first)
}

func (m *Monitor) announce(ctx context.Context, s Status, first bool) {
	data := map[string]any{"backend": m.backend, "location": m.location, "state": string(s.State)}
	switch s.State {
	case StateAttached:
		// A healthy boot is not an event; a recovery is.
		if first {
			m.log.Info("storage attached", "backend", m.backend, "location", m.location)
		} else {
			eventlog.Emit(ctx, m.repo, m.bus, m.log, EventDomain, EventAttached, repository.EventLogSeverityInfo,
				fmt.Sprintf("storage attached again: %s %s", m.backend, m.location), data)
		}
	case StateReadOnly:
		data["reason"] = s.Reason
		eventlog.Emit(ctx, m.repo, m.bus, m.log, EventDomain, EventReadOnly, repository.EventLogSeverityWarn,
			"storage is read-only; recording is paused: "+s.Reason, data)
	case StateFull:
		data["reason"] = s.Reason
		eventlog.Emit(ctx, m.repo, m.bus, m.log, EventDomain, EventFull, repository.EventLogSeverityWarn,
			"storage is full; recording is paused, cleanup remains available: "+s.Reason, data)
	default:
		data["reason"] = s.Reason
		eventlog.Emit(ctx, m.repo, m.bus, m.log, EventDomain, EventUnattached, repository.EventLogSeverityError,
			"storage is not attached; recording and scanning are paused: "+s.Reason, data)
	}
	if m.bus != nil {
		m.bus.StorageStatus.Publish(eventbus.StorageStatusEvent{State: string(s.State), At: s.CheckedAt})
	}
}

func stateOf(err error) State {
	switch {
	case err == nil:
		return StateAttached
	case errors.Is(err, storage.ErrReadOnly):
		return StateReadOnly
	case errors.Is(err, storage.ErrFull):
		return StateFull
	case errors.Is(err, storage.ErrUnattached):
		return StateUnattached
	default:
		return StateUnreachable
	}
}
