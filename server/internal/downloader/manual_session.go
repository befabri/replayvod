package downloader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
)

// manualSession owns the children and transitions of one manual intent. Only
// its event loop changes these fields; online observations use manualRun's inbox.
type manualSession struct {
	service  *Service
	run      *manualRun
	children *background.Scope
	finished map[string]bool
	launched map[string]bool
	stopped  bool
	drain    bool
}

type manualStep int

const (
	manualWait manualStep = iota
	manualReload
	manualComplete
)

func (r *manualSession) close(ctx context.Context) {
	s, m := r.service, r.run
	close(m.closed)
	var err error
	if r.drain {
		err = r.children.Wait()
	} else {
		if ctx.Err() == nil {
			r.children.Cancel(errExecutionDeferred)
		}
		err = r.children.Join()
	}
	if err != nil {
		s.log.Warn("manual recording child failed", "intent_id", m.id, "error", err)
	}
	s.mu.Lock()
	delete(s.manual, m.id)
	for id, d := range s.active {
		if d.manual == m {
			delete(s.active, id)
		}
	}
	s.mu.Unlock()
	s.notifyActiveChanged()
}

func (r *manualSession) launch(d *download, params Params, filename string) error {
	s, m := r.service, r.run
	if r.launched[d.jobID] {
		return nil
	}
	r.launched[d.jobID] = true
	delete(r.finished, d.jobID)
	s.mu.Lock()
	d.manual = m
	d.runCtx = r.children.Context()
	d.userCancelled = d.userCancelled || r.stopped
	d.cancel = func() { s.work.Cancel(m.id, context.Canceled) }
	s.active[d.jobID] = d
	s.mu.Unlock()
	s.notifyActiveChanged()
	return r.children.Go("recording "+d.jobID, false, func(childCtx context.Context) error {
		// Completion permits recovery to relaunch this job, so release its
		// scratch and progress stream before the event loop can observe it.
		defer func() { m.send(childCtx, manualEvent{jobID: d.jobID, done: true}) }()
		defer close(d.progressCh)
		defer s.releaseAttemptScratch(d)
		err := background.Call(childCtx, func(ctx context.Context) error { s.runAttempt(ctx, d, params, filename); return nil })
		if err != nil {
			s.failDownload(context.WithoutCancel(childCtx), d, s.log.With("job_id", d.jobID), err)
		}
		return err
	})
}

func (r *manualSession) recoverJobs(ctx context.Context) error {
	s, m := r.service, r.run
	var failures []error
	for after := ""; ; {
		jobs, err := s.repo.ListRecordingIntentJobs(ctx, m.id, after, recoveryPageSize)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, job := range jobs {
			after = job.ID
			if r.launched[job.ID] {
				continue
			}
			if r.stopped {
				if err := s.failUnrecoverableAttempt(ctx, &job, ErrCancelled, true); err != nil && len(failures) < 16 {
					failures = append(failures, fmt.Errorf("settle stopped manual child %s: %w", job.ID, err))
				}
				continue
			}
			d, params, name, err := s.reconstructAttempt(ctx, &job)
			if errors.Is(err, errInvalidResume) {
				err = s.failUnrecoverableAttempt(ctx, &job, err, r.stopped)
			} else if errors.Is(err, errObsoleteJob) {
				continue
			} else if err == nil {
				err = r.launch(d, params, name)
			}
			if err != nil && len(failures) < 16 {
				failures = append(failures, fmt.Errorf("recover manual child %s: %w", job.ID, err))
			}
		}
		if len(jobs) < recoveryPageSize || ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
	}
}

func (r *manualSession) watch(ctx context.Context) error {
	recoveryInterval := r.service.retryInterval
	if recoveryInterval <= 0 {
		recoveryInterval = archiveRetryPumpInterval
	}
	recoveryTicker := time.NewTicker(recoveryInterval)
	defer recoveryTicker.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var intent *repository.RecordingIntent
		if err := r.service.persist(ctx, "read manual intent", func(c context.Context) error {
			var err error
			intent, err = r.service.repo.GetRecordingIntent(c, r.run.id)
			return err
		}); err != nil {
			return err
		}
		step, err := r.reconcileIntent(ctx, intent)
		if err != nil || step == manualComplete {
			return err
		}
		if step == manualReload {
			continue
		}
		step, err = r.advanceRestartWindow(ctx, intent)
		if err != nil || step == manualComplete {
			return err
		}
		if step == manualReload {
			continue
		}
		if err := r.waitForEvent(ctx, intent, recoveryTicker.C, ticker.C); err != nil {
			return err
		}
	}
}

