-- Records why a recording was tombstoned: 'retention' (auto-pruned by the
-- schedule retention sweep) or 'manual' (an operator removed it via the new
-- video.delete mutation). NULL while the row is live (deleted_at IS NULL).
ALTER TABLE videos ADD COLUMN IF NOT EXISTS deletion_kind TEXT
    CHECK (deletion_kind IS NULL OR deletion_kind IN ('retention', 'manual'));

-- Backfill existing tombstones. Retention was the only deletion path before
-- this migration, so every already-removed recording was a retention prune.
UPDATE videos SET deletion_kind = 'retention'
WHERE deleted_at IS NOT NULL AND deletion_kind IS NULL;

-- Queue marker for operator-requested recording deletion. The row remains live
-- until the background deletion task has purged storage objects, respected
-- recording-webhook frozen-part invariants, and finalized the tombstone.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS delete_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_videos_delete_requested_at
    ON videos (delete_requested_at)
    WHERE delete_requested_at IS NOT NULL AND deleted_at IS NULL;
