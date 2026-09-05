-- name: ListWorkflowsPage :many
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    pc.worktree_enabled,
    pc.worktree_base_branch,
    pc.worktree_dir,
    pc.worktree_copy_paths,
    pc.worktree_symlink_paths,
    pc.worktree_diff_exclude,
    pc.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
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
    pc.worktree_enabled,
    pc.worktree_base_branch,
    pc.worktree_dir,
    pc.worktree_copy_paths,
    pc.worktree_symlink_paths,
    pc.worktree_diff_exclude,
    pc.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
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
    pc.worktree_enabled,
    pc.worktree_base_branch,
    pc.worktree_dir,
    pc.worktree_copy_paths,
    pc.worktree_symlink_paths,
    pc.worktree_diff_exclude,
    pc.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
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

-- name: InsertWorkflowProvisionerConfig :one
INSERT INTO workflow_provisioner_configs(
    worktree_enabled,
    worktree_base_branch,
    worktree_dir,
    worktree_copy_paths,
    worktree_symlink_paths,
    worktree_diff_exclude,
    archive_on_pr_closed)
VALUES (
    @worktree_enabled,
    @worktree_base_branch,
    @worktree_dir,
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
    provisioner_config_id)
VALUES (
    @name,
    @context,
    @workflow_config_id,
    @provisioner_config_id)
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

-- name: SetWorkflowProvisioner :exec
UPDATE
    workflow_provisioner_configs
SET
    worktree_enabled = @worktree_enabled,
    worktree_base_branch = @worktree_base_branch,
    worktree_dir = @worktree_dir,
    worktree_copy_paths = @worktree_copy_paths,
    worktree_symlink_paths = @worktree_symlink_paths,
    worktree_diff_exclude = @worktree_diff_exclude,
    archive_on_pr_closed = @archive_on_pr_closed
WHERE
    id =(
        SELECT
            provisioner_config_id
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
WITH freed AS (
    DELETE FROM marks
    WHERE marks.workflow_id = @id)
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
    provisioner_config_id;

-- name: DeleteWorkflowConfig :exec
DELETE FROM workflow_configs
WHERE id = @id;

-- name: DeleteWorkflowProvisionerConfig :exec
DELETE FROM workflow_provisioner_configs
WHERE id = @id;

-- name: GetWorkflowIncludingArchived :one
SELECT
    sqlc.embed(w),
    wc.harness,
    wc.cwd,
    wc.flags,
    pc.worktree_enabled,
    pc.worktree_base_branch,
    pc.worktree_dir,
    pc.worktree_copy_paths,
    pc.worktree_symlink_paths,
    pc.worktree_diff_exclude,
    pc.archive_on_pr_closed
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
WHERE
    w.id = @id
LIMIT 1;

-- name: ListWorkflowHooks :many
SELECT
    workflow_id,
    event,
    script
FROM
    workflow_hooks
ORDER BY
    workflow_id,
    position,
    id;

-- name: ListHooksForWorkflows :many
SELECT
    workflow_id,
    event,
    script
FROM
    workflow_hooks
WHERE
    workflow_id = ANY (@workflow_ids::uuid[])
ORDER BY
    workflow_id,
    position,
    id;

-- name: ListHooksForWorkflow :many
SELECT
    event,
    script
FROM
    workflow_hooks
WHERE
    workflow_id = @workflow_id
ORDER BY
    position,
    id;

-- name: DeleteWorkflowHooks :exec
DELETE FROM workflow_hooks
WHERE workflow_id = @workflow_id;

-- name: InsertWorkflowHook :exec
INSERT INTO workflow_hooks(
    workflow_id,
    event,
    script,
    position)
VALUES (
    @workflow_id,
    @event,
    @script,
    @position);

-- name: ListWorkflowWorktreeDirs :many
SELECT DISTINCT
    provisioners.worktree_dir
FROM
    workflows
    JOIN workflow_provisioner_configs AS provisioners ON provisioners.id = workflows.provisioner_config_id
WHERE
    BTRIM(provisioners.worktree_dir) <> '';
