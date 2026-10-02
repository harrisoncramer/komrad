-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "forge_resolved" boolean NOT NULL DEFAULT false;
-- Set comment to column: "forge_resolved" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."forge_resolved" IS 'Whether the forge said the thread was resolved as of the last sync, so only a change there resolves or reopens it here.';
