-- name: ListAgents :many
SELECT
    *
FROM
    agents s
WHERE
    s.archived_at IS NULL
ORDER BY
    s.id DESC;

-- name: ListAgentRows :many
WITH failed AS (
    SELECT DISTINCT ON (r.agent_id)
        r.agent_id,
        r.log_number
    FROM
        agent_runs r
    WHERE
        r.failure <> ''
        AND r.viewed_at IS NULL
    ORDER BY
        r.agent_id,
        r.started_at DESC,
        r.id DESC
),
tallied AS (
    SELECT
        v.agent_id,
        COUNT(DISTINCT v.round_id)::int AS rounds
    FROM
        reviews v
    WHERE
        v.released_at IS NOT NULL
    GROUP BY
        v.agent_id
),
concluded AS (
    SELECT DISTINCT ON (v.agent_id)
        v.agent_id,
        v.verdict,
        n.snapshot_sha
    FROM
        reviews v
        JOIN review_rounds n ON n.id = v.round_id
    WHERE
        v.released_at IS NOT NULL
        AND v.verdict IN ('approved', 'changes_requested')
    ORDER BY
        v.agent_id,
        n.ordinal DESC,
        v.released_at DESC
),
opened AS (
    SELECT DISTINCT ON (a.id)
        a.id AS agent_id,
        p.number,
        p.url,
        p.title,
        p.draft,
        p.merged_at,
        p.closed_at,
        p.review_decision,
        p.checks,
        p.mergeability
    FROM
        agents a
        JOIN pull_requests p ON p.repository_url = a.repository_url
            AND p.head_repository_url = a.repository_url
            AND p.head_branch = a.branch
        JOIN workflows w ON w.id = a.workflow_id
        LEFT JOIN workflow_provisioner_configs c ON c.id = w.provisioner_config_id
    WHERE
        c.worktree_base_branch IS NULL
        OR a.branch <> c.worktree_base_branch
    ORDER BY
        a.id,
        COALESCE(p.merged_at, p.closed_at) IS NOT NULL,
        p.opened_at DESC
)
SELECT
    sqlc.embed(s),
    EXISTS (
        SELECT
            1
        FROM
            mail m
        WHERE
            m.to_agent_id = s.id
            AND m.claimed_at IS NULL
            AND m.expires_at > NOW())::boolean AS unread_mail,
(f.agent_id IS NOT NULL)::boolean AS failed_run,
        f.log_number AS failed_run_log,
        k.mark,
        COALESCE(t.rounds, 0)::int AS local_rounds,
        c.verdict AS local_verdict,
        c.snapshot_sha AS local_snapshot_sha,
        p.number AS pull_request_number,
        p.url AS pull_request_url,
        p.title AS pull_request_title,
        p.draft AS pull_request_draft,
        p.merged_at AS pull_request_merged_at,
        p.closed_at AS pull_request_closed_at,
        p.review_decision AS pull_request_review_decision,
        p.checks AS pull_request_checks,
        p.mergeability AS pull_request_mergeability,
        EXISTS (
            SELECT
                1
            FROM
                pull_request_syncs y
            WHERE
                y.repository_url = s.repository_url
                AND y.updated_through IS NOT NULL)::boolean AS repository_read
        FROM
            agents s
        LEFT JOIN failed f ON f.agent_id = s.id
        LEFT JOIN marks k ON k.agent_id = s.id
        LEFT JOIN tallied t ON t.agent_id = s.id
        LEFT JOIN concluded c ON c.agent_id = s.id
        LEFT JOIN opened p ON p.agent_id = s.id
    WHERE (sqlc.narg('ids')::uuid[] IS NULL
        OR s.id = ANY (sqlc.narg('ids')::uuid[]))
    AND (sqlc.narg('ids')::uuid[] IS NOT NULL
        OR ((@archived::boolean) =(s.archived_at IS NOT NULL)
            AND s.deleting_at IS NULL
            AND (sqlc.narg('cursor_id')::uuid IS NULL
                OR s.id < sqlc.narg('cursor_id'))))
ORDER BY
    s.id DESC
LIMIT @lim;

-- name: ListArchiveSweepPage :many
SELECT
    *
FROM
    agents s
