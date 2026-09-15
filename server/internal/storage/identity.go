package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"syscall"
)

// MarkerPath is the object at the storage root that carries the identity the
// database expects. It must travel with the data directory or bucket.
const MarkerPath = ".replayvod-storage"

var (
	// ErrUnreachable means the root, bucket or marker could not be read.
	ErrUnreachable = errors.New("storage unreachable")
	// ErrUnattached means storage answered but is not the one the database
	// expects: the marker is missing, malformed or carries another id.
	ErrUnattached = errors.New("storage unattached")
	// ErrReadOnly means storage is attached but refuses writes.
	ErrReadOnly = errors.New("storage read-only")
	// ErrFull means identity is verified but capacity or quota is exhausted.
	// Reads and deletions remain available so cleanup can reclaim space.
	ErrFull = errors.New("storage full")
	// ErrCallerGone means a readiness probe reached no verdict because the
	// caller's context ended first, which is not evidence about storage. It
	// wraps alongside ErrUnreachable so reads still fail closed.
	ErrCallerGone = errors.New("storage probe abandoned by caller")
)

// CanRead reports whether a readiness verdict authenticates the storage for
// reads, including missing-object reconciliation. Unknown failures fail closed.
func CanRead(err error) bool {
	return err == nil || errors.Is(err, ErrReadOnly) || errors.Is(err, ErrFull)
}

// CanDelete permits reclaiming space on a full, verified volume, but never on
// read-only, unreachable or foreign storage.
func CanDelete(err error) bool {
	return err == nil || errors.Is(err, ErrFull)
}

func CallerGone(err error) bool { return errors.Is(err, ErrCallerGone) }

// Identity is what the readiness checks need from a backend: object access for
// the marker plus a reachability probe of the root location.
type Identity interface {
	Storage
	RootProber
}

// WriteProber checks write access and capacity without writing user data.
type WriteProber interface {
	ProbeWrite(ctx context.Context) error
}

type AttachOutcome string

const (
	// AttachMatched means the marker carried the id the database expected.
	AttachMatched AttachOutcome = "matched"
	// AttachInitialized means neither side had an id: a fresh marker was written.
	AttachInitialized AttachOutcome = "initialized"
	// AttachAdopted means the database had no id and took the marker's.
	AttachAdopted AttachOutcome = "adopted"
)

type AttachResult struct {
	ID      string
	Outcome AttachOutcome
}

const storageIDBytes = 32

// NewStorageID returns a random identity in the marker's format.
func NewStorageID() (string, error) {
	var raw [storageIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("storage id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func validStorageID(id string) bool {
	if len(id) != 2*storageIDBytes {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// Attach establishes the identity between a database expecting expectedID and
// the storage. An empty expectedID is a database that has never attached (a
// first boot, an upgrade, or a restore next to existing storage): it takes the
// marker that is there or writes a new one. A recorded id is only verified,
// never rewritten; use Adopt for a deliberate adoption. A read-only or full result
// retains the identity so callers can remember it while refusing writes.
func Attach(ctx context.Context, s Identity, expectedID string) (AttachResult, error) {
	if expectedID != "" {
		return AttachResult{ID: expectedID, Outcome: AttachMatched}, Ready(ctx, s, expectedID)
	}
	id, err := ReadMarker(ctx, s)
	// Local filesystem operations may ignore cancellation while blocked. Do
	// not initialize a marker after an abandoned attach finally returns ENOENT.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return AttachResult{}, ctxErr
	}
	switch {
	case err == nil:
		return AttachResult{ID: id, Outcome: AttachAdopted}, Ready(ctx, s, id)
	case errors.Is(err, fs.ErrNotExist):
		id, err := NewStorageID()
		if err != nil {
			return AttachResult{}, err
		}
		if err := WriteMarker(ctx, s, id); err != nil {
			return AttachResult{}, err
		}
		return AttachResult{ID: id, Outcome: AttachInitialized}, Ready(ctx, s, id)
	default:
		return AttachResult{}, err
	}
}

// Adopt explicitly claims storage. Ordinary Attach never repairs a malformed or
// foreign marker; only this owner action may replace it. A matching read-only
// volume needs no rewrite and can still be attached for playback.
func Adopt(ctx context.Context, s Identity, expectedID string) (AttachResult, error) {
	res, err := Attach(ctx, s, expectedID)
	if !errors.Is(err, ErrUnattached) {
		return res, err
	}
	if err := s.ProbeRoot(ctx); err != nil {
		return AttachResult{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	id := expectedID
	if id == "" {
		id, err = NewStorageID()
		if err != nil {
			return AttachResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return AttachResult{}, err
	}
	if p, ok := s.(WriteProber); ok {
		if err := p.ProbeWrite(ctx); err != nil {
			return AttachResult{}, fmt.Errorf("%w: cannot replace identity marker: %w", ErrUnattached, err)
		}
	}
	if err := WriteMarker(ctx, s, id); err != nil {
		return AttachResult{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	return AttachResult{ID: id, Outcome: AttachAdopted}, Ready(ctx, s, id)
}

// Ready reports whether storage is reachable, carries expectedID, and (where
// the backend can tell) accepts writes. It never writes user data or the
// marker. The error wraps ErrUnreachable, ErrUnattached, ErrReadOnly or ErrFull.
func Ready(ctx context.Context, s Identity, expectedID string) error {
	if expectedID == "" {
		return fmt.Errorf("%w: no storage attached yet", ErrUnattached)
	}
	if err := s.ProbeRoot(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	id, err := ReadMarker(ctx, s)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: identity marker %s is missing", ErrUnattached, MarkerPath)
	case errors.Is(err, ErrUnattached):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	case id != expectedID:
		return fmt.Errorf("%w: storage belongs to a different install", ErrUnattached)
	}
	if p, ok := s.(WriteProber); ok {
		if err := p.ProbeWrite(ctx); err != nil {
			switch {
			case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
				return fmt.Errorf("%w: %w", ErrFull, err)
			case errors.Is(err, syscall.EROFS), errors.Is(err, fs.ErrPermission):
				return fmt.Errorf("%w: %w", ErrReadOnly, err)
			default:
				return fmt.Errorf("%w: write probe failed: %w", ErrUnreachable, err)
			}
		}
	}
	return nil
}

// ReadMarker returns the identity stored at MarkerPath. A missing marker is
// fs.ErrNotExist; a marker that does not hold an id wraps ErrUnattached.
func ReadMarker(ctx context.Context, s Storage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := s.Open(ctx, MarkerPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	const maxMarkerSize = 4 * storageIDBytes
	// Read one byte beyond the limit so a valid prefix cannot hide trailing
	// content that makes the marker malformed.
	raw, err := io.ReadAll(io.LimitReader(f, maxMarkerSize+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", MarkerPath, err)
	}
	id := strings.TrimSpace(string(raw))
	if len(raw) > maxMarkerSize || !validStorageID(id) {
		return "", fmt.Errorf("%w: identity marker %s is malformed", ErrUnattached, MarkerPath)
	}
	return id, nil
}

// WriteMarker stores id at MarkerPath, claiming the storage for the database
// that recorded it. It creates a missing local root; that is the first-boot
// path, and the only one that ever creates the root.
func WriteMarker(ctx context.Context, s Storage, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validStorageID(id) {
		return fmt.Errorf("write %s: invalid storage id", MarkerPath)
	}
	if err := s.Save(ctx, MarkerPath, strings.NewReader(id+"\n")); err != nil {
		return fmt.Errorf("write %s: %w", MarkerPath, err)
	}
	return nil
}
