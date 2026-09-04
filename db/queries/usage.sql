-- name: InsertAgentTokenUsage :exec
INSERT INTO agent_token_usage(
    agent_id,
    KEY,
    at,
    input_tokens,
    cache_read_tokens,
    cache_write_tokens,
    output_tokens)
SELECT
    @agent_id::uuid,
    u.key,
    u.at,
    u.input_tokens,
    u.cache_read_tokens,
    u.cache_write_tokens,
    u.output_tokens
FROM
    ROWS
FROM (UNNEST(@keys::text[]),
    UNNEST(@ats::timestamptz[]),
    UNNEST(@inputs::bigint[]),
    UNNEST(@cache_reads::bigint[]),
    UNNEST(@cache_writes::bigint[]),
    UNNEST(@outputs::bigint[])) AS u(KEY,
        at,
        input_tokens,
        cache_read_tokens,
        cache_write_tokens,
        output_tokens)
ON CONFLICT (agent_id,
    KEY)
    DO NOTHING;

-- name: SumAgentTokenUsageSince :many
SELECT
    agent_id,
    SUM(input_tokens)::bigint AS input_tokens,
    SUM(cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint AS cache_write_tokens,
    SUM(output_tokens)::bigint AS output_tokens
FROM
    agent_token_usage
WHERE
    at >= @since
GROUP BY
    agent_id;

-- name: PruneAgentTokenUsage :exec
DELETE FROM agent_token_usage
WHERE at < @before;
