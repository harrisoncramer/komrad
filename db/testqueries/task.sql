-- name: DeleteTask :exec
DELETE FROM tasks
WHERE id::text = sqlc.arg('id')::text;
