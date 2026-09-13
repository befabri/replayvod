// Package mediastore owns the boundary between recording rows and media effects.
// All writers and deletion paths share one Store and its per-recording locks.
package mediastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// Gate verifies backend identity and returns a typed storage readiness error.
type Gate interface{ Verify(context.Context) error }

// ErrContentChanged rejects prepared input that no longer matches its digest.
var ErrContentChanged = errors.New("prepared media bytes changed")

// Store serializes media effects with recording ownership and durable publication
// records; construct one per backend and share it with every media consumer.
type Store struct {
	backend     storage.Storage
	repo        repository.Repository
	gate        Gate
	locks       *recordinglock.Locks
	scratch     *Scratch
	reconcileMu sync.Mutex
	after       string
}

// New requires non-nil dependencies and a nonempty scratch directory; it panics
// when any dependency is missing.
func New(repo repository.Repository, backend storage.Storage, gate Gate, locks *recordinglock.Locks, scratchDir string) *Store {
	if repo == nil || backend == nil || gate == nil || locks == nil || scratchDir == "" {
		panic("managed media requires repository, backend, readiness, shared locks and scratch directory")
	}
	return &Store{backend: backend, repo: repo, gate: gate, locks: locks, scratch: NewScratch(scratchDir)}
}

// Verify performs a fresh readiness check through the configured gate.
func (s *Store) Verify(ctx context.Context) error { return s.gate.Verify(ctx) }

// Ready returns cached readiness when supported, otherwise a bounded fresh check.
func (s *Store) Ready() error {
	if g, ok := s.gate.(interface{ Ready() error }); ok {
		return g.Ready()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Verify(ctx)
}

// Open verifies readable storage before returning a handle the caller must close.
func (s *Store) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if err := s.Verify(ctx); !storage.CanRead(err) {
		return nil, err
	}
	f, err := s.backend.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := s.Verify(ctx); !storage.CanRead(err) {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Stat returns object metadata only while the backend remains readable.
func (s *Store) Stat(ctx context.Context, key string) (storage.FileInfo, error) {
	if err := s.Verify(ctx); !storage.CanRead(err) {
		return storage.FileInfo{}, err
	}
	info, err := s.backend.Stat(ctx, key)
	if verdict := s.Verify(ctx); !storage.CanRead(verdict) {
		return storage.FileInfo{}, verdict
	}
	return info, err
}

// Exists reports object presence only while the backend remains readable.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if err := s.Verify(ctx); !storage.CanRead(err) {
		return false, err
	}
	found, err := s.backend.Exists(ctx, key)
	if verdict := s.Verify(ctx); !storage.CanRead(verdict) {
		return false, verdict
	}
	return found, err
}

// Publication returns the journal record, including unresolved uploads.
func (s *Store) Publication(ctx context.Context, key string) (*repository.MediaPublication, error) {
	return s.repo.GetMediaPublication(ctx, key)
}

// WaveformKey returns the committed waveform reference without inferring a path.
func (s *Store) WaveformKey(ctx context.Context, videoID int64) (string, error) {
	return s.repo.GetVideoWaveformKey(ctx, videoID)
}

// Scratch returns the shared workspace owner and capacity accounting.
func (s *Store) Scratch() *Scratch { return s.scratch }

// CapacityRoot returns the local media filesystem used for cache budgeting.
// Scratch capacity is accounted separately.
func (s *Store) CapacityRoot() string {
	if local, ok := s.backend.(*storage.LocalStorage); ok {
		return local.Root
	}
	return ""
}

// Recording holds exclusive media ownership for a recording until Close.
// Keep it open through publication and the transaction that stores its references.
type Recording struct {
	*Store
	VideoID int64
	claim   *repository.AttemptClaim
	unlock  func()
	mu      sync.Mutex
	closed  bool
}

// Lock waits for exclusive recording ownership; the caller must close the result.
func (s *Store) Lock(ctx context.Context, id int64) (*Recording, error) {
	unlock, err := s.locks.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Recording{Store: s, VideoID: id, unlock: unlock}, nil
}

// ForAttempt acquires recording ownership and guards publication against claim.
// The caller must close the result.
func (s *Store) ForAttempt(ctx context.Context, claim repository.AttemptClaim) (*Recording, error) {
	r, err := s.Lock(ctx, claim.VideoID)
	if err != nil {
		return nil, err
	}
	r.claim = &claim
	return r, nil
}

// NextSnapshotIndex finds an unused position at or after start, including
// unjournaled historical files; keep recording ownership through Save.
func (r *Recording) NextSnapshotIndex(ctx context.Context, filename string, start int) (int, error) {
	for index := max(start, 0); ; index++ {
		key := storagekeys.Snapshot(filename, index)
		_, err := r.Publication(ctx, key)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return 0, err
		}
		if errors.Is(err, repository.ErrNotFound) {
			exists, err := r.Exists(ctx, key)
			if err != nil {
				return 0, err
			}
			if !exists {
				return index, nil
			}
		}
	}
}

// Close waits for an active media effect and releases ownership once.
func (r *Recording) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.unlock()
	}
}
func (r *Recording) eligible(ctx context.Context, tx repository.Repository) error {
	var v *repository.Video
	var err error
	if r.claim != nil {
		v, err = repository.GuardAttempt(ctx, tx, *r.claim)
	} else {
		v, err = tx.GetVideoForUpdate(ctx, r.VideoID)
	}
	if err != nil {
		return err
	}
	if v.DeletedAt != nil || v.DeleteRequestedAt != nil {
		return repository.ErrStaleExecution
	}
	if r.claim != nil && v.Status != repository.VideoStatusRunning {
		return repository.ErrStaleExecution
	}
	return nil
}

