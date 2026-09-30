-- Modify "forge_review_threads" table
ALTER TABLE "forge_review_threads" ADD COLUMN "old_side" boolean NOT NULL DEFAULT false, ADD COLUMN "commit_sha" text NOT NULL DEFAULT '', ADD COLUMN "quote" text NOT NULL DEFAULT '';
-- Set comment to column: "line" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."line" IS 'The line of the file the thread was written against, counted in commit_sha on the side old_side names, null for a comment on the change as a whole or on a file.';
-- Set comment to column: "old_side" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."old_side" IS 'Whether the line is counted on the old side of the forge''s diff rather than the new one.';
-- Set comment to column: "commit_sha" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."commit_sha" IS 'The commit the forge counted the line in, empty when it names none.';
-- Set comment to column: "quote" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."quote" IS 'The text of the line the thread was written on as the forge''s diff drew it, with its diff marker taken off.';
