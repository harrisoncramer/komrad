-- name: DeleteMarkForTarget :execrows
DELETE FROM marks
WHERE
    agent_id = @id;

-- name: UpsertWorktreeSize :exec
INSERT INTO worktree_sizes(dir, bytes)
    VALUES (@dir, 0)
ON CONFLICT (dir)
    DO UPDATE SET
        bytes = 0;

-- name: InsertWorkflowHook :exec
INSERT INTO workflow_hooks(workflow_id)
    VALUES (@id);