WHERE
    s.archived_at IS NOT NULL
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR s.id < sqlc.narg('cursor_id'))
ORDER BY
    s.id DESC
LIMIT @lim;

-- name: MarkAgentsDeleting :exec
WITH claimed AS (
    UPDATE
        agents
    SET
        deleting_at = NOW()
    WHERE (id = ANY (@ids::uuid[])
        OR parent_agent_id = ANY (@ids::uuid[]))
    AND deleting_at IS NULL
RETURNING
    id,
    workflow_id)
UPDATE
    workflows w
SET
    deleting_at = NOW()
WHERE
    w.archived_at IS NOT NULL
    AND w.deleting_at IS NULL
    AND w.id IN (
        SELECT
            workflow_id
        FROM
            claimed)
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.workflow_id = w.id
            AND a.archived_at IS NOT NULL
            AND a.deleting_at IS NULL
            AND a.id NOT IN (
                SELECT
                    id
                FROM
                    claimed));

-- name: MarkArchivedAgentsDeleting :exec
WITH claimed AS (
    UPDATE
        agents a
    SET
        deleting_at = NOW()
    WHERE
        a.archived_at IS NOT NULL
        AND a.state <> 'failed_to_cleanup'
        AND a.deleting_at IS NULL
        AND NOT EXISTS (
            SELECT
                1
            FROM
                agents parent
            WHERE
                parent.id = a.parent_agent_id
                AND parent.state = 'failed_to_cleanup')
        RETURNING
            a.id,
            a.workflow_id)
UPDATE
    workflows w
SET
    deleting_at = NOW()
WHERE
    w.archived_at IS NOT NULL
    AND w.deleting_at IS NULL
    AND w.id IN (
        SELECT
            workflow_id
        FROM
            claimed)
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.workflow_id = w.id
            AND a.archived_at IS NOT NULL
            AND a.deleting_at IS NULL
            AND a.id NOT IN (
                SELECT
                    id
                FROM
                    claimed));

-- name: MarkArchivedAgentsDeletingByID :exec
WITH claimed AS (
    UPDATE
        agents a
    SET
        deleting_at = NOW()
    WHERE (a.id = ANY (@ids::uuid[])
        OR a.parent_agent_id = ANY (@ids::uuid[]))
    AND a.archived_at IS NOT NULL
    AND a.state <> 'failed_to_cleanup'
    AND a.deleting_at IS NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents parent
        WHERE
            parent.id = a.parent_agent_id
            AND parent.state = 'failed_to_cleanup')
    RETURNING
        a.id,
        a.workflow_id)
UPDATE
    workflows w
SET
    deleting_at = NOW()
WHERE
    w.archived_at IS NOT NULL
    AND w.deleting_at IS NULL
    AND w.id IN (
        SELECT
            workflow_id
        FROM
            claimed)
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.workflow_id = w.id
            AND a.archived_at IS NOT NULL
            AND a.deleting_at IS NULL
            AND a.id NOT IN (
                SELECT
                    id
                FROM
                    claimed));

-- name: ClearAgentsDeleting :exec
WITH released AS (
    UPDATE
        agents
    SET
        deleting_at = NULL
    WHERE (id = ANY (@ids::uuid[])
        OR parent_agent_id = ANY (@ids::uuid[]))
    AND deleting_at IS NOT NULL
RETURNING
    workflow_id)
UPDATE
    workflows
SET
    deleting_at = NULL
WHERE
    deleting_at IS NOT NULL
    AND id IN (
        SELECT
            workflow_id
        FROM
            released);

-- name: ClearAllAgentsDeleting :exec
UPDATE
    agents
SET
    deleting_at = NULL
WHERE
    deleting_at IS NOT NULL;

-- name: ListAllAgents :many
SELECT
    *
FROM
    agents s
ORDER BY
    s.id DESC;

-- name: ListAgentsForTranscriptSync :many
SELECT
    *
FROM
    agents s
WHERE
    s.archived_at IS NULL
    OR (s.session_id <> ''
        AND NOT EXISTS (
            SELECT
                1
            FROM
                agent_transcript_sessions t
            WHERE
                t.agent_id = s.id
                AND t.session_id = s.session_id
                AND t.synced_at >= s.archived_at))
