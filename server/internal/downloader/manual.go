package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
	provider "github.com/befabri/replayvod/server/internal/twitch"
	"github.com/google/uuid"
)

type manualEvent struct {
	jobID     string
	stoppedAt *time.Time
	done      bool
}
type onlineObservation struct {
	stream provider.Stream
	at     time.Time
}
type manualRun struct {
	id            string
	broadcasterID string
	params        Params
	reservation   *background.Reservation
	events        chan manualEvent
	online        chan struct{}
	onlineMu      sync.Mutex
	observations  map[string]onlineObservation
	offline       chan struct{}
	closed        chan struct{}
}

func (m *manualRun) send(ctx context.Context, event manualEvent) {
	select {
	case m.events <- event:
	case <-m.closed:
	case <-ctx.Done():
	}
}
func (s *Service) registerManual(id string, p Params, r *background.Reservation) *manualRun {
	m := &manualRun{id: id, broadcasterID: p.BroadcasterID, params: p, reservation: r, events: make(chan manualEvent, 16), online: make(chan struct{}, 1), offline: make(chan struct{}, 1), closed: make(chan struct{})}
	s.mu.Lock()
	s.manual[id] = m
	s.mu.Unlock()
	return m
}

// ObserveStreamOnline is a wake-up hint; the durable intent and its deadline
// decide whether this broadcast may use the already reserved live slot.
func (s *Service) ObserveStreamOnline(stream provider.Stream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.manual {
		if m.broadcasterID == stream.UserID {
			m.observeOnline(onlineObservation{stream: stream, at: s.now()})
		}
	}
}

// observeOnline retains the earliest observation of each broadcast while persistence is delayed.
func (m *manualRun) observeOnline(observed onlineObservation) {
	if observed.stream.ID == "" {
		return
	}
	m.onlineMu.Lock()
	if m.observations == nil {
		m.observations = make(map[string]onlineObservation)
	}
	if previous, ok := m.observations[observed.stream.ID]; ok {
		if previous.at.Before(observed.at) {
			observed.at = previous.at
		}
		if observed.stream.StartedAt.IsZero() {
			observed.stream.StartedAt = previous.stream.StartedAt
		}
	}
	m.observations[observed.stream.ID] = observed
	m.onlineMu.Unlock()
	select {
	case m.online <- struct{}{}:
	default:
	}
}

// nextReturn preserves other broadcasts and their original timestamps for later restart windows.
func (m *manualRun) nextReturn(intent *repository.RecordingIntent) (onlineObservation, bool) {
	m.onlineMu.Lock()
	defer m.onlineMu.Unlock()
	var next onlineObservation
	for id, observed := range m.observations {
		if id == intent.LastStreamID || (intent.LastStreamID == "" && !observed.stream.StartedAt.After(intent.CreatedAt)) {
			delete(m.observations, id)
			continue
		}
		if intent.WaitUntil == nil || observed.at.After(*intent.WaitUntil) {
			continue
		}
		if next.stream.ID == "" || observed.at.Before(next.at) || (observed.at.Equal(next.at) && id < next.stream.ID) {
			next = observed
		}
	}
	delete(m.observations, next.stream.ID)
	return next, next.stream.ID != ""
}

func (s *Service) runManual(m *manualRun, initial *download, p Params, filename string) {
	_ = m.reservation.Run(func(ctx context.Context) error {
		// The intent owns the accepted progress stream until a child takes it,
		// including errors or panics before the initial intent can be loaded.
		pendingInitial := initial
		defer func() {
			if pendingInitial != nil {
				close(pendingInitial.progressCh)
			}
		}()
		session := &manualSession{
			service: s, run: m, children: background.NewScope(ctx),
			finished: make(map[string]bool), launched: make(map[string]bool),
		}
		defer session.close(ctx)
		intent, err := s.repo.GetRecordingIntent(ctx, m.id)
		if err != nil {
			return err
		}
		session.stopped = intent.StopRequested || intent.Status == "stopped"
		if initial != nil && !session.stopped {
			if err := session.launch(initial, p, filename); err != nil {
				return err
			}
			pendingInitial = nil // The child closes progress after settlement.
		} else {
			if err := session.recoverJobs(ctx); err != nil {
				s.log.Warn("manual child recovery deferred", "intent_id", m.id, "error", err)
			}
		}
		return session.watch(ctx)
	}, func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) {
			s.log.Warn("manual recording intent deferred", "intent_id", m.id, "error", err)
		}
	})
	s.pumpAfterJobEnd()
}

