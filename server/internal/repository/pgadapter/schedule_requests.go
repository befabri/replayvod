package pgadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func pgScheduleRequestToDomain(row pggen.ScheduleRequest) *repository.ScheduleRequest {
	return &repository.ScheduleRequest{
		ID:            row.ID,
		BroadcasterID: row.BroadcasterID,
		RequestedBy:   row.RequestedBy,
		Note:          row.Note,
		Status:        row.Status,
		DecidedBy:     row.DecidedBy,
		DecidedAt:     row.DecidedAt,
		ScheduleID:    row.ScheduleID,
		CreatedAt:     row.CreatedAt,
	}
}

func (a *PGAdapter) CreateScheduleRequest(ctx context.Context, broadcasterID, requestedBy string, note *string) (*repository.ScheduleRequest, error) {
	row, err := a.queries.CreateScheduleRequest(ctx, pggen.CreateScheduleRequestParams{
		BroadcasterID: broadcasterID,
		RequestedBy:   requestedBy,
		Note:          note,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create schedule request: %w", mapErr(err))
	}
	return pgScheduleRequestToDomain(row), nil
}

func (a *PGAdapter) ListScheduleRequests(ctx context.Context, limit int, cursor *repository.ScheduleRequestCursor) ([]repository.ScheduleRequestView, error) {
	limit, before := repository.ScheduleRequestQueryBounds(limit, cursor)
	rows, err := a.queries.ListScheduleRequests(ctx, pggen.ListScheduleRequestsParams{BeforeCreatedAt: before.CreatedAt, BeforeID: before.ID, PageLimit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("pg list schedule requests: %w", err)
	}
	out := make([]repository.ScheduleRequestView, len(rows))
	for i, r := range rows {
		out[i] = pgScheduleRequestViewToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) ListScheduleRequestsForUser(ctx context.Context, userID string, limit int, cursor *repository.ScheduleRequestCursor) ([]repository.ScheduleRequestView, error) {
	limit, before := repository.ScheduleRequestQueryBounds(limit, cursor)
	rows, err := a.queries.ListScheduleRequestsForUser(ctx, pggen.ListScheduleRequestsForUserParams{BeforeCreatedAt: before.CreatedAt, BeforeID: before.ID, PageLimit: int32(limit), RequestedBy: userID})
	if err != nil {
		return nil, fmt.Errorf("pg list schedule requests for user %s: %w", userID, err)
	}
	out := make([]repository.ScheduleRequestView, len(rows))
	for i, r := range rows {
		out[i] = pgScheduleRequestViewToDomain(pggen.ListScheduleRequestsRow(r))
	}
	return out, nil
}

func (a *PGAdapter) DecideScheduleRequest(ctx context.Context, id int64, status, decidedBy string, scheduleID *int64) (bool, error) {
	affected, err := a.queries.DecideScheduleRequest(ctx, pggen.DecideScheduleRequestParams{
		ID:         id,
		Status:     status,
		DecidedBy:  &decidedBy,
		ScheduleID: scheduleID,
	})
	if err != nil {
		return false, fmt.Errorf("pg decide schedule request %d: %w", id, err)
	}
	return affected > 0, nil
}

// errRequestNotPending forces rollback before approval returns ok=false.
var errRequestNotPending = errors.New("pg: schedule request not pending")

func (a *PGAdapter) ApproveScheduleRequest(ctx context.Context, requestID int64, decidedBy string, input *repository.ScheduleInput, filters repository.ScheduleFilterInput) (*repository.DownloadSchedule, bool, error) {
	var out *repository.DownloadSchedule
	err := a.inTx(ctx, func(q *pggen.Queries, tx pgx.Tx) error {
		// Lock the channel before reading schedules so concurrent
		// approvals see the preceding commit.
		if _, err := tx.Exec(ctx, "SELECT broadcaster_id FROM channels WHERE broadcaster_id = $1 FOR UPDATE", input.BroadcasterID); err != nil {
			return fmt.Errorf("pg lock channel %s: %w", input.BroadcasterID, err)
		}
		active, err := q.ListActiveSchedulesForBroadcaster(ctx, input.BroadcasterID)
		if err != nil {
			return fmt.Errorf("pg recheck active schedules for %s: %w", input.BroadcasterID, err)
		}
		if len(active) > 0 {
			return fmt.Errorf("pg channel %s already actively scheduled: %w", input.BroadcasterID, repository.ErrDuplicate)
		}
		row, err := q.CreateSchedule(ctx, pgCreateScheduleParams(input))
		if err != nil {
			return fmt.Errorf("pg create schedule: %w", mapErr(err))
		}
		sched := pgScheduleToDomain(row)
		if err := replacePGScheduleFilters(ctx, q, sched.ID, filters); err != nil {
			return err
		}
		affected, err := q.DecideScheduleRequest(ctx, pggen.DecideScheduleRequestParams{
			ID:         requestID,
			Status:     repository.ScheduleRequestStatusApproved,
			DecidedBy:  &decidedBy,
			ScheduleID: &sched.ID,
		})
		if err != nil {
			return fmt.Errorf("pg decide schedule request %d: %w", requestID, err)
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

func (a *PGAdapter) DeleteScheduleRequest(ctx context.Context, id int64, requestedBy string) (bool, error) {
	affected, err := a.queries.DeleteScheduleRequest(ctx, pggen.DeleteScheduleRequestParams{
		ID:          id,
		RequestedBy: requestedBy,
	})
	if err != nil {
		return false, fmt.Errorf("pg delete schedule request %d: %w", id, err)
	}
	return affected > 0, nil
}

func pgScheduleRequestViewToDomain(r pggen.ListScheduleRequestsRow) repository.ScheduleRequestView {
	return repository.ScheduleRequestView{
		ScheduleRequest: repository.ScheduleRequest{
			ID:            r.ID,
			BroadcasterID: r.BroadcasterID,
			RequestedBy:   r.RequestedBy,
			Note:          r.Note,
			Status:        r.Status,
			DecidedBy:     r.DecidedBy,
			DecidedAt:     r.DecidedAt,
			ScheduleID:    r.ScheduleID,
			CreatedAt:     r.CreatedAt,
		},
		BroadcasterLogin: r.BroadcasterLogin,
		BroadcasterName:  r.BroadcasterName,
		ProfileImageURL:  r.ProfileImageUrl,
		RequestedByLogin: r.RequestedByLogin,
		RequestedByName:  r.RequestedByName,
	}
}