ORDER BY
    s.id DESC;

-- name: GetAgentInWorkflow :one
SELECT
    *
FROM
    agents s
WHERE
    s.workflow_id = @workflow_id
    AND s.name = @name
    AND s.archived_at IS NULL
LIMIT 1;

-- name: ListLiveAgentsNamed :many
SELECT
    *
FROM
    agents s
WHERE
    s.name = @name
    AND s.archived_at IS NULL
ORDER BY
    s.id DESC;

-- name: GetAgentByID :one
SELECT
    *
FROM
    agents s
WHERE
    s.id = @id
LIMIT 1;

-- name: ListAgentsByID :many
SELECT
    *
FROM
    agents s
WHERE
    s.id = ANY (@ids::uuid[])
ORDER BY
    s.id DESC;

-- name: ActiveNameExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            agents
        WHERE
            workflow_id = @workflow_id
            AND name = @name
            AND archived_at IS NULL);

-- name: AgentExists :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            agents
        WHERE
            id = @id);

-- name: CountLiveAgents :one
SELECT
    COUNT(*)
FROM
    agents
WHERE
    archived_at IS NULL
    AND state NOT IN ('failed_to_create', 'failed_to_cleanup');

-- name: LockAgentSlots :exec
SELECT
    PG_ADVISORY_XACT_LOCK(@key::bigint);

-- name: LockAgentMetadata :exec
SELECT
    PG_ADVISORY_XACT_LOCK(HASHTEXTEXTENDED(@agent_id::text, 0));

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
    is_hidden,
    provision_enabled,
    spawned_by_agent_id,
    parent_agent_id,
    adopted_branch,
    role)
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
    @is_hidden,
    sqlc.narg('provision_enabled'),
    sqlc.narg('spawned_by_agent_id'),
    sqlc.narg('parent_agent_id'),
    @adopted_branch,
    @role)
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

-- name: ReleaseAgentIdempotencyKey :many
UPDATE
    agents
SET
    idempotency_key = NULL
WHERE
    idempotency_key = @idempotency_key
    AND state = 'failed_to_create'
RETURNING
    id;

-- name: UpdateAgent :exec
UPDATE
    agents
SET
    cwd = @cwd,
    flags = @flags,
    prompt = @prompt,
    pane_id = @pane_id,
    state = @state,
    state_changed_at = CASE WHEN state IS DISTINCT FROM @state THEN
        NOW()
    ELSE
        state_changed_at
    END,
    session_id = CASE WHEN @session_id != '' THEN
        @session_id
    ELSE
        session_id
    END,
    workflow_id = @workflow_id
WHERE
    id = @id;

-- name: UpdateAgentIfLive :many
UPDATE
    agents
SET
    cwd = @cwd,
    flags = @flags,
    prompt = @prompt,
    pane_id = @pane_id,
    state = @state,
    state_changed_at = CASE WHEN state IS DISTINCT FROM @state THEN
        NOW()
    ELSE
        state_changed_at
    END,
    session_id = CASE WHEN @session_id != '' THEN
        @session_id
    ELSE
        session_id
    END,
    workflow_id = @workflow_id
WHERE
    id = @id
    AND archived_at IS NULL
RETURNING
    id;

-- name: MarkWorkflowAgentsArchived :many
WITH prior AS (
    SELECT
        id,
        pane_id,
        state IN ('thinking', 'waiting', 'uncertain', 'setting_up') AS unsettled,
        state IN ('thinking', 'waiting') AS unfinished
    FROM
        agents
    WHERE
        agents.workflow_id = @workflow_id
        AND agents.archived_at IS NULL
    FOR UPDATE
),
freed AS (
    DELETE FROM marks
    WHERE marks.agent_id IN (
            SELECT
                prior.id
            FROM
                prior))
UPDATE
    agents a
SET
    archived_at = NOW(),
    pane_id = '',
    state = CASE WHEN prior.unsettled THEN
        'idle'
    ELSE
        a.state
    END,
    state_changed_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_changed_at
    END,
    state_seen_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_seen_at
    END,
    reported_reason = CASE WHEN prior.unsettled THEN
        ''
    ELSE
        a.reported_reason
    END,
    interrupted_at = CASE WHEN prior.unfinished THEN
        NOW()
    ELSE
        a.interrupted_at
    END