func (s *Service) showRestartWait(m *manualRun, intent *repository.RecordingIntent) {
	s.mu.Lock()
	d := s.active[intent.CurrentJobID]
	s.mu.Unlock()
	if d == nil {
		v, err := s.repo.GetVideoByJobID(m.reservation.Context(), intent.CurrentJobID)
		if err != nil {
			return
		}
		d = &download{jobID: v.JobID, videoID: v.ID, broadcasterID: v.BroadcasterID, manual: m, startedAt: v.StartDownloadAt}
		s.mu.Lock()
		s.active[d.jobID] = d
		s.mu.Unlock()
	}
	// An earlier recording can still be remuxing. Its own progress remains
	// visible; completed capture is represented by the separate intent query.
	if snap := d.progressSnapshot(); snap.Stage == "done" || snap.Stage == "" || snap.Stage == "waiting" {
		snap.JobID = d.jobID
		snap.Stage = "waiting"
		d.setProgress(snap)
		s.notifyActiveChanged()
	}
}

func (s *Service) admitSuccessor(ctx context.Context, m *manualRun, intent *repository.RecordingIntent, observation onlineObservation) (*download, Params, string, error) {
	p := m.params
	// Hints can outlive their broadcast while the intent owner is delayed.
	// Even a complete metadata hint must still name the currently live stream.
	current, err := s.currentBroadcast(ctx, p.BroadcasterID, observation.stream.ID)
	if err != nil {
		return nil, p, "", err
	}
	stream := *current
	p.StreamID = &stream.ID
	p.StreamStartedAt = stream.StartedAt
	p.Title = stream.Title
	p.Language = stream.Language
	p.ViewerCount = int64(stream.ViewerCount)
	p.CategoryID = stream.GameID
	p.CategoryName = stream.GameName
	jobID := uuid.NewString()
	name := buildFilename(p.BroadcasterLogin, jobID)
	state := NewResumeState()
	state.MaxHeight = p.MaxHeight
	checkpoint, err := state.MarshalJSON()
	if err != nil {
		return nil, p, "", err
	}
	input := &repository.VideoInput{JobID: jobID, Filename: name, DisplayName: p.DisplayName, Title: p.Title, Status: repository.VideoStatusPending, Quality: p.Quality, BroadcasterID: p.BroadcasterID, StreamID: p.StreamID, StreamStartedAt: p.StreamStartedAt, ViewerCount: p.ViewerCount, Language: p.Language, RecordingType: p.RecordingType, ForceH264: p.ForceH264, IntentID: m.id, IntentPreviousJobID: intent.CurrentJobID, IntentObservedAt: observation.at}
	var v *repository.Video
	write := func(c context.Context) error {
		var e error
		v, e = repository.CreateAttempt(c, s.repo, input, checkpoint)
		return e
	}
	err = s.persistVideoChange(ctx, "successor admission", write)
	if err != nil {
		return nil, p, "", err
	}
	s.linkInitialMetadata(ctx, v.ID, p)
	d := &download{jobID: jobID, videoID: v.ID, executionID: uuid.NewString(), broadcasterID: p.BroadcasterID, attempt: 1, progressCh: make(chan Progress, 16), startedAt: s.now(), resume: state, manual: m}
	return d, p, name, nil
}

func (s *Service) resumeManualIntents(ctx context.Context) error {
	for after := ""; ; {
		intents, err := s.repo.ListRecoverableRecordingIntents(ctx, after, recoveryPageSize)
		if err != nil {
			return err
		}
		for _, intent := range intents {
			after = intent.ID
			s.mu.Lock()
			owned := s.manual[intent.ID] != nil
			s.mu.Unlock()
			if owned {
				continue
			}
			var p Params
			if err := json.Unmarshal(intent.Params, &p); err != nil {
				s.log.Warn("invalid manual recording intent", "intent_id", intent.ID, "error", err)
				continue
			}
			kind := "live"
			if intent.StopRequested || intent.Status == "stopped" {
				kind = "settlement"
			}
			reservation, err := s.work.Reserve(kind, intent.ID)
			if errors.Is(err, background.ErrCapacity) || errors.Is(err, background.ErrBusy) {
				continue
			}
			if err != nil {
				return err
			}
			m := s.registerManual(intent.ID, p, reservation)
			go s.runManual(m, nil, p, "")
		}
		if len(intents) < recoveryPageSize {
			return nil
		}
	}
}

// ObserveStreamOffline wakes reconciliation; HLS must still drain the final playlist segments.
func (s *Service) ObserveStreamOffline(broadcasterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.manual {
		if m.broadcasterID == broadcasterID {
			select {
			case m.offline <- struct{}{}:
			default:
			}
		}
	}
}
