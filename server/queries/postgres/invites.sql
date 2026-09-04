-- name: CreateInvite :one
INSERT INTO invites (token_hash, role, note, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInviteByTokenHash :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: RedeemInvite :execrows
UPDATE invites
SET redeemed_at = NOW(), redeemed_by = $2
WHERE token_hash = $1 AND redeemed_at IS NULL AND expires_at > NOW();

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC, id DESC;

-- name: DeleteInvite :execrows
DELETE FROM invites WHERE id = $1 AND redeemed_at IS NULL;
