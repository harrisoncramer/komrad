-- name: ListWorktreeSizes :many
SELECT
    dir,
    size_bytes,
    measured_at,
    error
FROM
    worktree_sizes
ORDER BY
    dir;

-- name: UpsertWorktreeSize :exec
INSERT INTO worktree_sizes(
    dir,
    size_bytes,
    measured_at,
    error)
VALUES (
    @dir,
    @size_bytes,
    @measured_at,
    sqlc.narg(error))
ON CONFLICT (
    dir)
    DO UPDATE SET
        size_bytes = EXCLUDED.size_bytes,
        measured_at = EXCLUDED.measured_at,
        error = EXCLUDED.error;

-- name: DeleteWorktreeSizes :exec
DELETE FROM worktree_sizes
WHERE dir = ANY (@dirs::text[]);
