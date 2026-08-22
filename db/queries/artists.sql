-- name: CreateArtist :one
INSERT INTO artists (id, display_name, folder_id)
VALUES (:id, :display_name, :folder_id)
RETURNING id, display_name, folder_id;

-- name: GetArtist :one
SELECT
    id,
    display_name,
    folder_id
FROM artists
WHERE id = :id;

-- name: GetArtists :many
SELECT
    id,
    display_name,
    folder_id
FROM artists;

-- name: UpdateArtist :one
UPDATE artists
SET id = :new_id, display_name = :display_name, folder_id = :folder_id
WHERE id = :old_id
RETURNING id, display_name, folder_id;

-- name: DeleteArtist :execrows
DELETE FROM artists
WHERE id = :id;