// Commit retains publication ownership through the short reference transaction.
func (r *Recording) Commit(ctx context.Context, fn func(repository.Repository) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return repository.ErrStaleExecution
	}
	if err := r.Verify(ctx); err != nil {
		return err
	}
	return r.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := r.eligible(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Save journals immutable, seekable input before uploading it.
// A key cannot be reused with different bytes or clear an ambiguous prior upload.
func (r *Recording) Save(ctx context.Context, key string, input io.Reader) error {
	return r.save(ctx, key, input, "")
}

// SavePrepared rejects bytes that differ from digest before journaling them.
// The caller must keep input immutable until SavePrepared returns.
func (r *Recording) SavePrepared(ctx context.Context, key string, input io.ReadSeeker, digest string) error {
	if digest == "" {
		return ErrContentChanged
	}
	return r.save(ctx, key, input, digest)
}

func (r *Recording) save(ctx context.Context, key string, input io.Reader, expectedDigest string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return repository.ErrStaleExecution
	}
	if err := r.Verify(ctx); err != nil {
		return err
	}
	// Callers supply seekable prepared outputs (files or byte readers). This
	// prevents hidden whole-video buffering and gives retries the same bytes.
	source, ok := input.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("media publication requires a prepared seekable input")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, mediaReader{ctx, source})
	if err != nil {
		return err
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if expectedDigest != "" && digest != expectedDigest {
		return ErrContentChanged
	}
	previousUnknown := false
	err = r.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := r.eligible(ctx, tx); err != nil {
			return err
		}
		previous, err := tx.GetMediaPublication(ctx, key)
		if err == nil {
			if previous.VideoID != r.VideoID || previous.Digest != digest || previous.DeleteRequested {
				return repository.ErrStaleExecution
			}
			previousUnknown = previous.Unresolved
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		_, err = tx.BeginMediaPublication(ctx, repository.MediaPublication{Key: key, VideoID: r.VideoID, Digest: digest, SizeBytes: size})
		return err
	})
	if err != nil {
		return err
	}
	if err = r.Verify(ctx); err != nil {
		return err
	}
	if err = r.backend.Save(ctx, key, source); err != nil {
		return err
	}
	if err = r.Verify(ctx); err != nil {
		return err
	}
	if previousUnknown {
		return nil
	}
	return r.repo.ConfirmMediaPublication(ctx, key, digest)
}

// Delete records cleanup before removing an object and retains unresolved uploads
// for later reconciliation. Missing objects are safe to retry.
func (r *Recording) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return repository.ErrStaleExecution
	}
	if err := r.Verify(ctx); !storage.CanDelete(err) {
		return err
	}
	// The intent survives even if this call observes absence. A prior remote
	// operation can materialize later, after application rows have been removed.
	if err := r.repo.RequestMediaPublicationDelete(ctx, key); err != nil {
		return err
	}
	if err := r.Verify(ctx); !storage.CanDelete(err) {
		return err
	}
	if err := r.backend.Delete(ctx, key); err != nil {
		return err
	}
	if err := r.Verify(ctx); !storage.CanDelete(err) {
		return err
	}
	return r.repo.DeleteMediaPublication(ctx, key) // retains unresolved records
}

// PurgePublications covers uploads not yet referenced by a part or artifact row.
func (r *Recording) PurgePublications(ctx context.Context) error {
	return r.PrunePublications(ctx, func(repository.MediaPublication) bool { return true })
}

// PrunePublications deletes selected publications in bounded pages.
// It continues after individual failures and returns their joined errors.
func (r *Recording) PrunePublications(ctx context.Context, remove func(repository.MediaPublication) bool) error {
	var errs []error
	for after := ""; ; {
		rows, err := r.repo.ListRecordingPublications(ctx, r.VideoID, after, 100)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		for _, row := range rows {
			after = row.Key
			if remove(row) {
				if err := r.Delete(ctx, row.Key); err != nil && len(errs) < 16 {
					errs = append(errs, fmt.Errorf("delete %s: %w", row.Key, err))
				}
			}
		}
		if len(rows) < 100 || ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
	}
}

// Reconcile retries pending deletions in bounded pages.
// Ambiguous uploads remain discoverable because remote completion may follow deletion.
func (s *Store) Reconcile(ctx context.Context) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	rows, err := s.repo.ListMediaPublications(ctx, s.after, 100)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		s.after = ""
		return nil
	}
	var errs []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
		s.after = row.Key
		if err := s.reconcilePublication(ctx, row); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) reconcilePublication(ctx context.Context, row repository.MediaPublication) error {
	r, err := s.Lock(ctx, row.VideoID)
	if err != nil {
		return err
	}
	defer r.Close()
	fresh, err := s.repo.GetMediaPublication(ctx, row.Key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Discovery precedes the lock, so a completed delete may have allowed a new
	// publication at this key; a different owner requires its own recording lock.
	if fresh.VideoID != row.VideoID {
		return nil
	}
	row = *fresh
	if !row.DeleteRequested {
		v, err := s.repo.GetVideo(ctx, row.VideoID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		if err == nil {
			// Missing-media tombstones keep their objects and references for
			// restoration. Only an explicit permanent deletion authorizes purge.
			if v.DeletedAt == nil || v.DeletionKind == nil ||
				(*v.DeletionKind != repository.DeletionKindManual && *v.DeletionKind != repository.DeletionKindRetention) {
				return nil
			}
		}
	}
	return r.Delete(ctx, row.Key)
}

type mediaReader struct {
	ctx    context.Context
	source io.Reader
}

func (r mediaReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
