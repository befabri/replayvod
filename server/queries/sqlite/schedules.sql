-- name: CreateSchedule :one
INSERT INTO download_schedules (
    broadcaster_id, requested_by, quality,
    has_min_viewers, min_viewers,
    has_categories, has_tags,
    is_delete_rediff, time_before_delete,
    is_disabled
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM download_schedules WHERE id = ?;

-- name: GetScheduleForUserChannel :one
SELECT * FROM download_schedules
WHERE broadcaster_id = ? AND requested_by = ?;

-- name: UpdateSchedule :one
UPDATE download_schedules SET
    quality             = ?,
    has_min_viewers     = ?,
    min_viewers         = ?,
    has_categories      = ?,
    has_tags            = ?,
    is_delete_rediff    = ?,
    time_before_delete  = ?,
    is_disabled         = ?,
    updated_at          = datetime('now')
WHERE id = ?
RETURNING *;

-- name: ToggleSchedule :one
-- SQLite stores booleans as INTEGER; "NOT is_disabled" works but flips
-- between 0/1 explicitly via CASE for clarity on non-boolean-ish values.
UPDATE download_schedules SET
    is_disabled = CASE WHEN is_disabled = 0 THEN 1 ELSE 0 END,
    updated_at  = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteSchedule :exec
DELETE FROM download_schedules WHERE id = ?;

-- name: ListSchedules :many
SELECT * FROM download_schedules ORDER BY created_at DESC LIMIT ? OFFSET ?;

-- name: ListSchedulesForUser :many
SELECT * FROM download_schedules
WHERE requested_by = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListActiveSchedulesForBroadcaster :many
SELECT * FROM download_schedules
WHERE broadcaster_id = ? AND is_disabled = 0;

-- name: RecordScheduleTrigger :exec
UPDATE download_schedules SET
    last_triggered_at = datetime('now'),
    trigger_count = trigger_count + 1
WHERE id = ?;

-- name: LinkScheduleCategory :exec
INSERT INTO download_schedule_categories (schedule_id, category_id)
VALUES (?, ?)
ON CONFLICT DO NOTHING;

-- name: UnlinkScheduleCategory :exec
DELETE FROM download_schedule_categories
WHERE schedule_id = ? AND category_id = ?;

-- name: ClearScheduleCategories :exec
DELETE FROM download_schedule_categories WHERE schedule_id = ?;

-- name: ListScheduleCategories :many
SELECT c.* FROM categories c
INNER JOIN download_schedule_categories dsc ON dsc.category_id = c.id
WHERE dsc.schedule_id = ?
ORDER BY c.name;

-- name: LinkScheduleTag :exec
INSERT INTO download_schedule_tags (schedule_id, tag_id)
VALUES (?, ?)
ON CONFLICT DO NOTHING;

-- name: UnlinkScheduleTag :exec
DELETE FROM download_schedule_tags
WHERE schedule_id = ? AND tag_id = ?;

-- name: ClearScheduleTags :exec
DELETE FROM download_schedule_tags WHERE schedule_id = ?;

-- name: ListScheduleTags :many
SELECT t.* FROM tags t
INNER JOIN download_schedule_tags dst ON dst.tag_id = t.id
WHERE dst.schedule_id = ?
ORDER BY t.name;
