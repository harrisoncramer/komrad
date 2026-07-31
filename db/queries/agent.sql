-- name: ListAgents :many
SELECT
    *
FROM
    agents s
WHERE
    s.deleted_at IS NULL
ORDER BY
    s.id DESC;

-- name: ListAgentsPage :many
SELECT
    *
FROM
    agents s
WHERE (@deleted::boolean) =(s.deleted_at IS NOT NULL)
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR s.id < sqlc.narg('cursor_id'))
ORDER BY
    s.id DESC
LIMIT @lim;

-- name: ListAllAgents :many
SELECT
    *
FROM
    agents s
ORDER BY
    s.id DESC;

-- name: GetAgent :one
SELECT
    *
FROM
    agents s
WHERE
    s.name = @name
    AND s.deleted_at IS NULL
LIMIT 1;

-- name: GetAgentByID :one
SELECT
    *
FROM
    agents s
WHERE
    s.id = @id
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
            AND deleted_at IS NULL);

-- name: AgentExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            agents
        WHERE
            id = @id);

-- name: InsertAgent :one
INSERT INTO agents(
    id,
    name,
    cwd,
    flags,
    prompt,
    pane_id,
    state,
    harness,
    session_id,
    workflow_id,
    schedule_id,
    idempotency_key,
    is_hidden)
VALUES (
    @id,
    @name,
    @cwd,
    @flags,
    @prompt,
    @pane_id,
    @state,
    @harness,
    @session_id,
    @workflow_id,
    sqlc.narg('schedule_id'),
    sqlc.narg('idempotency_key'),
    @is_hidden)
RETURNING
    id;

-- name: GetAgentByIdempotencyKey :one
SELECT
    *
FROM
    agents s
WHERE
    s.idempotency_key = @idempotency_key
LIMIT 1;

-- name: UpdateAgent :exec
UPDATE
    agents
SET
    cwd = @cwd,
    flags = @flags,
    prompt = @prompt,
    pane_id = @pane_id,
    state = @state,
    session_id = CASE WHEN @session_id != '' THEN
        @session_id
    ELSE
        session_id
    END,
    workflow_id = @workflow_id
WHERE
    id = @id;

-- name: MarkWorkflowAgentsDeleted :exec
UPDATE
    agents
SET
    deleted_at = NOW()
WHERE
    workflow_id = @workflow_id
    AND deleted_at IS NULL;

-- name: SetAgentStateByID :exec
UPDATE
    agents
SET
    state = @state
WHERE
    id = @id;

-- name: SetAgentSessionID :exec
UPDATE
    agents
SET
    session_id = @session_id
WHERE
    id = @id
    AND session_id = '';

-- name: ClearAgentPane :exec
UPDATE
    agents
SET
    pane_id = ''
WHERE
    id = @id;

-- name: SetAgentMark :many
UPDATE
    agents
SET
    mark = sqlc.narg(mark)
WHERE
    id = @id
    AND deleted_at IS NULL
RETURNING
    id;

-- name: SetAgentHidden :many
UPDATE
    agents
SET
    is_hidden = @hidden
WHERE
    id = @id
    AND deleted_at IS NULL
RETURNING
    id;

-- SetAgentMetadata records one fact against a live agent, replacing whatever
-- that key held before and leaving the agent's other keys alone. The insert
-- selects through a guard rather than naming the values outright so an id
-- belonging to no live agent writes nothing and is reported as such, instead of
-- raising a foreign key violation the handler would have to read to tell an
-- unknown agent from a real failure.
-- name: SetAgentMetadata :many
INSERT INTO agent_metadata(
    agent_id,
    KEY,
    value)
SELECT
    @agent_id,
    @key,
    @value
WHERE
    EXISTS (
        SELECT
            1
        FROM
            agents
        WHERE
            id = @agent_id
            AND deleted_at IS NULL)
ON CONFLICT (agent_id,
    KEY)
    DO UPDATE SET
        value = @value,
        updated_at = NOW()
    RETURNING
        agent_id;

-- name: DeleteAgentMetadata :many
DELETE FROM agent_metadata
WHERE agent_id = @agent_id
    AND KEY = @key
RETURNING
    agent_id;

-- ListAgentMetadata fetches the metadata for a whole page of agents at once, so
-- hydrating a listing costs one query rather than one per row.
-- name: ListAgentMetadata :many
SELECT
    agent_id,
    KEY,
    value
FROM
    agent_metadata
WHERE
    agent_id = ANY (@agent_ids::uuid[])
ORDER BY
    agent_id,
    KEY;

