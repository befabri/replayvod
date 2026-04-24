-- Collapse any duplicate open spans created by pre-index concurrent
-- writers before enforcing uniqueness. For each (video_id, title_id)
-- pair with >1 open row, keep the oldest and close the rest at
-- their own started_at (zero-duration) so totals are unaffected.
WITH dupes AS (
    SELECT id,
           video_id,
           title_id,
           started_at,
           ROW_NUMBER() OVER (
               PARTITION BY video_id, title_id
               ORDER BY started_at ASC, id ASC
           ) AS rn
    FROM video_title_spans
    WHERE ended_at IS NULL
)
UPDATE video_title_spans vts
   SET ended_at = dupes.started_at
  FROM dupes
 WHERE vts.id = dupes.id
   AND dupes.rn > 1;

WITH dupes AS (
    SELECT id,
           video_id,
           category_id,
           started_at,
           ROW_NUMBER() OVER (
               PARTITION BY video_id, category_id
               ORDER BY started_at ASC, id ASC
           ) AS rn
    FROM video_category_spans
    WHERE ended_at IS NULL
)
UPDATE video_category_spans vcs
   SET ended_at = dupes.started_at
  FROM dupes
 WHERE vcs.id = dupes.id
   AND dupes.rn > 1;

CREATE UNIQUE INDEX idx_video_title_spans_unique_open
    ON video_title_spans (video_id, title_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_category_spans_unique_open
    ON video_category_spans (video_id, category_id)
    WHERE ended_at IS NULL;
