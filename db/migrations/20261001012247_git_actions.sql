-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'worktree_pushed';
-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'worktree_aborted';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "auto_merge" boolean NULL, ADD COLUMN "auto_mergeable" boolean NOT NULL DEFAULT false;
-- Set comment to column: "auto_merge" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."auto_merge" IS 'Whether the forge says the pull request will merge on its own once its checks and reviews pass, null until it has been read. Read with the checks and kept fresh the same way.';
-- Set comment to column: "auto_mergeable" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."auto_mergeable" IS 'Whether the forge would let komrad''s credential turn auto-merge on now, which it will not for a draft, a pull request with nothing left to wait for or a repository that does not allow it.';
