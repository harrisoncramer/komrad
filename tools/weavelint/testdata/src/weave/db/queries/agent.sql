-- name: GetAgent :one
SELECT
    *
FROM
    agents
WHERE
    id = @id
FOR UPDATE;

-- name: SetAgentStateByID :exec
UPDATE
    agents
SET
    state = @state
WHERE
    id = @id;

-- name: MarkAgentsArchived :many
WITH archived AS (
    UPDATE
        agents a
    SET
        archived_at = now()
    WHERE
        a.id = ANY (@ids::uuid[])
    RETURNING
        a.id)
DELETE FROM marks
WHERE
    agent_id IN (
        SELECT
            id
        FROM
            archived)
RETURNING
    agent_id;

-- name: LockAgentMetadata :exec
SELECT
    1
FROM
    agents
WHERE
    id = @id
FOR UPDATE;
