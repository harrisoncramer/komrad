-- name: ListWorkflowPageIDs :many
SELECT
    w.id
FROM
    workflows w
WHERE (@archived::boolean) =(w.archived_at IS NOT NULL)
    AND w.deleting_at IS NULL
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

-- name: ListWatchedWorkflows :many
SELECT
    sqlc.embed(w),
    sqlc.embed(wc),
    sqlc.embed(pc)
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
WHERE
    w.deleting_at IS NULL
    AND (sqlc.narg('keys')::text[] IS NULL
        OR w.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    LOWER(w.name)
    COLLATE "C" ASC,
    w.id ASC;

-- name: GetWorkflow :one
SELECT
    sqlc.embed(w),
    sqlc.embed(wc),
    sqlc.embed(pc)
FROM
    workflows w
    JOIN workflow_configs wc ON wc.id = w.workflow_config_id
    JOIN workflow_provisioner_configs pc ON pc.id = w.provisioner_config_id
WHERE
    w.id = @id
    AND (@include_archived::boolean
        OR w.archived_at IS NULL)
LIMIT 1;

-- name: InsertWorkflowConfig :one
INSERT INTO workflow_configs(
    harness,
    cwd,
    flags,
    reviewer_harness,
    reviewer_flags)
VALUES (
    @harness,
    @cwd,
    @flags,
    @reviewer_harness,
    @reviewer_flags)
RETURNING
    *;

-- name: InsertWorkflowProvisionerConfig :one
INSERT INTO workflow_provisioner_configs(
    worktree_enabled,
    worktree_base_branch,
    worktree_dir,
    worktree_copy_paths,
    worktree_symlink_paths,
    worktree_diff_exclude)
VALUES (
    @worktree_enabled,
    @worktree_base_branch,
    @worktree_dir,
    @worktree_copy_paths,
    @worktree_symlink_paths,
    @worktree_diff_exclude)
RETURNING
    *;

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
    worktree_diff_exclude = @worktree_diff_exclude
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
    flags = @flags,
    reviewer_harness = @reviewer_harness,
    reviewer_flags = @reviewer_flags
WHERE
    id =(
        SELECT
            workflow_config_id
        FROM
            workflows
        WHERE
            workflows.id = @workflow_id);

-- name: MarkWorkflowArchived :exec
WITH prior AS (
    SELECT
        workflows.id
    FROM
        workflows
    WHERE
        workflows.id = @id
        AND workflows.archived_at IS NULL
    FOR UPDATE)
UPDATE
    workflows w
SET
    archived_at = NOW()
FROM
    prior
WHERE
    w.id = prior.id;

-- name: MarkWorkflowRestored :exec
UPDATE
    workflows
SET
    archived_at = NULL
WHERE
    id = @id;

-- name: ClearAbandonedWorkflowsDeleting :exec
UPDATE
    workflows w
SET
    deleting_at = NULL
WHERE
    w.deleting_at IS NOT NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.workflow_id = w.id
            AND a.deleting_at IS NOT NULL);

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

-- name: ListEventPromptsForWorkflows :many
SELECT
    workflow_id,
    event,
    prompt
FROM
    workflow_event_prompts
WHERE
    workflow_id = ANY (@workflow_ids::uuid[])
ORDER BY
    workflow_id,
    position;

-- name: ListEventPromptsForWorkflow :many
SELECT
    event,
    prompt
FROM
    workflow_event_prompts
WHERE
    workflow_id = @workflow_id
ORDER BY
    position;

-- name: DeleteWorkflowEventPrompts :exec
DELETE FROM workflow_event_prompts
WHERE workflow_id = @workflow_id;

-- name: InsertWorkflowPrompt :exec
INSERT INTO workflow_event_prompts(
    workflow_id,
    event,
    prompt,
    position)
VALUES (
    @workflow_id,
    @event,
    @prompt,
    @position);
