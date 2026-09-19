-- name: UpsertWorktreeSize :exec
INSERT INTO worktree_sizes(
    dir,
    size_bytes,
    counted_at,
    entries)
VALUES (
    @dir,
    @size_bytes,
    @counted_at,
    @entries)
ON CONFLICT (dir)
    DO UPDATE SET
        size_bytes = EXCLUDED.size_bytes,
        counted_at = EXCLUDED.counted_at,
        entries = EXCLUDED.entries;

-- name: ListWorktreeSizes :many
SELECT
    dir,
    size_bytes,
    counted_at,
    entries
FROM
    worktree_sizes
ORDER BY
    dir;

-- name: DeleteWorktreeSizes :exec
DELETE FROM worktree_sizes
WHERE dir = ANY (@dirs::text[]);
