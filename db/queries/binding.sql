-- name: GetEngineByName :one
SELECT
    id,
    name
FROM
    engines
WHERE
    name = @name;

-- name: UpsertBinding :one
WITH previous AS (
    SELECT
        b.role
    FROM
        identity.bindings b
    WHERE
        b.principal_id = @principal_id
        AND b.engine_id = @engine_id
),
written AS (
    INSERT INTO identity.bindings(
        principal_id,
        engine_id,
        role)
    VALUES (
        @principal_id,
        @engine_id,
        @role)
ON CONFLICT (
    principal_id,
    engine_id)
    DO UPDATE SET ROLE = EXCLUDED.role
RETURNING
    ROLE)
    SELECT
        written.role, previous.role AS previous_role
    FROM
        written
    LEFT JOIN previous ON TRUE;

-- name: DeleteBinding :exec
DELETE FROM identity.bindings
WHERE principal_id = @principal_id
    AND engine_id = @engine_id;

-- name: ListBindings :many
SELECT
    b.principal_id,
    b.engine_id,
    e.name AS engine_name,
    b.role
FROM
    identity.bindings b
    JOIN engines e ON e.id = b.engine_id
ORDER BY
    e.name;