FROM
    prior
WHERE
    a.id = prior.id
RETURNING
    a.id,
    prior.pane_id;

-- name: ListChildAgents :many
SELECT
    *
FROM
    agents
WHERE
    parent_agent_id = ANY (@parent_ids::uuid[])
ORDER BY
    id DESC;

-- name: CountLiveChildAgents :one
SELECT
    COUNT(*)
FROM
    agents
WHERE
    parent_agent_id = @parent_agent_id
    AND archived_at IS NULL;

-- name: ListLiveWorkflowAgents :many
SELECT
    *
FROM
    agents
WHERE
    workflow_id = @workflow_id
    AND archived_at IS NULL
ORDER BY
    id DESC;

-- name: MarkWorkflowAgentsRestored :exec
UPDATE
    agents
SET
    is_hidden = FALSE,
    archived_at = NULL
WHERE
    workflow_id = @workflow_id
    AND archived_at IS NOT NULL;

-- name: ListArchivedWorkflowAgents :many
SELECT
    *
FROM
    agents
WHERE
    workflow_id = @workflow_id
    AND archived_at IS NOT NULL
ORDER BY
    id DESC;

-- name: ClaimAgentNudge :many
UPDATE
    agents
SET
    nudged_at = NOW()
WHERE
    id = @id
    AND archived_at IS NULL
    AND (nudged_at IS NULL
        OR state_changed_at > nudged_at
        OR nudged_at < NOW() - interval '1 microsecond' * @retry_micros::bigint)
RETURNING
    id;

-- name: SetAgentNudgeSentByID :exec
UPDATE
    agents
SET
    nudge_sent_at = NOW()
WHERE
    id = @id;

-- name: SetAgentStateByID :exec
UPDATE
    agents
SET
    state = @state,
    state_changed_at = CASE WHEN state IS DISTINCT FROM @state THEN
        NOW()
    ELSE
        state_changed_at
    END,
    reported_reason = CASE WHEN state IS DISTINCT FROM @state
        AND @state IN ('idle', 'uncertain') THEN
        ''
    ELSE
        reported_reason
    END,
    state_seen_at = NOW()
WHERE
    id = @id
    AND archived_at IS NULL;

-- name: SetAgentTurnEndedByID :exec
UPDATE
    agents
SET
    turn_ended_at = NOW()
WHERE
    id = @id;

-- name: SetAgentReportedReasonByID :exec
UPDATE
    agents
SET
    reported_reason = @reported_reason
WHERE
    id = @id;

-- name: MarkAgentSettingUpIfLive :many
UPDATE
    agents
SET
    state = 'setting_up',
    state_changed_at = CASE WHEN state IS DISTINCT FROM 'setting_up' THEN
        NOW()
    ELSE
        state_changed_at
    END,
    reported_reason = '',
    state_seen_at = NOW()
WHERE
    id = @id
    AND archived_at IS NULL
RETURNING
    id;

-- name: MarkAgentSettingUpIfFailedToCreate :many
UPDATE
    agents
SET
    state = 'setting_up',
    state_changed_at = NOW(),
    reported_reason = '',
    state_seen_at = NOW()
WHERE
    id = @id
    AND state = 'failed_to_create'
    AND archived_at IS NULL
RETURNING
    id;

-- name: SetAgentCheckoutByID :exec
UPDATE
    agents
SET
    branch = sqlc.narg('branch'),
    repository_url = sqlc.narg('repository_url'),
    worktree_removed = @worktree_removed,
    sequence_in_progress = @sequence_in_progress
WHERE
    id = @id;

-- name: SetAgentSequenceInProgressByID :exec
UPDATE
    agents
SET
    sequence_in_progress = @sequence_in_progress
WHERE
    id = @id;

-- name: SetAgentInterruptedAtByID :exec
UPDATE
    agents
SET
    interrupted_at = sqlc.narg('interrupted_at')
WHERE
    id = @id;

-- name: HoldAgentByID :many
UPDATE
    agents
SET
    held_at = @held_at,
    held_mid_turn = @held_mid_turn
WHERE
    id = @id
    AND archived_at IS NULL
    AND held_at IS NULL
