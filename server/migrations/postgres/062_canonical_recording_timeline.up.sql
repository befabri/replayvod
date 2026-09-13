-- Upgrade history that predates the observation log. Preserve existing events
-- and only import a dimension when that recording has no observations for it.
-- A span boundary is evidence of its own dimension, not of the other dimension.
INSERT INTO video_metadata_changes(video_id, occurred_at, title_id, category_id)
SELECT s.video_id, s.started_at, s.title_id, NULL
FROM video_title_spans s
WHERE NOT EXISTS (
    SELECT 1 FROM video_metadata_changes c WHERE c.video_id=s.video_id AND c.title_id IS NOT NULL
)
UNION ALL
SELECT s.video_id, s.started_at, NULL, s.category_id
FROM video_category_spans s
WHERE NOT EXISTS (
    SELECT 1 FROM video_metadata_changes c WHERE c.video_id=s.video_id AND c.category_id IS NOT NULL
);
