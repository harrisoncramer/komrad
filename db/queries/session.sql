-- name: ListSessions :many
SELECT
    s.*,
    sc.cwd,
    sc.flags
FROM
    sessions s
    JOIN session_configs sc ON sc.id = s.session_config_id
WHERE
    s.is_deleted = 0
ORDER BY
    s.updated_at DESC;

-- name: ListSessionsPage :many
SELECT
    s.*,
    sc.cwd,
    sc.flags
FROM
    sessions s
    JOIN session_configs sc ON sc.id = s.session_config_id
WHERE
    s.is_deleted = 0
    AND (sqlc.narg ('cursor_updated_at') IS NULL
        OR s.updated_at < sqlc.narg ('cursor_updated_at')
        OR (s.updated_at = sqlc.narg ('cursor_updated_at')
            AND s.id < sqlc.narg ('cursor_id')))
ORDER BY
    s.updated_at DESC,
    s.id DESC
LIMIT @lim;

-- name: ListAllSessions :many
SELECT
    s.*,
    sc.cwd,
    sc.flags
FROM
    sessions s
    JOIN session_configs sc ON sc.id = s.session_config_id
ORDER BY
    s.updated_at DESC;

-- name: GetSession :one
SELECT
    s.*,
    sc.cwd,
    sc.flags
FROM
    sessions s
    JOIN session_configs sc ON sc.id = s.session_config_id
WHERE
    s.name = @name
    AND s.is_deleted = 0
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

-- name: InsertSessionConfig :exec
INSERT INTO session_configs (id, cwd, flags)
    VALUES (@id, @cwd, @flags);

-- name: InsertSession :exec
INSERT INTO sessions (id, name, session_config_id, prompt, pane_id, state, claude_session_id, workflow_id)
    VALUES (@id, @name, @session_config_id, @prompt, @pane_id, @state, @claude_session_id, sqlc.narg ('workflow_id'));

-- name: UpdateSessionConfig :exec
UPDATE
    session_configs
SET
    cwd = @cwd,
    flags = @flags,
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: UpdateSession :exec
UPDATE
    sessions
SET
    prompt = @prompt,
    pane_id = @pane_id,
    state = @state,
    claude_session_id = CASE WHEN @claude_session_id != '' THEN
        @claude_session_id
    ELSE
        claude_session_id
    END,
    workflow_id = sqlc.narg ('workflow_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = @id;

-- name: SetSessionWorkflow :exec
UPDATE
    sessions
SET
    workflow_id = sqlc.narg ('workflow_id'),
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

-- name: GetTranscript :one
SELECT
    content,
    synced_offset
FROM
    session_transcripts
WHERE
    session_id = @session_id;

-- name: UpsertTranscript :exec
INSERT INTO session_transcripts (session_id, content, synced_offset, synced_at)
    VALUES (@session_id, @content, @synced_offset, CURRENT_TIMESTAMP)
ON CONFLICT (session_id)
    DO UPDATE SET
        content = excluded.content,
        synced_offset = excluded.synced_offset,
        synced_at = CURRENT_TIMESTAMP;

