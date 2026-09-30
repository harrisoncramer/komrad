-- name: ListDocumentTypes :many
SELECT
    *
FROM
    document_types
ORDER BY
    lower(name),
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
INSERT INTO document_types(
    name,
    context)
VALUES (
    @name,
    @context)
RETURNING
    *;

-- name: UpdateDocumentType :one
UPDATE
    document_types
SET
    name = @name,
    context = @context,
    version = document_types.version + 1
WHERE
    id = @id
    AND version = sqlc.arg('read_version')
RETURNING
    *;

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
