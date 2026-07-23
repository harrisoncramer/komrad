-- name: ListWorkflows :many
SELECT
    *
FROM
    workflows
WHERE
    is_deleted = 0
ORDER BY
    updated_at DESC;

-- name: GetWorkflow :one
SELECT
    *
FROM
    workflows
WHERE
    id = @id
    AND is_deleted = 0
LIMIT 1;

-- name: InsertWorkflow :one
INSERT INTO workflows (id, name)
    VALUES (@id, @name)
RETURNING
    *;

-- name: RenameWorkflow :exec
UPDATE
    workflows
SET
    name = @name,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: MarkWorkflowDeleted :exec
UPDATE
    workflows
SET
    is_deleted = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;
