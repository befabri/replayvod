// Package mediatest builds managed media with controllable test readiness. Production
// code must construct mediastore at the composition root with its health monitor.
package mediatest

import (
	"context"
	"sync"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// GateFunc adapts a readiness function to mediastore.Gate.
type GateFunc func(context.Context) error

// Verify returns the readiness error from f.
func (f GateFunc) Verify(ctx context.Context) error { return f(ctx) }

type gate struct {
	mu    sync.RWMutex
	value mediastore.Gate
}

func (g *gate) Verify(ctx context.Context) error {
	g.mu.RLock()
	v := g.value
	g.mu.RUnlock()
	return v.Verify(ctx)
}
func (g *gate) Ready() error {
	g.mu.RLock()
	v := g.value
	g.mu.RUnlock()
	if ready, ok := v.(interface{ Ready() error }); ok {
		return ready.Ready()
	}
	return v.Verify(context.Background())
}

type fixture struct {
	repo    repository.Repository
	raw     storage.Storage
	gate    *gate
	locks   *recordinglock.Locks
	dir     string
	cleanup func(func())
}

var fixtures sync.Map

func (f fixture) newStore() *mediastore.Store {
	s := mediastore.New(f.repo, f.raw, f.gate, f.locks, f.dir)
	fixtures.Store(s, f)
	f.cleanup(func() { fixtures.Delete(s) })
	return s
}

// New returns a managed store owned by t, with a temporary scratch directory.
// Nil dependencies use a migrated SQLite repository, local storage, an always-ready
// gate, and fresh recording locks.
func New(t *testing.T, repo repository.Repository, raw storage.Storage, readiness mediastore.Gate, locks *recordinglock.Locks) *mediastore.Store {
	t.Helper()
	return NewAt(t, repo, raw, readiness, locks, t.TempDir())
}

// NewAt returns a managed store owned by t, using dir for scratch.
// Nil dependencies use the same defaults as New.
func NewAt(t *testing.T, repo repository.Repository, raw storage.Storage, readiness mediastore.Gate, locks *recordinglock.Locks, dir string) *mediastore.Store {
	t.Helper()
	if repo == nil {
		repo = sqliteadapter.New(testdb.NewSQLiteDB(t))
	}
	if raw == nil {
		var err error
		raw, err = storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
	}
	if readiness == nil {
		readiness = GateFunc(func(context.Context) error { return nil })
	}
	if locks == nil {
		locks = &recordinglock.Locks{}
	}
	f := fixture{
		repo: repo, raw: raw, gate: &gate{value: readiness},
		locks: locks, dir: dir, cleanup: t.Cleanup,
	}
	return f.newStore()
}

// SetGate replaces readiness for fixture s and stores derived with WithLocks.
// The replacement gate must be non-nil.
func SetGate(s *mediastore.Store, v mediastore.Gate) {
	f, ok := fixtures.Load(s)
	if !ok {
		panic("not a media fixture")
	}
	g := f.(fixture).gate
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = v
}

// WithLocks returns a store sharing fixture s's repository, backend, readiness
// gate, and scratch directory, with replacement locks. The original fixture's
// test owns the returned store. The replacement locks must be non-nil.
func WithLocks(s *mediastore.Store, locks *recordinglock.Locks) *mediastore.Store {
	value, ok := fixtures.Load(s)
	if !ok {
		panic("not a media fixture")
	}
	f := value.(fixture)
	f.locks = locks
	return f.newStore()
}

// Raw returns fixture s's underlying storage backend.
func Raw(s *mediastore.Store) storage.Storage {
	v, _ := fixtures.Load(s)
	return v.(fixture).raw
}

// Locks returns fixture s's recording locks.
func Locks(s *mediastore.Store) *recordinglock.Locks {
	v, _ := fixtures.Load(s)
	return v.(fixture).locks
}
