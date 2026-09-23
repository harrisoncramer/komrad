-- name: ListenToWatchChannel :exec
SELECT
    listen_to_watch_channel();

-- name: NotifyWatch :exec
SELECT
    PG_NOTIFY(watch_channel(), sqlc.arg('payload')::text);
