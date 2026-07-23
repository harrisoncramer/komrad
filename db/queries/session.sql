-- name: ListSessions :many
SELECT
    *
FROM
    sessions
WHERE
    is_deleted = 0
ORDER BY
    updated_at DESC;

-- name: ListAllSessions :many
SELECT
    *
FROM
    sessions
ORDER BY
    updated_at DESC;

-- name: ListSessionsByWorkflow :many
SELECT
    *
FROM
    sessions
WHERE
    is_deleted = 0
    AND workflow_id = @workflow_id
ORDER BY
    updated_at DESC;

-- name: ListAllSessionsByWorkflow :many
SELECT
    *
FROM
    sessions
WHERE
    workflow_id = @workflow_id
ORDER BY
    updated_at DESC;

-- name: GetSession :one
SELECT
    *
FROM
    sessions
WHERE
    name = @name
    AND is_deleted = 0
LIMIT 1;

-- name: ActiveNameExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            sessions
        WHERE
            name = @name
            AND is_deleted = 0);

-- name: UpsertSession :one
INSERT INTO sessions (id, name, cwd, flags, prompt, pane_id, state, claude_session_id, workflow_id)
    VALUES (@id, @name, @cwd, @flags, @prompt, @pane_id, @state, @claude_session_id, sqlc.narg('workflow_id'))
ON CONFLICT (name) WHERE is_deleted = 0
    DO UPDATE SET
        cwd = excluded.cwd,
        flags = excluded.flags,
        prompt = excluded.prompt,
        pane_id = excluded.pane_id,
        state = excluded.state,
        claude_session_id = CASE WHEN excluded.claude_session_id != '' THEN
            excluded.claude_session_id
        ELSE
            sessions.claude_session_id
        END,
        workflow_id = excluded.workflow_id,
        updated_at = CURRENT_TIMESTAMP
    RETURNING
        *;

-- name: SetSessionWorkflow :exec
UPDATE
    sessions
SET
    workflow_id = sqlc.narg('workflow_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: ClearWorkflowSessions :exec
UPDATE
    sessions
SET
    workflow_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE
    workflow_id = @workflow_id;

-- name: MarkWorkflowSessionsDeleted :exec
UPDATE
    sessions
SET
    is_deleted = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE
    workflow_id = @workflow_id
    AND is_deleted = 0;

-- name: UpdateSessionState :exec
UPDATE
    sessions
SET
    pane_id = @pane,
    state = @state,
    claude_session_id = CASE WHEN @claude_session_id != '' THEN
        @claude_session_id
    ELSE
        claude_session_id
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE
    name = @name
    AND is_deleted = 0;

-- name: SetSessionState :exec
UPDATE
    sessions
SET
    state = @state,
    updated_at = CURRENT_TIMESTAMP
WHERE
    name = @name
    AND is_deleted = 0;

-- name: SetSessionStateByID :exec
UPDATE
    sessions
SET
    state = @state,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE name = @name;

-- name: RenameSession :exec
UPDATE
    sessions
SET
    name = @name,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: MarkSessionDeleted :exec
UPDATE
    sessions
SET
    is_deleted = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE
    name = @name
    AND is_deleted = 0;

-- name: RestoreSession :exec
UPDATE
    sessions
SET
    is_deleted = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;
