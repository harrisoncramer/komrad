-- name: ListAgents :many
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    agents s
    JOIN agent_configs sc ON sc.id = s.agent_config_id
WHERE
    s.is_deleted = FALSE
ORDER BY
    s.updated_at DESC;

-- name: ListAgentsPage :many
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    agents s
    JOIN agent_configs sc ON sc.id = s.agent_config_id
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

-- name: ListAllAgents :many
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    agents s
    JOIN agent_configs sc ON sc.id = s.agent_config_id
ORDER BY
    s.updated_at DESC;

-- name: GetAgent :one
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    agents s
    JOIN agent_configs sc ON sc.id = s.agent_config_id
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
            agents
        WHERE
            name = @name
            AND is_deleted = FALSE);

-- name: AgentExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            agents
        WHERE
            id = @id);

-- name: InsertAgentConfig :one
INSERT INTO agent_configs (cwd, flags)
    VALUES (@cwd, @flags)
RETURNING
    id;

-- name: InsertAgent :one
INSERT INTO agents (name, agent_config_id, prompt, pane_id, state, claude_session_id, workflow_id, schedule_id, idempotency_key)
    VALUES (@name, @agent_config_id, @prompt, @pane_id, @state, @claude_session_id, @workflow_id, sqlc.narg ('schedule_id'), sqlc.narg ('idempotency_key'))
RETURNING
    id;

-- name: GetAgentByIdempotencyKey :one
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    agents s
    JOIN agent_configs sc ON sc.id = s.agent_config_id
WHERE
    s.idempotency_key = @idempotency_key
LIMIT 1;

-- name: UpdateAgentConfig :exec
UPDATE
    agent_configs
SET
    cwd = @cwd,
    flags = @flags
WHERE
    id = @id;

-- name: UpdateAgent :exec
UPDATE
    agents
SET
    prompt = @prompt,
    pane_id = @pane_id,
    state = @state,
    claude_session_id = CASE WHEN @claude_session_id != '' THEN
        @claude_session_id
    ELSE
        claude_session_id
    END,
    workflow_id = @workflow_id
WHERE
    id = @id;

-- name: SetAgentWorkflow :exec
UPDATE
    agents
SET
    workflow_id = @workflow_id
WHERE
    id = @id;

-- name: MarkWorkflowAgentsDeleted :exec
UPDATE
    agents
SET
    is_deleted = TRUE
WHERE
    workflow_id = @workflow_id
    AND is_deleted = FALSE;

-- SetAgentState returns the row it touched so the caller can tell an agent that
-- does not exist from one that already held the state.
-- name: SetAgentState :many
UPDATE
    agents
SET
    state = @state
WHERE
    name = @name
    AND is_deleted = FALSE
RETURNING
    id;

-- name: SetAgentStateByID :exec
UPDATE
    agents
SET
    state = @state
WHERE
    id = @id;

-- GetAgentForDelete resolves the two ids a hard delete has to remove rows by.
-- name: GetAgentForDelete :one
SELECT
    id,
    agent_config_id
FROM
    agents
WHERE
    name = @name;

-- name: DeleteAgentByID :exec
DELETE FROM agents
WHERE id = @id;

-- name: DeleteAgentConfig :exec
DELETE FROM agent_configs
WHERE id = @id;

-- name: DeleteAgentTranscript :exec
DELETE FROM agent_transcripts
WHERE agent_id = @agent_id;

-- RenameAgent returns the row it touched so the caller can tell an id that
-- matches nothing from a rename to the name the agent already had.
-- name: RenameAgent :many
UPDATE
    agents
SET
    name = @name
WHERE
    id = @id
RETURNING
    id;

-- name: MarkAgentDeleted :exec
UPDATE
    agents
SET
    is_deleted = TRUE
WHERE
    name = @name
    AND is_deleted = FALSE;

-- name: RestoreAgent :exec
UPDATE
    agents
SET
    is_deleted = FALSE
WHERE
    id = @id;

-- name: GetTranscript :one
SELECT
    content,
    synced_offset
FROM
    agent_transcripts
WHERE
    agent_id = @agent_id;

-- name: UpsertTranscript :exec
INSERT INTO agent_transcripts (agent_id, content, synced_offset, synced_at)
    VALUES (@agent_id, @content, @synced_offset, now())
ON CONFLICT (agent_id)
    DO UPDATE SET
        content = excluded.content,
        synced_offset = excluded.synced_offset,
        synced_at = now();

-- SearchTranscripts finds the transcripts containing the query as a substring,
-- best match first. The ILIKE is the form the trigram index can answer, so the
-- pattern arrives already escaped and wrapped in wildcards, while the raw query
-- is passed alongside it for the position and the count, which have to match
-- literally. Rank is how many times the query occurs in a transcript, so an agent
-- that discussed something at length outranks one that mentioned it once. The
-- snippet is a plain window of the text around the first occurrence; the caller
-- marks the match within it.
-- name: SearchTranscripts :many
WITH matches AS (
    SELECT
        t.agent_id,
        substring(t.content FROM greatest (1, position(lower(@query) IN lower(t.content)) - @lead::int)
            FOR @width::int)::text AS snippet,
        (length(t.content) - length(replace(lower(t.content), lower(@query), ''))) / length(@query) AS rank
    FROM
        agent_transcripts t
    WHERE
        t.content ILIKE @pattern
)
SELECT
    matches.agent_id,
    s.name,
    s.is_deleted,
    matches.snippet,
    matches.rank
FROM
    matches
    JOIN agents s ON s.id = matches.agent_id
WHERE (sqlc.narg ('cursor_rank')::int IS NULL
    OR rank < sqlc.narg ('cursor_rank')
    OR (rank = sqlc.narg ('cursor_rank')
        AND agent_id > sqlc.narg ('cursor_agent_id')))
ORDER BY
    matches.rank DESC,
    matches.agent_id ASC
LIMIT @lim;

-- name: CountLiveScheduleAgents :one
SELECT
    count(*)
FROM
    agents
WHERE
    schedule_id = @schedule_id
    AND is_deleted = FALSE
    AND state <> 'done';
