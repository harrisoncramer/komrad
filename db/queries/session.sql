-- name: ListSessions :many
SELECT
    *
FROM
    sessions
ORDER BY
    updated_at DESC;

-- name: GetSession :one
SELECT
    *
FROM
    sessions
WHERE
    name = @name
LIMIT 1;

-- name: UpsertSession :one
INSERT INTO sessions (id, name, cwd, flags, prompt, pane_id, state)
    VALUES (@id, @name, @cwd, @flags, @prompt, @pane_id, @state)
ON CONFLICT (name)
    DO UPDATE SET
        cwd = excluded.cwd,
        flags = excluded.flags,
        prompt = excluded.prompt,
        pane_id = excluded.pane_id,
        state = excluded.state,
        updated_at = CURRENT_TIMESTAMP
    RETURNING
        *;

-- name: SetSessionState :exec
UPDATE
    sessions
SET
    state = @state,
    updated_at = CURRENT_TIMESTAMP
WHERE
    name = @name;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE name = @name;

