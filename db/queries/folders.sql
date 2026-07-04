-- name: CreateFolder :exec
INSERT INTO folders (id, display_name, parent_id)
VALUES (:id, :display_name, :parent_id);

-- name: GetFolder :one
SELECT id, display_name, parent_id
FROM folders WHERE id = :id;

-- name: GetFolders :many
SELECT id, display_name, parent_id
FROM folders;

-- name: GetFolderAncestors :many
WITH RECURSIVE ancestor_chain AS (
    SELECT id, display_name, parent_id, 0 AS depth
    FROM folders
    WHERE folders.id = :id

    UNION ALL

    SELECT f.id, f.display_name, f.parent_id, ac.depth + 1
    FROM folders f
    JOIN ancestor_chain ac ON f.id = ac.parent_id
)
SELECT display_name 
FROM ancestor_chain 
WHERE id != 'root'
ORDER BY depth DESC;

-- name: CheckFolderHasChildren :one
SELECT EXISTS (
    SELECT 1 FROM folders WHERE (parent_id = :id OR (:id IS NULL AND parent_id IS NULL))
    UNION ALL
    SELECT 1 FROM artists WHERE (folder_id = :id OR (:id IS NULL AND folder_id IS NULL))
);

-- name: CheckFolderExists :one
SELECT EXISTS (
    SELECT 1 FROM folders where id = :id
);

-- name: DeleteFolder :exec
DELETE FROM folders
WHERE id = :id;

-- name: UpdateFolder :exec
UPDATE folders 
SET id = :new_id, display_name = :display_name, parent_id = :parent_id 
WHERE id = :old_id;
