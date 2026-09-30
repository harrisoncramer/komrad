-- name: InsertDocument :one
WITH inserted AS (
    INSERT INTO documents(
        agent_id,
        name,
        type,
        body)
    VALUES (
        @agent_id,
        @name,
        @type,
        @body)
RETURNING
    *
),
recorded AS (
    INSERT INTO document_versions(
        document_id,
        version,
        name,
        type,
        body,
        by_person,
        created_at)
    SELECT
        id,
        version,
        name,
        type,
        body,
        @by_person,
        updated_at
    FROM
        inserted
)
SELECT
    *
FROM
    inserted;

-- name: UpdateDocument :one
WITH updated AS (
    UPDATE
        documents
    SET
        name = @name,
        type = @type,
        body = @body,
        version = documents.version + 1
    WHERE
        documents.id = @id
        AND documents.agent_id = @agent_id
        AND documents.version = sqlc.arg('read_version')
    RETURNING
        *
),
recorded AS (
    INSERT INTO document_versions(
        document_id,
        version,
        name,
        type,
        body,
        by_person,
        created_at)
    SELECT
        id,
        version,
        name,
        type,
        body,
        @by_person,
        updated_at
    FROM
        updated
)
SELECT
    *
FROM
    updated;

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
    type,
    version,
    created_at,
    updated_at
FROM
    documents
WHERE
    agent_id = @agent_id
    AND (sqlc.narg('type')::text IS NULL
        OR type = sqlc.narg('type')::text)
ORDER BY
    updated_at DESC,
    id DESC;

-- name: ListDocumentVersions :many
SELECT
    version,
    name,
    type,
    by_person,
    created_at
FROM
    document_versions
WHERE
    document_id = @document_id
ORDER BY
    version DESC;

-- name: GetDocumentVersion :one
SELECT
    *
FROM
    document_versions
WHERE
    document_id = @document_id
    AND version = @version;
