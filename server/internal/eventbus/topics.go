package eventbus

import "time"

// Buses collects every topic the app publishes. One struct passed
// to services and subscription handlers so we have a single source
// of truth for "what live feeds exist."
type Buses struct {
	EventLogs    *Topic[EventLogEvent]
	StreamLive   *Topic[StreamLiveEvent]
	StreamStatus *Topic[StreamStatusEvent]
	TaskStatus   *Topic[TaskStatusEvent]
}

// New constructs a bus set with sensible buffer sizes.
func New() *Buses {
	return &Buses{
		EventLogs:    NewTopic[EventLogEvent](32),
		StreamLive:   NewTopic[StreamLiveEvent](16),
		StreamStatus: NewTopic[StreamStatusEvent](32),
		TaskStatus:   NewTopic[TaskStatusEvent](32),
	}
}

// EventLogEvent mirrors a row appended to event_logs. Published from
// the same code path that writes the row so SSE subscribers see each
// event within the same goroutine the DB insert runs on.
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

// StreamLiveEvent fires when stream.online dispatches a matching
// schedule. Pushed from the schedule processor on every successful
// auto-download trigger — consumed by the dashboard's "Just went
// live" card. Scoped to match-firings specifically because the card
// cares about "we started recording this" events, not the general
// online/offline signal (that's StreamStatusEvent).
type StreamLiveEvent struct {
	BroadcasterID    string    `json:"broadcaster_id"`
	BroadcasterLogin string    `json:"broadcaster_login"`
	DisplayName      string    `json:"display_name"`
	StreamID         string    `json:"stream_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	MatchedSchedules int       `json:"matched_schedules"`
	JobID            string    `json:"job_id,omitempty"`
}

// StreamStatusKind enumerates the two transitions StreamStatusEvent
// carries. Exported as typed constants so consumers (SSE subscribers)
// can branch on the value without magic strings.
type StreamStatusKind string

const (
	StreamStatusOnline  StreamStatusKind = "online"
	StreamStatusOffline StreamStatusKind = "offline"
)

// StreamStatusEvent fires on every stream.online and stream.offline
// EventSub webhook, unconditional of schedule matches. This is the
// delta feed for the dashboard's live-indicator Set — subscribers
// compose it with an initial stream.liveIds snapshot to maintain an
// accurate "currently live" membership Set without polling.
//
// Distinct from StreamLiveEvent: that one is the schedule-match /
// recording-started firing; this one is the pure status transition.
// Both can fire for the same stream.online webhook.
type StreamStatusEvent struct {
	Kind             StreamStatusKind `json:"kind"`
	BroadcasterID    string           `json:"broadcaster_id"`
	BroadcasterLogin string           `json:"broadcaster_login"`
	DisplayName      string           `json:"display_name,omitempty"`
	StreamID         string           `json:"stream_id,omitempty"`
	At               time.Time        `json:"at"`
}

// TaskStatusEvent fires on every scheduler task transition (start,
// success, failure). Subscribers see the same lifecycle the DB row
// reflects, but in real time.
type TaskStatusEvent struct {
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	DurationMs     int64     `json:"duration_ms"`
	Error          string    `json:"error,omitempty"`
	TransitionedAt time.Time `json:"transitioned_at"`
}
