package downloader

import (
	"context"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
)

// captureObservers keeps snapshot and title workers alive across part splits.
// Capture stops them before metadata closure; close also releases subscriptions.
type captureObservers struct {
	attempt           *recordingAttempt
	children          *background.Scope
	titleStops        []context.CancelFunc
	stopTitleTracking func()
	started           bool
}

func newCaptureObservers(ctx context.Context, attempt *recordingAttempt) *captureObservers {
	return &captureObservers{
		attempt:           attempt,
		children:          background.NewScope(ctx),
		stopTitleTracking: func() {},
	}
}

func (o *captureObservers) start(ctx context.Context) error {
	a := o.attempt
	s, d, p := a.service, a.download, a.params
	if o.started || d.vod || d.resume.CaptureStoppedAt != nil || d.resume.EndListSeen {
		return nil
	}
	// Prepared recovery performs no live-channel effects. Reopen metadata only
	// after the original broadcast is verified for renewed capture.
	if d.recovered {
		if err := s.persist(ctx, "resume capture metadata", func(c context.Context) error {
			return repository.WithAttempt(c, s.repo, d.claim(), func(tx repository.Repository) error {
				if err := tx.SetJobExecution(c, d.jobID, d.executionID, true); err != nil {
					return err
				}
				return tx.ResumeVideoMetadataSpans(c, d.videoID, s.now())
			})
		}); err != nil {
			return err
		}
	}
	o.started = true
	snapWriter := &storageSnapshotWriter{storage: s.storage, claim: d.claim(), filename: a.filename}
	if err := o.children.Go("snapshots", false, func(childCtx context.Context) error {
		count := s.snapshots.Run(childCtx, p.BroadcasterLogin, snapWriter)
		a.log.Debug("snapshot ticker done", "captures", count)
		return nil
	}); err != nil {
		return err
	}
	o.stopTitleTracking = s.startTitleTracking(ctx, p, d.claim(), a.log, func(cancel context.CancelFunc) {
		o.titleStops = append(o.titleStops, cancel)
	}, d)
	return nil
}

func (o *captureObservers) stop() {
	for _, stop := range o.titleStops {
		stop()
	}
	o.titleStops = nil
	if err := o.children.Join(); err != nil {
		o.attempt.log.Warn("recording auxiliary worker failed", "error", err)
	}
}

func (o *captureObservers) close() {
	defer o.stop()
	o.stopTitleTracking()
}

func (a *recordingAttempt) finishLiveCapture(ctx context.Context, observers *captureObservers) error {
	s, d := a.service, a.download
	observers.stop()
	if d.vod {
		return nil
	}
	if d.resume.CaptureStoppedAt == nil {
		at := s.now()
		d.resume.CaptureStoppedAt = &at
		s.checkpointResume(context.WithoutCancel(ctx), d, a.log)
	}
	if d.persistenceErr != nil {
		return d.persistenceErr
	}
	if err := s.persist(ctx, "capture metadata closure", func(c context.Context) error {
		return repository.StopAttemptMetadata(c, s.repo, d.claim(), *d.resume.CaptureStoppedAt)
	}); err != nil {
		d.persistenceErr = err
		return err
	}
	if d.manual != nil {
		d.manual.send(ctx, manualEvent{jobID: d.jobID, stoppedAt: d.resume.CaptureStoppedAt})
	}
	return nil
}
