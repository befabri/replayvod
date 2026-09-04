-- name: CreateScheduleRequest :one
INSERT INTO schedule_requests (broadcaster_id, requested_by, note)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetScheduleRequest :one
SELECT * FROM schedule_requests WHERE id = $1;

-- name: ListScheduleRequests :many
SELECT sr.*, c.broadcaster_login, c.broadcaster_name, c.profile_image_url,
       u.login AS requested_by_login, u.display_name AS requested_by_name
FROM schedule_requests sr
INNER JOIN channels c ON c.broadcaster_id = sr.broadcaster_id
INNER JOIN users u ON u.id = sr.requested_by
WHERE (sr.created_at, sr.id) < (sqlc.arg(before_created_at), CAST(sqlc.arg(before_id) AS BIGINT))
ORDER BY sr.created_at DESC, sr.id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListScheduleRequestsForUser :many
SELECT sr.*, c.broadcaster_login, c.broadcaster_name, c.profile_image_url,
       u.login AS requested_by_login, u.display_name AS requested_by_name
FROM schedule_requests sr
INNER JOIN channels c ON c.broadcaster_id = sr.broadcaster_id
INNER JOIN users u ON u.id = sr.requested_by
WHERE sr.requested_by = sqlc.arg(requested_by) AND (sr.created_at, sr.id) < (sqlc.arg(before_created_at), CAST(sqlc.arg(before_id) AS BIGINT))
ORDER BY sr.created_at DESC, sr.id DESC
LIMIT sqlc.arg(page_limit);

-- name: DecideScheduleRequest :execrows
UPDATE schedule_requests
SET status = $2, decided_by = $3, decided_at = NOW(), schedule_id = $4
WHERE id = $1 AND status = 'PENDING';

-- name: DeleteScheduleRequest :execrows
DELETE FROM schedule_requests
WHERE id = $1 AND requested_by = $2 AND status = 'PENDING';
