-- name: ListMarks :many
SELECT
    mark,
    agent_id,
    workflow_id
FROM
    marks
ORDER BY
    mark;

-- name: ClearMarkConflicts :exec
WITH target AS (
    SELECT
        sqlc.narg(agent_id)::uuid AS agent_id,
        sqlc.narg(workflow_id)::uuid AS workflow_id
),
live AS (
    SELECT
        1
    FROM
        target
    WHERE
        EXISTS (
            SELECT
                1
            FROM
                agents
            WHERE
                agents.id = target.agent_id
                AND agents.archived_at IS NULL)
        OR EXISTS (
            SELECT
                1
            FROM
                workflows
            WHERE
                workflows.id = target.workflow_id
                AND workflows.archived_at IS NULL))
DELETE FROM marks
USING target
WHERE EXISTS (
        SELECT
            1
        FROM
            live)
    AND (marks.mark = @mark
        OR marks.agent_id = target.agent_id
        OR marks.workflow_id = target.workflow_id);

-- name: DeleteMarkForTarget :execrows
DELETE FROM marks
WHERE marks.agent_id = sqlc.narg(agent_id)
    OR marks.workflow_id = sqlc.narg(workflow_id);

-- name: InsertAgentMark :execrows
INSERT INTO marks(mark, agent_id)
SELECT
    @mark,
    agents.id
FROM
    agents
WHERE
    agents.id = @agent_id
    AND agents.archived_at IS NULL;

-- name: InsertWorkflowMark :execrows
INSERT INTO marks(mark, workflow_id)
SELECT
    @mark,
    workflows.id
FROM
    workflows
WHERE
    workflows.id = @workflow_id
    AND workflows.archived_at IS NULL;
