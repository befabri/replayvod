CREATE TABLE IF NOT EXISTS invites (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash  TEXT UNIQUE NOT NULL,
    role        TEXT NOT NULL,
    note        TEXT,
    created_by  TEXT NOT NULL REFERENCES users(id),
    expires_at  TEXT NOT NULL,
    redeemed_at TEXT,
    redeemed_by TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS schedule_requests (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    broadcaster_id TEXT NOT NULL REFERENCES channels(broadcaster_id),
    requested_by   TEXT NOT NULL REFERENCES users(id),
    note           TEXT,
    status         TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    decided_by     TEXT REFERENCES users(id),
    decided_at     TEXT,
    schedule_id    INTEGER REFERENCES download_schedules(id) ON DELETE SET NULL,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_requests_pending_unique
ON schedule_requests (broadcaster_id, requested_by) WHERE status = 'PENDING';

-- Request-origin attribution: schedules created by approving a request
-- are owned by the deciding admin (requested_by) but remember who asked
-- (requested_from). NULL for directly created schedules. Attribution
-- only, so a deleted requester clears it instead of blocking the delete.
ALTER TABLE download_schedules ADD COLUMN requested_from TEXT REFERENCES users(id) ON DELETE SET NULL;

-- Keep the retired video_requests table for existing history and downgrades.
-- The old API could populate it even without a dashboard UI. A request for a
-- particular video is not consent to automatically record its channel, so it
-- must not be converted into a schedule request. New code uses schedule_requests.
