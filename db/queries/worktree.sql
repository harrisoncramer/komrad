-- name: ListWorktrees :many
SELECT
    dir,
    repo_dir,
    branch,
    readable,
    mtime,
    size_bytes,
    measured_at,
    size_error
FROM
    worktrees
ORDER BY
    dir;

-- name: ListWatchedWorktrees :many
SELECT
    w.dir,
    w.repo_dir,
    w.branch,
    w.readable,
    w.mtime,
    w.size_bytes,
    w.measured_at,
    w.size_error
FROM
    worktrees w
WHERE (sqlc.narg(keys)::text[] IS NULL
    OR w.dir = ANY (sqlc.narg(keys)::text[]))
AND NOT EXISTS (
    SELECT
        1
    FROM
        agents a
    WHERE
        a.id::text = w.name
        AND a.deleting_at IS NOT NULL)
ORDER BY
    w.dir;

-- name: UpsertWorktree :exec
INSERT INTO worktrees(
    dir,
    name,
    repo_dir,
    branch,
    readable,
    mtime)
VALUES (
    @dir,
    @name,
    @repo_dir,
    @branch,
    @readable,
    sqlc.narg(mtime))
ON CONFLICT (
    engine_id,
    dir)
    DO UPDATE SET
        name = EXCLUDED.name,
        repo_dir = EXCLUDED.repo_dir,
        branch = EXCLUDED.branch,
        readable = EXCLUDED.readable,
        mtime = EXCLUDED.mtime
    WHERE (
        worktrees.name, worktrees.repo_dir, worktrees.branch, worktrees.readable, worktrees.mtime) IS DISTINCT FROM (
        EXCLUDED.name,
        EXCLUDED.repo_dir,
        EXCLUDED.branch,
        EXCLUDED.readable,
        EXCLUDED.mtime);

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

-- name: DeleteWorktreesByName :exec
DELETE FROM worktrees
WHERE name = ANY (@names::text[]);
