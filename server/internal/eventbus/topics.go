package eventbus

import "time"

// Buses collects every topic the app publishes. One struct passed
// to services and subscription handlers so we have a single source
// of truth for "what live feeds exist."
type Buses struct {
	EventLogs  *Topic[EventLogEvent]
	StreamLive *Topic[StreamLiveEvent]
	TaskStatus *Topic[TaskStatusEvent]
}

// New constructs a bus set with sensible buffer sizes.
func New() *Buses {
	return &Buses{
		EventLogs:  NewTopic[EventLogEvent](32),
		StreamLive: NewTopic[StreamLiveEvent](16),
		TaskStatus: NewTopic[TaskStatusEvent](32),
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
// schedule (including when no schedule matches — operators still
// want to see their channels go live). Pushed from the schedule
// processor.
type StreamLiveEvent struct {
	BroadcasterID    string    `json:"broadcaster_id"`
	BroadcasterLogin string    `json:"broadcaster_login"`
	DisplayName      string    `json:"display_name"`
	StreamID         string    `json:"stream_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	MatchedSchedules int       `json:"matched_schedules"`
	JobID            string    `json:"job_id,omitempty"`
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
