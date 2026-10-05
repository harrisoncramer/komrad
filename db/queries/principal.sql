-- name: InsertPrincipal :one
INSERT INTO identity.principals(
    name,
    kind,
    provider,
    subject)
VALUES (
    @name,
    @kind,
    sqlc.narg('provider'),
    sqlc.narg('subject'))
RETURNING
    *;

-- name: GetPrincipalByName :one
SELECT
    *
FROM
    identity.principals
WHERE
    name = @name;

-- name: GetPrincipalNameByIdentity :one
SELECT
    name
FROM
    identity.principals
WHERE
    provider = @provider
    AND subject = @subject;

-- name: ListPrincipals :many
SELECT
    *
FROM
    identity.principals
ORDER BY
    name;

-- name: SetPrincipalDisabled :one
UPDATE
    identity.principals
SET
    disabled_at = CASE WHEN @disabled::boolean THEN
        COALESCE(disabled_at, NOW())
    END
WHERE
    name = @name
RETURNING
    *;

-- name: DeletePrincipal :one
WITH removed AS (
    DELETE FROM identity.principals
    WHERE name = @name
    RETURNING
        id
)
SELECT
    removed.id,
(
        SELECT
            COUNT(*)
        FROM
            identity.bindings b
        WHERE
            b.principal_id = removed.id)::bigint AS bindings
FROM
    removed;
