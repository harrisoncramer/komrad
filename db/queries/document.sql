-- name: InsertDocument :one
INSERT INTO documents(
    agent_id,
    name,
    kind,
    body)
VALUES (
    @agent_id,
    @name,
    @kind,
    @body)
RETURNING
    *;

-- name: UpdateDocument :one
UPDATE
    documents
SET
    name = @name,
    kind = @kind,
    body = @body,
    updated_at = NOW()
WHERE
    id = @id
    AND agent_id = @agent_id
RETURNING
    *;

-- name: GetDocument :one
SELECT
    *
FROM
    documents
WHERE
    id = @id;

-- name: GetDocumentIDByName :one
SELECT
    id
FROM
    documents
WHERE
    agent_id = @agent_id
    AND name = @name;

-- name: ListAgentDocuments :many
SELECT
    id,
    agent_id,
    name,
    kind,
    created_at,
    updated_at
FROM
    documents
WHERE
    agent_id = @agent_id
    AND (sqlc.narg('kind')::document_kind IS NULL
        OR kind = sqlc.narg('kind')::document_kind)
ORDER BY
    updated_at DESC,
    id DESC;
