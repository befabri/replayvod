-- name: CreateScheduleRequest :one
INSERT INTO schedule_requests (broadcaster_id, requested_by, note)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetScheduleRequest :one
SELECT * FROM schedule_requests WHERE id = ?;

-- name: ListScheduleRequests :many
SELECT sr.*, c.broadcaster_login, c.broadcaster_name, c.profile_image_url,
       u.login AS requested_by_login, u.display_name AS requested_by_name
FROM schedule_requests sr
INNER JOIN channels c ON c.broadcaster_id = sr.broadcaster_id
INNER JOIN users u ON u.id = sr.requested_by
WHERE (sr.created_at, sr.id) < (sqlc.arg(before_created_at), CAST(sqlc.arg(before_id) AS INTEGER))
ORDER BY sr.created_at DESC, sr.id DESC
LIMIT sqlc.arg(limit);

-- name: ListScheduleRequestsForUser :many
SELECT sr.*, c.broadcaster_login, c.broadcaster_name, c.profile_image_url,
       u.login AS requested_by_login, u.display_name AS requested_by_name
FROM schedule_requests sr
INNER JOIN channels c ON c.broadcaster_id = sr.broadcaster_id
INNER JOIN users u ON u.id = sr.requested_by
WHERE sr.requested_by = sqlc.arg(user_id) AND (sr.created_at, sr.id) < (sqlc.arg(before_created_at), CAST(sqlc.arg(before_id) AS INTEGER))
ORDER BY sr.created_at DESC, sr.id DESC
LIMIT sqlc.arg(limit);

-- name: DecideScheduleRequest :execrows
UPDATE schedule_requests
SET status = ?, decided_by = ?, decided_at = datetime('now'), schedule_id = ?
WHERE id = ? AND status = 'PENDING';

-- name: DeleteScheduleRequest :execrows
DELETE FROM schedule_requests
WHERE id = ? AND requested_by = ? AND status = 'PENDING';
