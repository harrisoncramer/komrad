-- name: ListWorktrees :many
SELECT
    w.dir,
    w.repo_dir,
    w.branch,
    w.readable,
    w.mtime,
    w.size_bytes,
    w.share_bytes,
    w.measured_at,
    w.size_error,
    w.standing_read_at,
    w.dirty,
    w.sequencing,
    w.unlanded,
    w.standing_error,
    t.archived_at
FROM
    worktrees w
    LEFT JOIN tasks t ON t.id::text = w.name
ORDER BY
    w.dir;

-- name: ListWatchedWorktrees :many
SELECT
    w.dir,
    w.repo_dir,
    w.branch,
    w.readable,
    w.mtime,
    w.size_bytes,
    w.share_bytes,
    w.measured_at,
    w.size_error,
    w.standing_read_at,
    w.dirty,
    w.sequencing,
    w.unlanded,
    w.standing_error,
    t.archived_at
FROM
    worktrees w
    LEFT JOIN tasks t ON t.id::text = w.name
WHERE (sqlc.narg(keys)::text[] IS NULL
    OR w.dir = ANY (sqlc.narg(keys)::text[]))
AND t.deleting_at IS NULL
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
    share_bytes = @share_bytes,
    measured_at = @measured_at,
    size_error = sqlc.narg(size_error)
WHERE
    dir = @dir;

-- name: DeleteWorktrees :exec
DELETE FROM worktrees
WHERE dir = ANY (@dirs::text[]);

-- name: DeleteTaskWorktrees :exec
DELETE FROM worktrees
WHERE name = sqlc.arg(task_id)::text;

-- name: RecordWorktreeStanding :exec
UPDATE
    worktrees
SET
    standing_read_at = @standing_read_at,
    dirty = @dirty,
    sequencing = @sequencing,
    unlanded = @unlanded,
    standing_error = sqlc.narg(standing_error)
WHERE
    dir = @dir;
