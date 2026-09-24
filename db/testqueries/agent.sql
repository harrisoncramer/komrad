-- name: BackdateAgentNudge :exec
UPDATE
    agents
SET
    nudged_at = NOW() - interval '1 microsecond' * @age_micros::bigint
WHERE
    id::text = @id::text;

-- name: BackdateAgentStateSeen :exec
UPDATE
    agents
SET
    state_seen_at = NOW() - interval '1 microsecond' * @age_micros::bigint
WHERE
    id::text = @id::text;
