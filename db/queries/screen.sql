-- name: UpsertAgentScreen :exec
INSERT INTO agent_screens(
    agent_id,
    frame,
    width,
    height,
    captured_at)
VALUES (
    @agent_id,
    @frame,
    @width,
    @height,
    NOW())
ON CONFLICT (
    agent_id)
    DO UPDATE SET
        frame = EXCLUDED.frame,
        width = EXCLUDED.width,
        height = EXCLUDED.height,
        captured_at = EXCLUDED.captured_at;

-- name: GetAgentScreen :one
SELECT
    agent_id,
    frame,
    width,
    height,
    captured_at
FROM
    agent_screens
WHERE
    agent_id = @agent_id;
