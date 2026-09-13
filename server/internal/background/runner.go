package background

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrStopped  = errors.New("background runner stopped")
	ErrBusy     = errors.New("background work already owned")
	ErrCapacity = errors.New("background capacity exhausted")
)

// Runner keeps keys and per-kind capacity reserved through settlement.
// Runner is safe for concurrent use; construct it with New.
type Runner struct {
	mu      sync.Mutex
	limits  map[string]int
	used    map[string]int
	work    map[string]*Reservation
	stopped bool
	done    chan struct{}
	idle    chan struct{}
	slots   map[string]chan struct{}
}

// Reservation owns a key and capacity until Run settles or Release abandons it.
type Reservation struct {
	runner    *Runner
	kind, key string
	ctx       context.Context
	cancel    context.CancelCauseFunc
	started   bool
	released  bool
	permit    chan struct{}
}

// New creates a runner; omitted kinds and nonpositive limits have no capacity limit.
func New(limits map[string]int) *Runner {
	copyLimits := make(map[string]int, len(limits))
	for kind, limit := range limits {
		copyLimits[kind] = limit
	}
	idle := make(chan struct{})
	close(idle)
	slots := make(map[string]chan struct{})
	for kind, limit := range limits {
		if limit > 0 {
			slots[kind] = make(chan struct{}, limit)
		}
	}
	return &Runner{limits: copyLimits, used: map[string]int{}, work: map[string]*Reservation{}, done: make(chan struct{}), idle: idle, slots: slots}
}

// Reserve claims key across all kinds, returning ErrBusy if it is already owned.
// Call Run or Release for every successful reservation, including after Stop.
func (r *Runner) Reserve(kind, key string) (*Reservation, error) {
	return r.reserve(kind, key, true)
}

func (r *Runner) reserve(kind, key string, enforceLimit bool) (*Reservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, ErrStopped
	}
	if _, exists := r.work[key]; exists {
		return nil, ErrBusy
	}
	if limit := r.limits[kind]; enforceLimit && limit > 0 && r.used[kind] >= limit {
		return nil, ErrCapacity
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	work := &Reservation{runner: r, kind: kind, key: key, ctx: ctx, cancel: cancel}
	if slots := r.slots[kind]; enforceLimit && slots != nil {
		slots <- struct{}{} // used < limit while holding mu guarantees space.
		work.permit = slots
	}
	if len(r.work) == 0 {
		r.idle = make(chan struct{})
	}
	r.work[key] = work
	r.used[kind]++
	return work, nil
}

func (w *Reservation) Context() context.Context { return w.ctx }

// Run blocks through settlement and may be called once, even after cancellation.
func (w *Reservation) Run(run func(context.Context) error, settle func(error)) error {
	r := w.runner
	r.mu.Lock()
	if w.started || w.released {
		r.mu.Unlock()
		return ErrBusy
	}
	w.started = true
	r.mu.Unlock()
	defer w.finish()
	err := Call(w.ctx, run)
	if settle != nil {
		settleErr := Call(context.WithoutCancel(w.ctx), func(context.Context) error { settle(err); return nil })
		err = errors.Join(err, settleErr)
	}
	return err
}

// Release abandons an unstarted reservation; Run retains started work until settlement ends.
func (w *Reservation) Release() {
	r := w.runner
	r.mu.Lock()
	defer r.mu.Unlock()
	if !w.started {
		w.releaseLocked()
	}
}

func (w *Reservation) finish() {
	w.runner.mu.Lock()
	defer w.runner.mu.Unlock()
	w.releaseLocked()
}

func (w *Reservation) releaseLocked() {
	r := w.runner
	if w.released {
		return
	}
	w.released = true
	w.cancel(context.Canceled)
	if w.permit != nil {
		<-w.permit
	}
	delete(r.work, w.key)
	r.used[w.kind]--
	if len(r.work) == 0 {
		close(r.idle)
	}
	if r.stopped && len(r.work) == 0 {
		close(r.done)
	}
}

func (r *Runner) Start(kind, key string, run func(context.Context) error, settle func(error)) error {
	w, err := r.Reserve(kind, key)
	if err != nil {
		return err
	}
	go w.Run(run, settle)
	return nil
}

// StartQueued reserves key immediately and waits for per-kind capacity before running.
// Queued and running work both retain ownership through settlement.
func (r *Runner) StartQueued(kind, key string, run func(context.Context) error, settle func(error)) error {
	w, err := r.reserve(kind, key, false)
	if err != nil {
		return err
	}
	go w.Run(func(ctx context.Context) error {
		if slots := r.slots[kind]; slots != nil {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			r.mu.Lock()
			w.permit = slots
			r.mu.Unlock()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return run(ctx)
	}, settle)
	return nil
}

// WaitIdle waits for the current batch to settle without closing admission.
func (r *Runner) WaitIdle(ctx context.Context) error {
	r.mu.Lock()
	idle := r.idle
	r.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) Cancel(key string, cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w := r.work[key]; w != nil {
		w.cancel(cause)
	}
}

// Stop closes admission and cancels work; owners must still Run or Release reservations.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	for _, w := range r.work {
		w.cancel(context.Canceled)
	}
	if len(r.work) == 0 {
		close(r.done)
	}
}

// Wait waits for Stop and for all reservations to be released.
func (r *Runner) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Used counts reservations, including queued work and work still settling.
func (r *Runner) Used(kind string) int { r.mu.Lock(); defer r.mu.Unlock(); return r.used[kind] }
