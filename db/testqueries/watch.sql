-- name: NotifyWatch :exec
SELECT
    PG_NOTIFY(watch_channel(), @payload::text);

-- name: GetWatchChannel :one
SELECT
    watch_channel()::text;

-- name: ListListeningChannels :many
SELECT
    PG_LISTENING_CHANNELS()::text;

-- name: TerminateWatchListeners :exec
SELECT
    PG_TERMINATE_BACKEND(pid)
FROM
    pg_catalog.pg_stat_activity
WHERE
    datname = CURRENT_DATABASE()
    AND pid <> PG_BACKEND_PID()
    AND query LIKE '%listen_to_watch_channel%';
