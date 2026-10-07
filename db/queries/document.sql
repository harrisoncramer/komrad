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
    d.updated_at
FROM
    documents d
    JOIN agents a ON a.id = d.agent_id
WHERE (sqlc.narg('agent_id')::uuid IS NULL
    OR d.agent_id = sqlc.narg('agent_id')::uuid)
AND (sqlc.narg('type')::text IS NULL
    OR d.type = sqlc.narg('type')::text)
AND a.deleting_at IS NULL
AND (sqlc.narg('cursor_id')::uuid IS NULL
    OR (d.updated_at,
        d.id) <(@cursor_updated_at::timestamptz,
        sqlc.narg('cursor_id')::uuid))
ORDER BY
    d.updated_at DESC,
    d.id DESC
LIMIT @lim;

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

-- name: SearchDocuments :many
WITH q AS (
    SELECT
        WEBSEARCH_TO_TSQUERY('english_nostop', @wanted::text) AS wanted,
        WEBSEARCH_TO_TSQUERY('english_nostop', @excluded::text) AS unwanted,
        sqlc.narg('phrase')::text AS phrase
),
found AS (
    SELECT
        d.id,
        d.agent_id,
        d.name,
        d.body,
        TO_TSVECTOR('english_nostop', d.body) AS tsv
    FROM
        documents d
        JOIN agents a ON a.id = d.agent_id
    WHERE
        a.deleting_at IS NULL
        AND (sqlc.narg('agent_id')::uuid IS NULL
            OR d.agent_id = sqlc.narg('agent_id')::uuid)
        AND (sqlc.narg('type')::text IS NULL
            OR d.type = sqlc.narg('type')::text))
SELECT
    f.id,
    f.agent_id,
    f.name,
    TS_HEADLINE('english_nostop', f.body, q.wanted, @headline::text)::text AS snippet,
    (TS_RANK_CD(f.tsv, q.wanted, 2) + COALESCE((f.body ~* q.phrase)::int, 0))::float8 AS rank
FROM
    found f
    CROSS JOIN q
WHERE
    q.wanted <> ''
    AND f.tsv @@ (q.wanted && q.unwanted)
ORDER BY
    rank DESC,
    f.id
LIMIT @lim;

-- name: SearchDocumentsNear :many
WITH q AS (
    SELECT
        @terms::text[] AS terms,
        @patterns::text[] AS patterns,
        WEBSEARCH_TO_TSQUERY('english_nostop', @wanted::text) AS wanted,
        WEBSEARCH_TO_TSQUERY('english_nostop', @excluded::text) AS unwanted
),
found AS (
    SELECT
        d.id,
        d.agent_id,
        d.name,
        d.body,
        TO_TSVECTOR('english_nostop', d.body) AS tsv
    FROM
        documents d
        JOIN agents a ON a.id = d.agent_id
    WHERE
        a.deleting_at IS NULL
        AND (sqlc.narg('agent_id')::uuid IS NULL
            OR d.agent_id = sqlc.narg('agent_id')::uuid)
        AND (sqlc.narg('type')::text IS NULL
            OR d.type = sqlc.narg('type')::text))
SELECT
    f.id,
    f.agent_id,
    f.name,
    SUBSTRING(f.body FROM GREATEST(1,(
                SELECT
                    MIN(POSITION(LOWER(t) IN LOWER(f.body)))
                FROM UNNEST(q.terms) t
                WHERE
                    POSITION(LOWER(t) IN LOWER(f.body)) > 0) - @lead::int)
            FOR @width::int)::text AS snippet,
    WORD_SIMILARITY(ARRAY_TO_STRING(q.terms, ' '), f.body)::float8 AS rank
FROM
    found f
    CROSS JOIN q
WHERE
    CARDINALITY(q.terms) > 0
    AND f.body ILIKE ALL (q.patterns)
    AND NOT (q.wanted <> ''
        AND f.tsv @@ q.wanted)
    AND (q.unwanted = ''
        OR f.tsv @@ q.unwanted)
ORDER BY
    rank DESC,
    f.id
LIMIT @lim;
