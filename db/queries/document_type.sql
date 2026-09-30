-- name: ListDocumentTypes :many
SELECT
    *
FROM
    document_types
ORDER BY
    LOWER(name),
    name;

-- name: GetDocumentType :one
SELECT
    *
FROM
    document_types
WHERE
    id = @id;

-- name: GetDocumentTypeByName :one
SELECT
    *
FROM
    document_types
WHERE
    name = @name;

-- name: InsertDocumentType :one
WITH inserted AS (
    INSERT INTO document_types(
        name,
        context)
    VALUES (
        @name,
        @context)
RETURNING
    *
),
recorded AS (
    INSERT INTO document_type_versions(
        document_type_id,
        version,
        name,
        context,
        created_at)
    SELECT
        id,
        version,
        name,
        context,
        updated_at
    FROM
        inserted
)
SELECT
    *
FROM
    inserted;

-- name: UpdateDocumentType :one
WITH updated AS (
    UPDATE
        document_types
    SET
        name = @name,
        context = @context,
        version = document_types.version + 1
    WHERE
        document_types.id = @id
        AND document_types.version = sqlc.arg('read_version')
    RETURNING
        *
),
recorded AS (
    INSERT INTO document_type_versions(
        document_type_id,
        version,
        name,
        context,
        created_at)
    SELECT
        id,
        version,
        name,
        context,
        updated_at
    FROM
        updated
)
SELECT
    *
FROM
    updated;

-- name: DeleteDocumentType :execrows
DELETE FROM document_types
WHERE id = @id;

-- name: CountDocumentsOfType :one
SELECT
    COUNT(*)
FROM
    documents
WHERE
    type = @type;

-- name: ListDocumentTypeVersions :many
SELECT
    version,
    name,
    created_at
FROM
    document_type_versions
WHERE
    document_type_id = @document_type_id
ORDER BY
    version DESC;

-- name: GetDocumentTypeVersion :one
SELECT
    *
FROM
    document_type_versions
WHERE
    document_type_id = @document_type_id
    AND version = @version;
