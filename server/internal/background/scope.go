// Package background joins workers before releasing ownership or settling results.
package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Scope joins concurrent children and collects their errors, including panics.
// Construct it with NewScope and call Wait or Join before releasing child resources.
type Scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	err    error
}

func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	return &Scope{ctx: ctx, cancel: cancel}
}

func (s *Scope) Context() context.Context { return s.ctx }
func (s *Scope) Cancel(cause error)       { s.cancel(cause) }

// Go starts a child whose panic becomes an error returned by Wait or Join.
// A fatal error cancels siblings; calls after Wait or Join return context.Canceled.
func (s *Scope) Go(name string, fatal bool, run func(context.Context) error) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return context.Canceled
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		err := Call(s.ctx, run)
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		err = fmt.Errorf("%s: %w", name, err)
		s.mu.Lock()
		s.err = errors.Join(s.err, err)
		s.mu.Unlock()
		if fatal {
			s.cancel(err)
		}
	}()
	return nil
}

// Call runs run synchronously and converts a panic into an error.
func Call(ctx context.Context, run func(context.Context) error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("worker panic: %v", value)
		}
	}()
	return run(ctx)
}

// Join closes admission, cancels children, and waits for them to exit.
// Callers must not hold a recording publication lock while joining.
func (s *Scope) Join() error {
	s.mu.Lock()
	s.closed = true
	s.cancel(context.Canceled)
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Wait closes admission, joins children, then cancels the scope context.
func (s *Scope) Wait() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
	s.cancel(context.Canceled)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
