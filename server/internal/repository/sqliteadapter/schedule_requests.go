package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func sqliteScheduleRequestToDomain(row sqlitegen.ScheduleRequest) *repository.ScheduleRequest {
	return &repository.ScheduleRequest{
		ID:            row.ID,
		BroadcasterID: row.BroadcasterID,
		RequestedBy:   row.RequestedBy,
		Note:          fromNullString(row.Note),
		Status:        row.Status,
		DecidedBy:     fromNullString(row.DecidedBy),
		DecidedAt:     timePtrFromSQLite(row.DecidedAt),
		ScheduleID:    fromNullInt64(row.ScheduleID),
		CreatedAt:     row.CreatedAt.Time,
	}
}

func (a *SQLiteAdapter) CreateScheduleRequest(ctx context.Context, broadcasterID, requestedBy string, note *string) (*repository.ScheduleRequest, error) {
	row, err := a.queries.CreateScheduleRequest(ctx, sqlitegen.CreateScheduleRequestParams{
		BroadcasterID: broadcasterID,
		RequestedBy:   requestedBy,
		Note:          toNullString(note),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create schedule request: %w", mapErr(err))
	}
	return sqliteScheduleRequestToDomain(row), nil
}

func (a *SQLiteAdapter) ListScheduleRequests(ctx context.Context, limit int, cursor *repository.ScheduleRequestCursor) ([]repository.ScheduleRequestView, error) {
	limit, before := repository.ScheduleRequestQueryBounds(limit, cursor)
	rows, err := a.queries.ListScheduleRequests(ctx, sqlitegen.ListScheduleRequestsParams{BeforeCreatedAt: sqliteTime(before.CreatedAt), BeforeID: before.ID, PageLimit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule requests: %w", err)
	}
	out := make([]repository.ScheduleRequestView, len(rows))
	for i, r := range rows {
		out[i] = sqliteScheduleRequestViewToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListScheduleRequestsForUser(ctx context.Context, userID string, limit int, cursor *repository.ScheduleRequestCursor) ([]repository.ScheduleRequestView, error) {
	limit, before := repository.ScheduleRequestQueryBounds(limit, cursor)
	rows, err := a.queries.ListScheduleRequestsForUser(ctx, sqlitegen.ListScheduleRequestsForUserParams{BeforeCreatedAt: sqliteTime(before.CreatedAt), BeforeID: before.ID, PageLimit: int64(limit), RequestedBy: userID})
	if err != nil {
		return nil, fmt.Errorf("sqlite list schedule requests for user %s: %w", userID, err)
	}
	out := make([]repository.ScheduleRequestView, len(rows))
	for i, r := range rows {
		out[i] = sqliteScheduleRequestViewToDomain(sqlitegen.ListScheduleRequestsRow(r))
	}
	return out, nil
}

func sqliteScheduleRequestViewToDomain(r sqlitegen.ListScheduleRequestsRow) repository.ScheduleRequestView {
	return repository.ScheduleRequestView{
		ScheduleRequest: repository.ScheduleRequest{
			ID:            r.ID,
			BroadcasterID: r.BroadcasterID,
			RequestedBy:   r.RequestedBy,
			Note:          fromNullString(r.Note),
			Status:        r.Status,
			DecidedBy:     fromNullString(r.DecidedBy),
			DecidedAt:     timePtrFromSQLite(r.DecidedAt),
			ScheduleID:    fromNullInt64(r.ScheduleID),
			CreatedAt:     r.CreatedAt.Time,
		},
		BroadcasterLogin: r.BroadcasterLogin,
		BroadcasterName:  r.BroadcasterName,
		ProfileImageURL:  fromNullString(r.ProfileImageUrl),
		RequestedByLogin: r.RequestedByLogin,
		RequestedByName:  r.RequestedByName,
	}
}

func (a *SQLiteAdapter) DecideScheduleRequest(ctx context.Context, id int64, status, decidedBy string, scheduleID *int64) (bool, error) {
	affected, err := a.queries.DecideScheduleRequest(ctx, sqlitegen.DecideScheduleRequestParams{
		ID:         id,
		Status:     status,
		DecidedBy:  toNullString(&decidedBy),
		ScheduleID: toNullInt64(scheduleID),
	})
	if err != nil {
		return false, fmt.Errorf("sqlite decide schedule request %d: %w", id, err)
	}
	return affected > 0, nil
}

// errRequestNotPending forces rollback before approval returns ok=false.
var errRequestNotPending = errors.New("sqlite: schedule request not pending")

func (a *SQLiteAdapter) ApproveScheduleRequest(ctx context.Context, requestID int64, decidedBy string, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, bool, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *sqlitegen.Queries, _ *sql.Tx) error {
		row, err := q.CreateSchedule(ctx, sqliteCreateScheduleParams(input))
		if err != nil {
			return fmt.Errorf("sqlite create schedule: %w", mapErr(err))
		}
		sched := sqliteScheduleToDomain(row)
		// Insert before reading so SQLite acquires its write lock
		// before the duplicate check.
		active, err := q.ListActiveSchedulesForBroadcaster(ctx, input.BroadcasterID)
		if err != nil {
			return fmt.Errorf("sqlite recheck active schedules for %s: %w", input.BroadcasterID, err)
		}
		for _, other := range active {
			if other.ID != sched.ID {
				return fmt.Errorf("sqlite channel %s already actively scheduled: %w", input.BroadcasterID, repository.ErrDuplicate)
			}
		}
		if err := replaceSQLiteScheduleFilters(ctx, q, sched.ID, filters); err != nil {
			return err
		}
		affected, err := q.DecideScheduleRequest(ctx, sqlitegen.DecideScheduleRequestParams{
			ID:         requestID,
			Status:     repository.ScheduleRequestStatusApproved,
			DecidedBy:  toNullString(&decidedBy),
			ScheduleID: toNullInt64(&sched.ID),
		})
		if err != nil {
			return fmt.Errorf("sqlite decide schedule request %d: %w", requestID, err)
		}
		if affected == 0 {
			return errRequestNotPending
		}
		out = sched
		return nil
	})
	if errors.Is(err, errRequestNotPending) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (a *SQLiteAdapter) DeleteScheduleRequest(ctx context.Context, id int64, requestedBy string) (bool, error) {
	affected, err := a.queries.DeleteScheduleRequest(ctx, sqlitegen.DeleteScheduleRequestParams{
		ID:          id,
		RequestedBy: requestedBy,
	})
	if err != nil {
		return false, fmt.Errorf("sqlite delete schedule request %d: %w", id, err)
	}
	return affected > 0, nil
}
