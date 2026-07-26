-- name: ListSessions :many
SELECT
    s.*,
    sc.cwd,
    sc.flags
FROM
    sessions s
    JOIN session_configs sc ON sc.id = s.session_config_id
WHERE
    s.is_deleted = FALSE
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
    s.is_deleted = FALSE
    AND (sqlc.narg ('cursor_updated_at')::timestamptz IS NULL
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
    AND s.is_deleted = FALSE
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
            AND is_deleted = FALSE);

-- name: SessionExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            sessions
        WHERE
            id = @id);

-- name: InsertSessionConfig :one
INSERT INTO session_configs (cwd, flags)
    VALUES (@cwd, @flags)
RETURNING
    id;

-- name: InsertSession :one
INSERT INTO sessions (name, session_config_id, prompt, pane_id, state, claude_session_id, workflow_id)
    VALUES (@name, @session_config_id, @prompt, @pane_id, @state, @claude_session_id, sqlc.narg ('workflow_id'))
RETURNING
    id;

-- name: UpdateSessionConfig :exec
UPDATE
    session_configs
SET
    cwd = @cwd,
    flags = @flags
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
    workflow_id = sqlc.narg ('workflow_id')
WHERE
    id = @id;

-- name: SetSessionWorkflow :exec
UPDATE
    sessions
SET
    workflow_id = sqlc.narg ('workflow_id')
WHERE
    id = @id;

-- name: ClearWorkflowSessions :exec
UPDATE
    sessions
SET
    workflow_id = NULL
WHERE
    workflow_id = @workflow_id;

-- name: MarkWorkflowSessionsDeleted :exec
UPDATE
    sessions
SET
    is_deleted = TRUE
WHERE
    workflow_id = @workflow_id
    AND is_deleted = FALSE;

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
    END
WHERE
    name = @name
    AND is_deleted = FALSE;

-- SetSessionState returns the row it touched so the caller can tell a chat that
-- does not exist from one that already held the state.
-- name: SetSessionState :many
UPDATE
    sessions
SET
    state = @state
WHERE
    name = @name
    AND is_deleted = FALSE
RETURNING
    id;

-- name: SetSessionStateByID :exec
UPDATE
    sessions
SET
    state = @state
WHERE
    id = @id;

-- GetSessionForDelete resolves the two ids a hard delete has to remove rows by.
-- name: GetSessionForDelete :one
SELECT
    id,
    session_config_id
FROM
    sessions
WHERE
    name = @name;

-- name: DeleteSessionByID :exec
DELETE FROM sessions
WHERE id = @id;

-- name: DeleteSessionConfig :exec
DELETE FROM session_configs
WHERE id = @id;

-- name: DeleteSessionTranscript :exec
DELETE FROM session_transcripts
WHERE session_id = @session_id;

-- RenameSession returns the row it touched so the caller can tell an id that
-- matches nothing from a rename to the name the chat already had.
-- name: RenameSession :many
UPDATE
    sessions
SET
    name = @name
WHERE
    id = @id
RETURNING
    id;

-- name: MarkSessionDeleted :exec
UPDATE
    sessions
SET
    is_deleted = TRUE
WHERE
    name = @name
    AND is_deleted = FALSE;

-- name: RestoreSession :exec
UPDATE
    sessions
SET
    is_deleted = FALSE
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
    VALUES (@session_id, @content, @synced_offset, now())
ON CONFLICT (session_id)
    DO UPDATE SET
        content = excluded.content,
        synced_offset = excluded.synced_offset,
        synced_at = now();

-- SearchTranscripts finds the transcripts containing the query as a substring,
-- best match first. The ILIKE is the form the trigram index can answer, so the
-- pattern arrives already escaped and wrapped in wildcards, while the raw query
-- is passed alongside it for the position and the count, which have to match
-- literally. Rank is how many times the query occurs in a transcript, so a chat
-- that discussed something at length outranks one that mentioned it once. The
-- snippet is a plain window of the text around the first occurrence; the caller
-- marks the match within it.
-- name: SearchTranscripts :many
WITH matches AS (
    SELECT
        t.session_id,
        substring(t.content FROM greatest (1, position(lower(@query) IN lower(t.content)) - @lead::int)
            FOR @width::int)::text AS snippet,
        (length(t.content) - length(replace(lower(t.content), lower(@query), ''))) / length(@query) AS rank
    FROM
        session_transcripts t
    WHERE
        t.content ILIKE @pattern
)
SELECT
    matches.session_id,
    s.name,
    s.is_deleted,
    matches.snippet,
    matches.rank
FROM
    matches
    JOIN sessions s ON s.id = matches.session_id
WHERE (sqlc.narg ('cursor_rank')::int IS NULL
    OR rank < sqlc.narg ('cursor_rank')
    OR (rank = sqlc.narg ('cursor_rank')
        AND session_id > sqlc.narg ('cursor_session_id')))
ORDER BY
    matches.rank DESC,
    matches.session_id ASC
LIMIT @lim;
