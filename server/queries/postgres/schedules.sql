-- name: CreateSchedule :one
INSERT INTO download_schedules (
    broadcaster_id, requested_by, quality,
    has_min_viewers, min_viewers,
    has_categories, has_tags,
    is_delete_rediff, time_before_delete,
    is_disabled
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM download_schedules WHERE id = $1;

-- name: GetScheduleForUserChannel :one
SELECT * FROM download_schedules
WHERE broadcaster_id = $1 AND requested_by = $2;

-- name: UpdateSchedule :one
UPDATE download_schedules SET
    quality             = $2,
    has_min_viewers     = $3,
    min_viewers         = $4,
    has_categories      = $5,
    has_tags            = $6,
    is_delete_rediff    = $7,
    time_before_delete  = $8,
    is_disabled         = $9,
    updated_at          = NOW()
WHERE id = $1
RETURNING *;

-- name: ToggleSchedule :one
UPDATE download_schedules SET is_disabled = NOT is_disabled, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteSchedule :exec
DELETE FROM download_schedules WHERE id = $1;

-- name: ListSchedules :many
SELECT * FROM download_schedules ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: ListSchedulesForUser :many
SELECT * FROM download_schedules
WHERE requested_by = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListActiveSchedulesForBroadcaster :many
-- Match path: called on every stream.online event. Partial index
-- idx_schedules_active makes this O(log active_schedules).
SELECT * FROM download_schedules
WHERE broadcaster_id = $1 AND is_disabled = FALSE;

-- name: RecordScheduleTrigger :exec
-- Atomic increment + timestamp stamp. Called after a successful auto-download
-- trigger so the dashboard can show "this schedule fired N times, last at T".
UPDATE download_schedules SET
    last_triggered_at = NOW(),
    trigger_count = trigger_count + 1
WHERE id = $1;

-- name: LinkScheduleCategory :exec
INSERT INTO download_schedule_categories (schedule_id, category_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnlinkScheduleCategory :exec
DELETE FROM download_schedule_categories
WHERE schedule_id = $1 AND category_id = $2;

-- name: ClearScheduleCategories :exec
DELETE FROM download_schedule_categories WHERE schedule_id = $1;

-- name: ListScheduleCategories :many
SELECT c.* FROM categories c
INNER JOIN download_schedule_categories dsc ON dsc.category_id = c.id
WHERE dsc.schedule_id = $1
ORDER BY c.name;

-- name: LinkScheduleTag :exec
INSERT INTO download_schedule_tags (schedule_id, tag_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnlinkScheduleTag :exec
DELETE FROM download_schedule_tags
WHERE schedule_id = $1 AND tag_id = $2;

-- name: ClearScheduleTags :exec
DELETE FROM download_schedule_tags WHERE schedule_id = $1;

-- name: ListScheduleTags :many
SELECT t.* FROM tags t
INNER JOIN download_schedule_tags dst ON dst.tag_id = t.id
WHERE dst.schedule_id = $1
ORDER BY t.name;
