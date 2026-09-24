-- name: ListWatchedWorktrees :many
SELECT
    dir,
    repo_dir,
    branch,
    readable,
    mtime,
    size_bytes,
    measured_at,
    size_error,
    tenant_id
FROM
    worktrees
WHERE
    sqlc.narg(keys)::text[] IS NULL
    OR dir = ANY (sqlc.narg(keys)::text[])
ORDER BY
    dir;

-- name: UpsertWorktree :exec
INSERT INTO worktrees(
    dir,
    repo_dir,
    branch,
    readable,
    mtime)
VALUES (
    @dir,
    @repo_dir,
    @branch,
    @readable,
    sqlc.narg(mtime))
ON CONFLICT (
    tenant_id,
    dir)
    DO UPDATE SET
        repo_dir = EXCLUDED.repo_dir,
        branch = EXCLUDED.branch,
        readable = EXCLUDED.readable,
        mtime = EXCLUDED.mtime
    WHERE (worktrees.repo_dir, worktrees.branch, worktrees.readable, worktrees.mtime) IS DISTINCT FROM (EXCLUDED.repo_dir, EXCLUDED.branch, EXCLUDED.readable, EXCLUDED.mtime);

-- name: RecordWorktreeSize :exec
UPDATE
    worktrees
SET
    size_bytes = @size_bytes,
    measured_at = @measured_at,
    size_error = sqlc.narg(size_error)
WHERE
    dir = @dir;

-- name: DeleteWorktrees :exec
DELETE FROM worktrees
WHERE dir = ANY (@dirs::text[]);
