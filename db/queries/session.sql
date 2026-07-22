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
INSERT INTO sessions (id, name, cwd, flags, prompt, pane_id, state, claude_session_id)
    VALUES (@id, @name, @cwd, @flags, @prompt, @pane_id, @state, @claude_session_id)
ON CONFLICT (name)
    DO UPDATE SET
        cwd = excluded.cwd,
        flags = excluded.flags,
        prompt = excluded.prompt,
        pane_id = excluded.pane_id,
        state = excluded.state,
        claude_session_id = CASE WHEN excluded.claude_session_id != '' THEN excluded.claude_session_id ELSE sessions.claude_session_id END,
        updated_at = CURRENT_TIMESTAMP
    RETURNING
        *;

-- name: UpsertSessionState :exec
INSERT INTO sessions (id, name, pane_id, state, claude_session_id)
    VALUES (@id, @name, @pane, @state, @claude_session_id)
ON CONFLICT (name)
    DO UPDATE SET
        pane_id = excluded.pane_id,
        state = excluded.state,
        claude_session_id = CASE WHEN excluded.claude_session_id != '' THEN excluded.claude_session_id ELSE sessions.claude_session_id END,
        updated_at = CURRENT_TIMESTAMP;

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

