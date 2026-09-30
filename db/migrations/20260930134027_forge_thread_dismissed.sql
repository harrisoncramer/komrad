-- Modify "forge_review_threads" table
ALTER TABLE "forge_review_threads" ADD COLUMN "dismissed_at" timestamptz NULL;
-- Set comment to column: "dismissed_at" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."dismissed_at" IS 'When the person dismissed the thread in their review, null while they never have. A dismissed thread is still drawn but no longer counted. The sync leaves it alone, and a comment written on the forge after it makes the thread active again.';
