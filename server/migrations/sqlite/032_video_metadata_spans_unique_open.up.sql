-- Collapse pre-index duplicate open spans (see postgres/032 for the
-- shared rationale). SQLite's UPDATE FROM lands rowids via a
-- correlated subquery since older SQLite versions don't expose
-- row_number() in an UPDATE target list.
UPDATE video_title_spans
   SET ended_at = started_at
 WHERE ended_at IS NULL
   AND id NOT IN (
       SELECT MIN(id)
         FROM video_title_spans
        WHERE ended_at IS NULL
        GROUP BY video_id, title_id
   );

UPDATE video_category_spans
   SET ended_at = started_at
 WHERE ended_at IS NULL
   AND id NOT IN (
       SELECT MIN(id)
         FROM video_category_spans
        WHERE ended_at IS NULL
        GROUP BY video_id, category_id
   );

CREATE UNIQUE INDEX idx_video_title_spans_unique_open
    ON video_title_spans (video_id, title_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX idx_video_category_spans_unique_open
    ON video_category_spans (video_id, category_id)
    WHERE ended_at IS NULL;
