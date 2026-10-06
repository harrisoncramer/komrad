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

-- name: ListDocuments :many
SELECT
    d.id,
    d.agent_id,
    d.name,
    d.type,
    d.version,
    d.created_at,
    d.updated_at,
    a.name AS agent_name,
    w.name AS workflow_name,
(a.archived_at IS NOT NULL)::boolean AS agent_archived
FROM
    documents d
    JOIN agents a ON a.id = d.agent_id
    JOIN workflows w ON w.id = a.workflow_id
WHERE (sqlc.narg('agent_id')::uuid IS NULL
    OR d.agent_id = sqlc.narg('agent_id')::uuid)
AND (sqlc.narg('type')::text IS NULL
    OR d.type = sqlc.narg('type')::text)
AND a.deleting_at IS NULL
ORDER BY
    d.updated_at DESC,
    d.id DESC;

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

-- name: DeleteDocument :execrows
DELETE FROM documents
WHERE id = @id;
