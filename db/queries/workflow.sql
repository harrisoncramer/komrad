-- name: ListWorkflows :many
SELECT
    w.*,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.is_deleted = 0
ORDER BY
    w.updated_at DESC;

-- name: GetWorkflow :one
SELECT
    w.*,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.id = @id
    AND w.is_deleted = 0
LIMIT 1;

-- name: InsertWorkflowConfig :exec
INSERT INTO workflow_configs (id, cwd, flags)
    VALUES (@id, @cwd, @flags);

-- name: InsertWorkflow :one
INSERT INTO workflows (id, name, workflow_config_id)
    VALUES (@id, @name, @workflow_config_id)
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

