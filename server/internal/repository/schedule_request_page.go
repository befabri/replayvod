package repository

import (
	"math"
	"time"
)

// ScheduleRequestCursor identifies the last item in a newest-first page.
// Both timestamp and ID are needed when multiple requests share a timestamp.
type ScheduleRequestCursor struct {
	CreatedAt time.Time
	ID        int64
}

// ScheduleRequestQueryBounds returns a limit with room for one lookahead row
// and an exclusive cursor; nil starts above the maximum RFC3339 timestamp.
func ScheduleRequestQueryBounds(limit int, cursor *ScheduleRequestCursor) (int, ScheduleRequestCursor) {
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 201)
	if cursor != nil {
		return limit, *cursor
	}
	return limit, ScheduleRequestCursor{CreatedAt: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), ID: math.MaxInt64}
}
