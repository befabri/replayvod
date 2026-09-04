-- name: CreateInvite :one
INSERT INTO invites (token_hash, role, note, created_by, expires_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetInviteByTokenHash :one
SELECT * FROM invites WHERE token_hash = ?;

-- name: RedeemInvite :execrows
UPDATE invites
SET redeemed_at = datetime('now'), redeemed_by = ?
WHERE token_hash = ? AND redeemed_at IS NULL AND expires_at > datetime('now');

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC, id DESC;

-- name: DeleteInvite :execrows
DELETE FROM invites WHERE id = ? AND redeemed_at IS NULL;
