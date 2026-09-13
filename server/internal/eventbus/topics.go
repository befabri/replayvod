package eventbus

import "time"

// Buses holds the process topics shared by services and subscribers.
type Buses struct {
	EventLogs         *Topic[EventLogEvent]
	StreamLive        *Topic[StreamLiveEvent]
	StreamStatus      *Topic[StreamStatusEvent]
	TaskStatus        *Topic[TaskStatusEvent]
	RecordingTerminal *Topic[RecordingTerminalEvent]
	StorageStatus     *Topic[StorageStatusEvent]
	ArchiveQueue      *Topic[ArchiveQueueEvent]
	VideoChanges      *Topic[VideoChangeEvent]
}

// New initializes every process topic with a bounded subscriber buffer.
func New() *Buses {
	return &Buses{
		EventLogs:         NewTopic[EventLogEvent](32),
		StreamLive:        NewTopic[StreamLiveEvent](16),
		StreamStatus:      NewTopic[StreamStatusEvent](32),
		TaskStatus:        NewTopic[TaskStatusEvent](32),
		RecordingTerminal: NewTopic[RecordingTerminalEvent](16),
		StorageStatus:     NewTopic[StorageStatusEvent](8),
		ArchiveQueue:      NewTopic[ArchiveQueueEvent](32),
		VideoChanges:      NewTopic[VideoChangeEvent](1),
	}
}

// VideoChangeEvent invalidates video snapshots after a committed recording,
// intent, removal, or restore transition. It carries no row delta: one buffered
// notification covers every change before it is read. Consumers reread the DB.
// Reconnecting clients reread snapshots to cover changes while disconnected.
type VideoChangeEvent struct{}

// NotifyVideoChange invalidates video snapshots after commit; a nil bus is safe.
func (b *Buses) NotifyVideoChange() {
	if b != nil && b.VideoChanges != nil {
		b.VideoChanges.Publish(VideoChangeEvent{})
	}
}

// StorageStatusEvent fires on every storage readiness transition (attached,
// read-only, full, unattached, unreachable). This is a viewer-safe notification;
// diagnostics belong exclusively to owner-only status details and event logs.
type StorageStatusEvent struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

// ArchiveQueueKind enumerates the archive queue membership changes.
type ArchiveQueueKind string

const (
	ArchiveQueued         ArchiveQueueKind = "queued"
	ArchiveDequeued       ArchiveQueueKind = "dequeued"
	ArchiveStarted        ArchiveQueueKind = "started"
	ArchiveCompleted      ArchiveQueueKind = "completed"
	ArchiveFailed         ArchiveQueueKind = "failed"
	ArchiveRetryCancelled ArchiveQueueKind = "retry_cancelled"
)

// ArchiveQueueEvent invalidates the queue snapshot after a membership or state
// change; clients reread the row for retry details.
type ArchiveQueueEvent struct {
	Kind    ArchiveQueueKind `json:"kind"`
	VideoID int64            `json:"video_id"`
	At      time.Time        `json:"at"`
}

// EventLogEvent carries an event log row after its insertion succeeds.
type EventLogEvent struct {
	ID          int64          `json:"id"`
	Domain      string         `json:"domain"`
	EventType   string         `json:"event_type"`
	Severity    string         `json:"severity"`
	Message     string         `json:"message"`
	ActorUserID *string        `json:"actor_user_id,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// StreamLiveEvent reports a successful recording trigger from matching schedules.
type StreamLiveEvent struct {
	BroadcasterID    string    `json:"broadcaster_id"`
	BroadcasterLogin string    `json:"broadcaster_login"`
	DisplayName      string    `json:"display_name"`
	StreamID         string    `json:"stream_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	MatchedSchedules int       `json:"matched_schedules"`
	JobID            string    `json:"job_id,omitempty"`
}

// StreamStatusKind identifies an online or offline transition.
type StreamStatusKind string

const (
	StreamStatusOnline  StreamStatusKind = "online"
	StreamStatusOffline StreamStatusKind = "offline"
)

// StreamStatusEvent reports online and offline webhooks regardless of schedule
// matches; subscribers combine it with a stream.liveIds snapshot.
type StreamStatusEvent struct {
	Kind             StreamStatusKind `json:"kind"`
	BroadcasterID    string           `json:"broadcaster_id"`
	BroadcasterLogin string           `json:"broadcaster_login"`
	DisplayName      string           `json:"display_name,omitempty"`
	StreamID         string           `json:"stream_id,omitempty"`
	At               time.Time        `json:"at"`
}

// RecordingTerminalKind enumerates the two terminal recording outcomes that can
// wake the durable outbound-webhook dispatcher.
type RecordingTerminalKind string

const (
	// RecordingCompleted wakes delivery of recording.completed after a DONE commit.
	RecordingCompleted RecordingTerminalKind = "completed"
	// RecordingFailed wakes delivery of recording.failed after failure or operator
	// cancellation; shutdown leaves the recording resumable and emits neither kind.
	RecordingFailed RecordingTerminalKind = "failed"
)

// RecordingTerminalEvent fires when a recording reaches a terminal state. The
// durable webhook row has already been committed by the downloader; this event
// only wakes the dispatcher so it can poll immediately. Best-effort like every
// other topic: a dropped wake-up only waits for the next poll interval.
type RecordingTerminalEvent struct {
	VideoID int64                 `json:"video_id"`
	Kind    RecordingTerminalKind `json:"kind"`
}

// TaskStatusEvent reports a committed scheduler transition.
type TaskStatusEvent struct {
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	DurationMs     int64     `json:"duration_ms"`
	Error          string    `json:"error,omitempty"`
	TransitionedAt time.Time `json:"transitioned_at"`
}
