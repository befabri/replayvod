-- name: InsertVideoMetadataChange :one
INSERT INTO video_metadata_changes (video_id, occurred_at, title_id, category_id)
VALUES (@video_id, @occurred_at, @title_id, @category_id)
RETURNING id;

-- name: ListVideoMetadataChangesForVideo :many
SELECT
    vmc.id,
    vmc.video_id,
    vmc.occurred_at,
    vmc.title_id,
    t.name        AS title_name,
    t.created_at  AS title_created_at,
    vmc.category_id,
    c.name        AS category_name,
    c.box_art_url AS category_box_art_url,
    c.igdb_id     AS category_igdb_id,
    c.created_at  AS category_created_at,
    c.updated_at  AS category_updated_at
FROM video_metadata_changes vmc
LEFT JOIN titles     t ON t.id = vmc.title_id
LEFT JOIN categories c ON c.id = vmc.category_id
WHERE vmc.video_id = @video_id
ORDER BY vmc.occurred_at ASC, vmc.id ASC;