RETURNING
    id;

-- name: ReleaseAgentByID :many
UPDATE
    agents
SET
    held_at = NULL,
    held_mid_turn = FALSE
WHERE
    id = @id
    AND held_at IS NOT NULL
RETURNING
    id;

-- name: SetAgentHarness :exec
UPDATE
    agents
SET
    harness = @harness,
    flags = @flags,
    session_id = @session_id
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

-- name: ForgetAgentPane :many
UPDATE
    agents
SET
    pane_id = '',
    interrupted_at = COALESCE(sqlc.narg('interrupted_at'), interrupted_at)
WHERE
    id = @id
    AND pane_id = @pane_id
    AND updated_at = @updated_at
RETURNING
    id;

-- name: SetAgentHidden :many
UPDATE
    agents
SET
    is_hidden = @hidden
WHERE
    id = @id
    AND archived_at IS NULL
RETURNING
    id;

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
            id = @agent_id)
ON CONFLICT (agent_id,
    KEY)
    DO UPDATE SET
        value = @value
    RETURNING
        agent_id;

-- name: DeleteAgentMetadata :many
DELETE FROM agent_metadata
WHERE agent_id = @agent_id
    AND KEY = @key
RETURNING
    agent_id;

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

-- name: DeleteAgentsByID :many
DELETE FROM agents
WHERE id = ANY (@ids::uuid[])
RETURNING
    id,
    pane_id;

-- name: DeleteAgentsTranscriptSessions :exec
DELETE FROM agent_transcript_sessions
WHERE agent_id = ANY (@agent_ids::uuid[]);

-- name: RenameAgent :many
UPDATE
    agents
SET
    name = @name
WHERE
    id = @id
RETURNING
    id;

-- name: RestampAgentsArchived :exec
UPDATE
    agents
SET
    archived_at = NOW()
WHERE
    id = ANY (@ids::uuid[])
    AND archived_at IS NOT NULL;

-- name: MarkAgentsArchived :many
WITH prior AS (
    SELECT
        id,
        pane_id,
        state IN ('thinking', 'waiting', 'uncertain', 'setting_up') AS unsettled,
        state IN ('thinking', 'waiting') AS unfinished
    FROM
        agents
    WHERE
        id = ANY (@ids::uuid[])
        AND archived_at IS NULL
    FOR UPDATE
),
freed AS (
    DELETE FROM marks
    WHERE marks.agent_id IN (
            SELECT
                prior.id
            FROM
                prior))
UPDATE
    agents a
SET
    archived_at = NOW(),
    pane_id = '',
    state = CASE WHEN prior.unsettled THEN
        'idle'
    ELSE
        a.state
    END,
    state_changed_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_changed_at
    END,
    state_seen_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_seen_at
    END,
    reported_reason = CASE WHEN prior.unsettled THEN
        ''
    ELSE
        a.reported_reason
    END,
    interrupted_at = CASE WHEN prior.unfinished THEN
        NOW()
    ELSE
        a.interrupted_at
    END
FROM
    prior
WHERE
    a.id = prior.id
RETURNING
    a.id,
    prior.pane_id;

-- name: MarkAgentArchivedIfUnchanged :many
WITH prior AS (
    SELECT
        agents.id,
        agents.state IN ('thinking', 'waiting', 'uncertain', 'setting_up') AS unsettled,
        agents.state IN ('thinking', 'waiting') AS unfinished
    FROM
        agents
    WHERE
        agents.id = @id
        AND agents.archived_at IS NULL
        AND agents.updated_at = @updated_at
    FOR UPDATE
),
freed AS (
    DELETE FROM marks
    WHERE marks.agent_id IN (
            SELECT
                prior.id
            FROM
                prior))
UPDATE
    agents a
SET
    archived_at = NOW(),
    pane_id = '',
    state = CASE WHEN prior.unsettled THEN
        'idle'
    ELSE
        a.state
    END,
    state_changed_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_changed_at
    END,
    state_seen_at = CASE WHEN prior.unsettled THEN
        NOW()
    ELSE
        a.state_seen_at
    END,
    reported_reason = CASE WHEN prior.unsettled THEN
        ''
    ELSE
        a.reported_reason
    END,
    interrupted_at = CASE WHEN prior.unfinished THEN
        NOW()
    ELSE
        a.interrupted_at
    END
