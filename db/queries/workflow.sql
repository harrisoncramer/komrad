-- name: ListWorkflowsPage :many
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    ac.setup_action,
    ac.cleanup_action,
    ac.worktree_enabled,
    ac.worktree_base_branch,
    ac.worktree_copy_paths,
    ac.worktree_symlink_paths,
    ac.worktree_diff_exclude,
    ac.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_action_configs ac ON ac.id = w.action_config_id
WHERE (@archived::boolean) =(w.archived_at IS NOT NULL)
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR (LOWER(w.name)
            COLLATE "C",
            w.id) >(LOWER(sqlc.narg('cursor_name')::text)
            COLLATE "C",
            sqlc.narg('cursor_id')::uuid))
ORDER BY
    LOWER(w.name)
    COLLATE "C" ASC,
    w.id ASC
LIMIT @lim;

-- name: ListWorkflows :many
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    ac.setup_action,
    ac.cleanup_action,
    ac.worktree_enabled,
    ac.worktree_base_branch,
    ac.worktree_copy_paths,
    ac.worktree_symlink_paths,
    ac.worktree_diff_exclude,
    ac.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_action_configs ac ON ac.id = w.action_config_id
WHERE
    w.archived_at IS NULL
ORDER BY
    LOWER(w.name)
    COLLATE "C" ASC,
    w.id ASC;

-- name: GetWorkflow :one
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    ac.setup_action,
    ac.cleanup_action,
    ac.worktree_enabled,
    ac.worktree_base_branch,
    ac.worktree_copy_paths,
    ac.worktree_symlink_paths,
    ac.worktree_diff_exclude,
    ac.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_action_configs ac ON ac.id = w.action_config_id
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

-- name: InsertWorkflowActionConfig :one
INSERT INTO workflow_action_configs(
    setup_action,
    cleanup_action,
    worktree_enabled,
    worktree_base_branch,
    worktree_copy_paths,
    worktree_symlink_paths,
    worktree_diff_exclude,
    archive_on_pr_closed)
VALUES (
    @setup_action,
    @cleanup_action,
    @worktree_enabled,
    @worktree_base_branch,
    @worktree_copy_paths,
    @worktree_symlink_paths,
    @worktree_diff_exclude,
    @archive_on_pr_closed)
RETURNING
    id;

-- name: InsertWorkflow :one
INSERT INTO workflows(
    name,
    context,
    workflow_config_id,
    action_config_id)
VALUES (
    @name,
    @context,
    @workflow_config_id,
    @action_config_id)
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
    workflow_action_configs
SET
    setup_action = @setup_action,
    cleanup_action = @cleanup_action,
    worktree_enabled = @worktree_enabled,
    worktree_base_branch = @worktree_base_branch,
    worktree_copy_paths = @worktree_copy_paths,
    worktree_symlink_paths = @worktree_symlink_paths,
    worktree_diff_exclude = @worktree_diff_exclude,
    archive_on_pr_closed = @archive_on_pr_closed
WHERE
    id =(
        SELECT
            action_config_id
        FROM
            workflows
        WHERE
            workflows.id = @workflow_id);

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

-- name: DeleteWorkflow :one
DELETE FROM workflows
WHERE id = @id
RETURNING
    workflow_config_id,
    action_config_id;

-- name: DeleteWorkflowConfig :exec
DELETE FROM workflow_configs
WHERE id = @id;

-- name: DeleteWorkflowActionConfig :exec
DELETE FROM workflow_action_configs
WHERE id = @id;

-- name: GetWorkflowIncludingArchived :one
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    ac.setup_action,
    ac.cleanup_action,
    ac.worktree_enabled,
    ac.worktree_base_branch,
    ac.worktree_copy_paths,
    ac.worktree_symlink_paths,
    ac.worktree_diff_exclude,
    ac.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_action_configs ac ON ac.id = w.action_config_id
WHERE
    w.id = @id
LIMIT 1;
