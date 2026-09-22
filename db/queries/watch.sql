-- name: NotifyWatch :exec
SELECT
    PG_NOTIFY(@channel::text, @table_name::text || ' ' || keys.key)
FROM
    UNNEST(@keys::text[]) AS keys(key);