FROM
    prior
WHERE
    a.id = prior.id
RETURNING
    a.id;

-- name: RestoreAgents :exec
UPDATE
    agents
SET
    is_hidden = FALSE,
    archived_at = NULL
WHERE
    id = ANY (@ids::uuid[]);

-- name: GetTranscript :one
SELECT
    COALESCE(STRING_AGG(content, E'\n\n' ORDER BY seq), '')::text AS content
FROM
    agent_transcript_chunks
WHERE
    agent_id = @agent_id;

-- name: ListLastTranscriptTurns :many
WITH newest AS (
    SELECT
        c.seq
    FROM
        agent_transcript_chunks c
    WHERE
        c.agent_id = @agent_id
        AND c.role = @role
    ORDER BY
        c.seq DESC
    LIMIT @lim
)
SELECT
    c.seq,
    c.role,
    c.content
FROM
    agent_transcript_chunks c
WHERE
    c.agent_id = @agent_id
    AND c.seq >=(
        SELECT
            MIN(seq)
        FROM
            newest)
    AND c.seq <=(
        SELECT
            MAX(seq)
        FROM
            newest)
ORDER BY
    c.seq;

-- name: OpenTranscriptSession :one
INSERT INTO agent_transcript_sessions(
    id,
    agent_id,
    session_id)
VALUES (
    @id,
    @agent_id,
    @session_id)
ON CONFLICT (
    agent_id,
    session_id)
    DO UPDATE SET
        synced_offset = agent_transcript_sessions.synced_offset
    RETURNING
        id,
        synced_offset;

-- name: AdvanceTranscriptSession :exec
UPDATE
    agent_transcript_sessions
SET
    synced_offset = @synced_offset,
    synced_at = NOW()
WHERE
    id = @id;

-- name: DeleteTranscriptSessionChunks :exec
DELETE FROM agent_transcript_chunks
WHERE session_id = @session_id;

-- name: DeleteAgentsTranscriptChunks :exec
DELETE FROM agent_transcript_chunks
WHERE agent_id = ANY (@agent_ids::uuid[]);

-- name: NextTranscriptChunk :one
SELECT
    COALESCE(MAX(seq) + 1, 0)::int AS next_seq,
    COALESCE(MAX(content_offset + OCTET_LENGTH(content)), 0)::bigint AS content_length
FROM
    agent_transcript_chunks
WHERE
    agent_id = @agent_id;

-- name: InsertTranscriptChunks :copyfrom
INSERT INTO agent_transcript_chunks(
    agent_id,
    session_id,
    seq,
    role,
    content,
    content_offset)
VALUES (
    @agent_id,
    @session_id,
    @seq,
    @role,
    @content,
    @content_offset);

-- name: SearchTranscripts :many
WITH q AS (
    SELECT
        PLAINTO_TSQUERY('english', ARRAY_TO_STRING(@terms::text[], ' ')) AS words,
        ARRAY (
            SELECT
                PHRASETO_TSQUERY('english', t)
            FROM
                UNNEST(@terms::text[]) t
            WHERE
                PHRASETO_TSQUERY('english', t) <> '') AS required,
            ARRAY (
                SELECT
                    PHRASETO_TSQUERY('english', e)
                FROM
                    UNNEST(@excluded::text[]) e) AS forbidden
),
ranked AS (
    SELECT
        c.agent_id,
        c.seq,
        c.role,
        c.content_offset,
        OCTET_LENGTH(c.content) AS content_length,
        a.name,
        a.archived_at IS NOT NULL AS archived,
        TS_HEADLINE('english', c.content, q.words, @headline) AS snippet,
        TS_RANK_CD(c.tsv, q.words, 32) AS rank
    FROM
        agent_transcript_chunks c
        CROSS JOIN q
        JOIN agents a ON a.id = c.agent_id
    WHERE
        CARDINALITY(q.required) > 0
        AND c.tsv @@ ALL (q.required)
        AND NOT (c.tsv @@ ANY (q.forbidden))
        AND (sqlc.narg('agent_id')::uuid IS NULL
            OR c.agent_id = sqlc.narg('agent_id')))
