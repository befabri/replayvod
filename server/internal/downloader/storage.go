package downloader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

func (s *Service) verifyStorage(ctx context.Context) error {
	if err := s.storage.Verify(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
	}
	return nil
}

// waitForStorage keeps an admitted attempt alive during an outage. The current
// checkpoint and scratch files remain owned by that attempt; no archive retry
// is consumed. Polling also handles attachment before run unwinds, when a
// one-shot Resume notification would otherwise find the job still active.
func (s *Service) waitForStorage(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verifyStorage(ctx); err == nil {
		return nil
	} else {
		s.log.Warn("recording waiting for storage to be attached and writable", "error", err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.verifyStorage(ctx); err == nil {
				return nil
			}
		}
	}
}

// writeToStorage gates each recording write and repeats it if storage was lost
// during the operation. The callback must reopen its input on each call. A
// successful Save on a volume that became untrusted cannot finalize a part;
// after attachment we copy from scratch again onto the trusted volume.
func (s *Service) writeToStorage(ctx context.Context, write func() error) error {
	for {
		if err := s.waitForStorage(ctx); err != nil {
			return err
		}
		err := write()
		if s.verifyStorage(ctx) != nil || errors.Is(err, storage.ErrUnattached) || errors.Is(err, storage.ErrUnreachable) || errors.Is(err, storage.ErrReadOnly) || errors.Is(err, storage.ErrFull) {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return err
	}
}