-- name: DeleteAgentByID :exec
DELETE FROM agents
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
    deleted_at = NOW()
WHERE
    name = @name
    AND deleted_at IS NULL;

-- name: RestoreAgent :exec
UPDATE
    agents
SET
    is_hidden = FALSE,
    deleted_at = NULL
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
INSERT INTO agent_transcripts(
    agent_id,
    content,
    synced_offset,
    synced_at)
VALUES (
    @agent_id,
    @content,
    @synced_offset,
    NOW())
ON CONFLICT (
    agent_id)
    DO UPDATE SET
        content = excluded.content,
        synced_offset = excluded.synced_offset,
        synced_at = NOW();

-- name: DeleteAgentTranscriptChunks :exec
DELETE FROM agent_transcript_chunks
WHERE agent_id = @agent_id;

-- NextTranscriptChunkSeq is the position the next appended turn takes, which is
-- one past the highest already stored, or zero for an agent with no turns yet.
-- name: NextTranscriptChunkSeq :one
SELECT
    COALESCE(MAX(seq) + 1, 0)::int AS next
FROM
    agent_transcript_chunks
WHERE
    agent_id = @agent_id;

-- InsertTranscriptChunks appends turns for one agent. Copy rather than insert, so
-- a sync that folded in several turns writes them in one stream instead of a round
-- trip each.
-- name: InsertTranscriptChunks :copyfrom
INSERT INTO agent_transcript_chunks(
    agent_id,
    seq,
    role,
    content,
    content_offset)
VALUES (
    @agent_id,
    @seq,
    @role,
    @content,
    @content_offset);

-- SearchTranscripts finds the turns matching the query, best match first. Two
-- matchers run together because they fail in opposite places. The tsquery matches
-- whole stemmed words, so it finds a turn that phrased something differently but
-- cannot match a word still being typed. The ILIKE matches any substring, so it
-- covers the half-typed word but knows nothing about word boundaries. Rank is the
-- cover density of the word match, which rewards the query's terms appearing close
-- together, and is zero for a turn only the substring found, sorting those last.
-- A word match takes its snippet from ts_headline, which marks the lexemes it
-- matched rather than a literal run, so a stemmed hit is still highlighted. A
-- substring-only match has no lexemes to mark and takes a plain window around the
-- literal occurrence, which the caller marks instead.
-- name: SearchTranscripts :many
WITH q AS (
    SELECT
        WEBSEARCH_TO_TSQUERY('english', @query) AS query
)
SELECT
    c.agent_id,
    c.seq,
    c.role,
    c.content_offset,
    a.name,
(a.deleted_at IS NOT NULL)::boolean AS deleted,
    CASE WHEN c.tsv @@ q.query THEN
        TS_HEADLINE('english', c.content, q.query, @headline)
    ELSE
        SUBSTRING(c.content FROM greatest(1, POSITION(LOWER(@query) IN LOWER(c.content)) - @lead::int)
            FOR @width::int)
    END::text AS snippet,
    TS_RANK_CD(c.tsv, q.query)::float8 AS rank
FROM
    agent_transcript_chunks c
    CROSS JOIN q
    JOIN agents a ON a.id = c.agent_id
WHERE (c.tsv @@ q.query
    OR c.content ILIKE @pattern)
AND (sqlc.narg('cursor_rank')::float8 IS NULL
    OR TS_RANK_CD(c.tsv, q.query) < sqlc.narg('cursor_rank')
    OR (TS_RANK_CD(c.tsv, q.query) = sqlc.narg('cursor_rank')
        AND (c.agent_id,
            c.seq) >(sqlc.narg('cursor_agent_id')::uuid,
            sqlc.narg('cursor_seq')::int)))
ORDER BY
    rank DESC,
    c.agent_id ASC,
    c.seq ASC
LIMIT @lim;

-- CountLiveScheduleAgents counts the agents of a schedule that are still working
-- on something, which is what tells a firing the last run has not finished.
-- Thinking and waiting are the two states that say so: one is generating, the
-- other is blocked on an answer somebody still owes it.
--
-- An idle run is not counted, whether it has finished or never got going. Idle is
-- the state a launch that died on the spot keeps for good, since the reconciler
-- only ever promotes an agent it can see working, and counting it would turn one
-- failed launch into a schedule that never fires again. A run that really is
-- starting is promoted within a reconcile interval, which is far inside the
-- shortest interval a cron expression can name.
-- name: CountLiveScheduleAgents :one
SELECT
    COUNT(*)
FROM
    agents
WHERE
    schedule_id = @schedule_id
    AND deleted_at IS NULL
    AND state IN ('thinking', 'waiting');