// reconcileIntent restores capture state before considering another broadcast.
// Recovery must retain the original waiting deadline.
func (r *manualSession) reconcileIntent(ctx context.Context, intent *repository.RecordingIntent) (manualStep, error) {
	s, m := r.service, r.run
	if ctx.Err() != nil {
		if errors.Is(context.Cause(ctx), ErrCancelled) || intent.StopRequested {
			return manualComplete, r.closeIntent(ctx, "stop manual intent", "stopped")
		}
		return manualComplete, ctx.Err() // shutdown preserves the deadline and attempts
	}
	if intent.StopRequested {
		s.mu.Lock()
		for _, d := range s.active {
			if d.manual == m {
				d.userCancelled = true
			}
		}
		s.mu.Unlock()
		s.work.Cancel(m.id, ErrCancelled)
		return manualComplete, r.closeIntent(ctx, "stop manual intent", "stopped")
	}
	if intent.Status == "stopped" || intent.Status == "expired" {
		r.drain = true
		return manualComplete, nil
	}
	if intent.Status != "active" {
		return manualWait, nil
	}
	job, err := s.repo.GetJob(ctx, intent.CurrentJobID)
	if err != nil {
		return manualComplete, err
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil && job.Status != repository.JobStatusDone && job.Status != repository.JobStatusFailed {
		// Bounded child recovery settles this member while healthy siblings run.
		s.log.Warn("manual checkpoint recovery deferred", "job_id", job.ID, "error", err)
	}
	if err == nil && state.CaptureStoppedAt != nil {
		return manualReload, r.setRestartDeadline(ctx, intent, job.ID, *state.CaptureStoppedAt)
	}
	if job.Status == repository.JobStatusDone || job.Status == repository.JobStatusFailed {
		r.drain = true
		return manualComplete, r.closeIntent(ctx, "close failed manual intent", "stopped")
	}
	return manualWait, nil
}

func (r *manualSession) advanceRestartWindow(ctx context.Context, intent *repository.RecordingIntent) (manualStep, error) {
	s, m := r.service, r.run
	if intent.Status != "waiting" || intent.WaitUntil == nil {
		return manualWait, nil
	}
	// Consider an on-time observation even if persistence delayed this owner
	// past expiry. Admission still verifies the currently live broadcast.
	s.showRestartWait(m, intent)
	if observed, ok := m.nextReturn(intent); ok {
		d, params, name, err := s.admitSuccessor(ctx, m, intent, observed)
		if errors.Is(err, errBroadcastEnded) || errors.Is(err, repository.ErrDuplicate) || errors.Is(err, repository.ErrStaleExecution) {
			return manualReload, nil
		}
		if err != nil {
			// Keep the original observation through provider/database outages.
			m.observeOnline(observed)
			s.log.Warn("successor admission deferred", "intent_id", m.id, "error", err)
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
			return manualReload, nil
		}
		if r.finished[intent.CurrentJobID] {
			s.mu.Lock()
			delete(s.active, intent.CurrentJobID)
			s.mu.Unlock()
		}
		return manualReload, r.launch(d, params, name)
	}
	if !s.now().Before(*intent.WaitUntil) {
		err := r.closeIntent(ctx, "expire manual intent", "expired")
		r.drain = err == nil
		return manualComplete, err
	}
	return manualWait, nil
}

func (r *manualSession) waitForEvent(ctx context.Context, intent *repository.RecordingIntent, recovery, poll <-chan time.Time) error {
	select {
	case <-ctx.Done():
	case <-r.run.offline:
		// HLS owns draining; this hint only wakes capture reconciliation.
	case <-r.run.online:
	case event := <-r.run.events:
		return r.handleEvent(ctx, intent, event)
	case <-recovery:
		if err := r.recoverJobs(ctx); err != nil {
			r.service.log.Warn("manual child recovery deferred", "intent_id", r.run.id, "error", err)
		}
	case <-poll:
		r.pollOnline(ctx, intent)
	}
	return nil
}

func (r *manualSession) handleEvent(ctx context.Context, intent *repository.RecordingIntent, event manualEvent) error {
	if event.stoppedAt != nil && event.jobID == intent.CurrentJobID && intent.Status == "active" {
		if err := r.setRestartDeadline(ctx, intent, event.jobID, *event.stoppedAt); err != nil && !errors.Is(err, repository.ErrStaleExecution) {
			return err
		}
	}
	if event.done {
		r.finished[event.jobID] = true
		delete(r.launched, event.jobID)
		if event.jobID != intent.CurrentJobID {
			r.service.mu.Lock()
			delete(r.service.active, event.jobID)
			r.service.mu.Unlock()
			r.service.notifyActiveChanged()
		}
	}
	return nil
}

func (r *manualSession) pollOnline(ctx context.Context, intent *repository.RecordingIntent) {
	if intent.Status != "waiting" || r.service.observe == nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	stream, err := r.service.observe(probeCtx, r.run.broadcasterID)
	cancel()
	if err == nil && stream != nil {
		r.run.observeOnline(onlineObservation{stream: *stream, at: r.service.now()})
	}
}

func (r *manualSession) setRestartDeadline(ctx context.Context, intent *repository.RecordingIntent, jobID string, stoppedAt time.Time) error {
	until := stoppedAt.Add(time.Duration(intent.WaitSeconds) * time.Second)
	return r.service.persistVideoChange(ctx, "restart deadline", func(c context.Context) error {
		return r.service.repo.SetRecordingIntentWaiting(c, r.run.id, jobID, until)
	})
}

func (r *manualSession) closeIntent(ctx context.Context, operation, status string) error {
	return r.service.persistVideoChange(ctx, operation, func(c context.Context) error {
		return r.service.repo.CloseRecordingIntent(c, r.run.id, status)
	})
}