SELECT
    r.agent_id,
    r.seq,
    r.role,
    r.content_offset,
    r.content_length,
    r.name,
    r.archived::boolean AS archived,
    r.snippet::text AS snippet,
    r.rank::float8 AS rank
FROM
    ranked r
WHERE (sqlc.narg('cursor_rank')::float8 IS NULL
    OR r.rank < sqlc.narg('cursor_rank')
    OR (r.rank = sqlc.narg('cursor_rank')
        AND (r.agent_id,
            r.seq) >(sqlc.narg('cursor_agent_id')::uuid,
            sqlc.narg('cursor_seq')::int)))
ORDER BY
    r.rank DESC,
    r.agent_id ASC,
    r.seq ASC
LIMIT @lim;

-- name: SearchTranscriptsNear :many
WITH q AS (
    SELECT
        @terms::text[] AS terms,
        @patterns::text[] AS patterns,
        ARRAY (
            SELECT
                PHRASETO_TSQUERY('english', t)
            FROM
                UNNEST(@terms::text[]) t
            WHERE
                PHRASETO_TSQUERY('english', t) <> '') AS required,
        ARRAY (
            SELECT
                PHRASETO_TSQUERY('english', e)
            FROM
                UNNEST(@excluded::text[]) e) AS forbidden
),
ranked AS (
    SELECT
        c.agent_id,
        c.seq,
        c.role,
        c.content_offset,
        OCTET_LENGTH(c.content) AS content_length,
        a.name,
        a.archived_at IS NOT NULL AS archived,
        SUBSTRING(c.content FROM GREATEST(1,(
                    SELECT
                        MIN(POSITION(LOWER(t) IN LOWER(c.content)))
                    FROM UNNEST(q.terms) t
                    WHERE
                        POSITION(LOWER(t) IN LOWER(c.content)) > 0) - @lead::int)
                FOR @width::int) AS snippet,
        WORD_SIMILARITY(ARRAY_TO_STRING(q.terms, ' '), c.content) AS rank
    FROM
        agent_transcript_chunks c
        CROSS JOIN q
        JOIN agents a ON a.id = c.agent_id
    WHERE
        CARDINALITY(q.terms) > 0
        AND c.content ILIKE ALL (q.patterns)
        AND NOT (CARDINALITY(q.required) > 0
            AND c.tsv @@ ALL (q.required))
        AND NOT (c.tsv @@ ANY (q.forbidden))
        AND (sqlc.narg('agent_id')::uuid IS NULL
            OR c.agent_id = sqlc.narg('agent_id')))
SELECT
    r.agent_id,
    r.seq,
    r.role,
    r.content_offset,
    r.content_length,
    r.name,
    r.archived::boolean AS archived,
    r.snippet::text AS snippet,
    r.rank::float8 AS rank
FROM
    ranked r
WHERE (sqlc.narg('cursor_rank')::float8 IS NULL
    OR r.rank < sqlc.narg('cursor_rank')
    OR (r.rank = sqlc.narg('cursor_rank')
        AND (r.agent_id,
            r.seq) >(sqlc.narg('cursor_agent_id')::uuid,
            sqlc.narg('cursor_seq')::int)))
ORDER BY
    r.rank DESC,
    r.agent_id ASC,
    r.seq ASC
LIMIT @lim;

-- name: CountLiveScheduleAgents :one
SELECT
    COUNT(*)
FROM
    agents
WHERE
    schedule_id = @schedule_id
    AND archived_at IS NULL
    AND state IN ('thinking', 'waiting', 'uncertain');

-- name: ListScheduleAgentNamesOutsideWorkflow :many
SELECT
    name
FROM
    agents
WHERE
    schedule_id = @schedule_id
    AND workflow_id <> @workflow_id
ORDER BY
    id DESC;

-- name: ListAgentDirs :many
SELECT
    id,
    name,
    cwd,
    state,
    state_changed_at,
    archived_at,
    deleting_at,
    adopted_branch,
    workflow_id
FROM
    agents;

-- name: GetWorkflowOrchestratorID :one
SELECT
    s.id
FROM
    agents s
WHERE
    s.workflow_id = @workflow_id
    AND s.role = 'orchestrator'
LIMIT 1;
