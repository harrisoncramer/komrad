-- name: ListWorkflowsPage :many
SELECT
    sqlc.embed(w),
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.is_deleted = FALSE
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR w.id < sqlc.narg('cursor_id'))
ORDER BY
    w.id DESC
LIMIT @lim;

-- name: ListWorkflows :many
SELECT
    sqlc.embed(w),
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.is_deleted = FALSE
ORDER BY
    w.id DESC;

-- name: GetWorkflow :one
SELECT
    sqlc.embed(w),
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.id = @id
    AND w.is_deleted = FALSE
LIMIT 1;

-- name: InsertWorkflowConfig :one
INSERT INTO workflow_configs(
    cwd,
    flags)
VALUES (
    @cwd,
    @flags)
RETURNING
    id;

-- name: InsertWorkflow :one
INSERT INTO workflows(
    name,
    context,
    workflow_config_id)
VALUES (
    @name,
    @context,
    @workflow_config_id)
RETURNING
    *;

-- name: RenameWorkflow :exec
UPDATE
    workflows
SET
    name = @name
WHERE
    id = @id;

-- name: SetWorkflowContext :exec
UPDATE
    workflows
SET
    context = @context
WHERE
    id = @id;

-- name: UpdateWorkflowConfig :exec
UPDATE
    workflow_configs
SET
    cwd = @cwd,
    flags = @flags
WHERE
    id =(
        SELECT
            workflow_config_id
        FROM
            workflows
        WHERE
            workflows.id = @workflow_id);

-- name: MarkWorkflowDeleted :exec
UPDATE
    workflows
SET
    is_deleted = TRUE
WHERE
    id = @id;
