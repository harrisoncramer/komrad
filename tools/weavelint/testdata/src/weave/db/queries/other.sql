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

-- name: ListAgentRows :many
WITH marked AS (
    SELECT
        agent_id
    FROM
        marks
)
SELECT
    a.id
FROM
    agents a
    LEFT JOIN marked k ON k.agent_id = a.id
    LEFT JOIN (
        SELECT
            1) x ON TRUE
WHERE
    EXISTS (
        SELECT
            1
        FROM
            marks m
        WHERE
            m.agent_id = a.id);

-- name: ListWorkflowRows :many
SELECT
    w.id
FROM
    workflows w
    JOIN workflow_hooks h ON h.workflow_id = w.id
    LEFT JOIN marks k ON k.workflow_id = w.id;
