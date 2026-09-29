-- Modify "worktrees" table
ALTER TABLE "worktrees" ADD COLUMN "share_bytes" bigint NOT NULL DEFAULT 0;
-- Set comment to column: "share_bytes" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."share_bytes" IS 'The directory''s share of size_bytes, each hard linked file''s blocks split evenly across its links. Zero until measured and while size_error is set.';
