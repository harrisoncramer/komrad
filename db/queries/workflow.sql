-- name: ListWorkflowsPage :many
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE (@archived::boolean) =(w.archived_at IS NOT NULL)
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR w.id < sqlc.narg('cursor_id'))
ORDER BY
    w.id DESC
LIMIT @lim;

-- name: ListWorkflows :many
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.archived_at IS NULL
ORDER BY
    w.id DESC;

-- name: GetWorkflow :one
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.id = @id
    AND w.archived_at IS NULL
LIMIT 1;

-- name: InsertWorkflowConfig :one
INSERT INTO workflow_configs(
    harness,
    cwd,
    flags)
VALUES (
    @harness,
    @cwd,
    @flags)
RETURNING
    id;

-- name: InsertWorkflow :one
INSERT INTO workflows(
    name,
    context,
    setup_action,
    cleanup_action,
    workflow_config_id)
VALUES (
    @name,
    @context,
    @setup_action,
    @cleanup_action,
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

-- name: SetWorkflowActions :exec
UPDATE
    workflows
SET
    setup_action = @setup_action,
    cleanup_action = @cleanup_action
WHERE
    id = @id;

-- name: UpdateWorkflowConfig :exec
UPDATE
    workflow_configs
SET
    harness = @harness,
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

-- name: MarkWorkflowArchived :exec
UPDATE
    workflows
SET
    archived_at = NOW()
WHERE
    id = @id
    AND archived_at IS NULL;

-- name: MarkWorkflowRestored :exec
UPDATE
    workflows
SET
    archived_at = NULL
WHERE
    id = @id;

-- name: GetWorkflowIncludingArchived :one
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
WHERE
    w.id = @id
LIMIT 1;
