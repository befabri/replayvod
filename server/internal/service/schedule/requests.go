package schedule

import (
	"context"
	"errors"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrRequestNotFound covers absent requests and, during cancellation, foreign
// or decided requests to prevent enumeration.
var ErrRequestNotFound = errors.New("schedule: request not found")

// ErrRequestAlreadyDecided is returned when approving/rejecting a
// request that is no longer pending.
var ErrRequestAlreadyDecided = errors.New("schedule: request already decided")

// ErrRequestAlreadyExists is returned when the user already has a
// pending request for this channel.
var ErrRequestAlreadyExists = errors.New("schedule: pending request already exists")

// ErrAlreadyScheduled indicates that the channel already has an active
// schedule.
var ErrAlreadyScheduled = errors.New("schedule: channel already has an active schedule")

// CreateRequest files a request for a known channel with no active schedule.
// Each user may have one pending request per channel.
func (s *Service) CreateRequest(ctx context.Context, userID, broadcasterID string, note *string) (*repository.ScheduleRequest, error) {
	if _, err := s.repo.GetChannel(ctx, broadcasterID); err != nil {
		return nil, err
	}
	active, err := s.repo.ListActiveSchedulesForBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("check existing schedules: %w", err)
	}
	if len(active) > 0 {
		return nil, ErrAlreadyScheduled
	}
	req, err := s.repo.CreateScheduleRequest(ctx, broadcasterID, userID, note)
	if errors.Is(err, repository.ErrDuplicate) {
		// The partial unique index is the authority for pending duplicates.
		return nil, ErrRequestAlreadyExists
	}
	if err != nil {
		return nil, fmt.Errorf("create schedule request: %w", err)
	}
	s.log.Info("schedule requested", "request_id", req.ID, "broadcaster_id", broadcasterID, "requested_by", userID)
	return req, nil
}

type RequestPage struct {
	Items      []repository.ScheduleRequestView
	NextCursor *repository.ScheduleRequestCursor
}

func requestPageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return min(limit, 200)
}

func requestPage(rows []repository.ScheduleRequestView, limit int) RequestPage {
	page := RequestPage{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := page.Items[limit-1]
		page.NextCursor = &repository.ScheduleRequestCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page
}

func (s *Service) ListRequests(ctx context.Context, limit int, cursor *repository.ScheduleRequestCursor) (RequestPage, error) {
	limit = requestPageLimit(limit)
	rows, err := s.repo.ListScheduleRequests(ctx, limit+1, cursor)
	if err != nil {
		return RequestPage{}, err
	}
	return requestPage(rows, limit), nil
}

func (s *Service) ListRequestsForUser(ctx context.Context, userID string, limit int, cursor *repository.ScheduleRequestCursor) (RequestPage, error) {
	limit = requestPageLimit(limit)
	rows, err := s.repo.ListScheduleRequestsForUser(ctx, userID, limit+1, cursor)
	if err != nil {
		return RequestPage{}, err
	}
	return requestPage(rows, limit), nil
}

// ApproveRequest atomically approves the request and creates an admin-owned
// schedule. The request determines the channel regardless of
// input.BroadcasterID and retains requester attribution; live recording starts
// only after commit.
func (s *Service) ApproveRequest(ctx context.Context, adminID string, requestID int64, input WriteInput) (*View, error) {
	req, err := s.repo.GetScheduleRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRequestNotFound
		}
		return nil, err
	}
	if req.Status != repository.ScheduleRequestStatusPending {
		return nil, ErrRequestAlreadyDecided
	}
	// Another request may have been approved since filing; leave this one
	// pending for rejection.
	active, err := s.repo.ListActiveSchedulesForBroadcaster(ctx, req.BroadcasterID)
	if err != nil {
		return nil, fmt.Errorf("check existing schedules: %w", err)
	}
	if len(active) > 0 {
		return nil, ErrAlreadyScheduled
	}

	input.BroadcasterID = req.BroadcasterID
	scheduleInput, filters, err := buildScheduleInput(adminID, input, nil)
	if err != nil {
		return nil, err
	}
	scheduleInput.RequestedFrom = &req.RequestedBy
	sched, ok, err := s.repo.ApproveScheduleRequest(ctx, requestID, adminID, scheduleInput, filters)
	if errors.Is(err, repository.ErrDuplicate) {
		return nil, ErrAlreadyScheduled
	}
	if err != nil {
		return nil, fmt.Errorf("approve schedule request: %w", err)
	}
	if !ok {
		return nil, ErrRequestAlreadyDecided
	}
	s.log.Info("schedule request approved", "request_id", requestID, "schedule_id", sched.ID, "requested_by", req.RequestedBy, "decided_by", adminID)
	sched = s.triggerLiveIfEligible(ctx, sched)
	return s.inflateOne(ctx, sched)
}

// RejectRequest closes a pending request without creating a schedule.
func (s *Service) RejectRequest(ctx context.Context, adminID string, requestID int64) error {
	ok, err := s.repo.DecideScheduleRequest(ctx, requestID, repository.ScheduleRequestStatusRejected, adminID, nil)
	if err != nil {
		return fmt.Errorf("mark request rejected: %w", err)
	}
	if !ok {
		_, getErr := s.repo.GetScheduleRequest(ctx, requestID)
		if errors.Is(getErr, repository.ErrNotFound) {
			return ErrRequestNotFound
		}
		if getErr != nil {
			return fmt.Errorf("load request after rejected decision: %w", getErr)
		}
		return ErrRequestAlreadyDecided
	}
	s.log.Info("schedule request rejected", "request_id", requestID, "decided_by", adminID)
	return nil
}

// CancelRequest lets the requester withdraw their own pending request.
func (s *Service) CancelRequest(ctx context.Context, userID string, requestID int64) error {
	ok, err := s.repo.DeleteScheduleRequest(ctx, requestID, userID)
	if err != nil {
		return fmt.Errorf("cancel schedule request: %w", err)
	}
	if !ok {
		return ErrRequestNotFound
	}
	return nil
}
