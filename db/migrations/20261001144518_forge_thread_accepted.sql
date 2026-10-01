-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "external_login" text NULL, ADD COLUMN "forge_comment_id" uuid NULL, ADD COLUMN "forge_edited_at" timestamptz NULL;
-- Set comment to column: "external_login" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."external_login" IS 'The forge login of whoever wrote this on the forge, for a comment the person accepted from there, empty when the forge no longer knew them. Null for anything written in komrad.';
-- Set comment to column: "forge_comment_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."forge_comment_id" IS 'The forge comment this was copied from, null for anything written in komrad. It names no row once the forge deletes the comment, which is how the copy is known to be gone there.';
-- Set comment to column: "forge_edited_at" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."forge_edited_at" IS 'When the forge comment had last been edited at the moment it was copied, null when it never had been. A later edit on the forge is told apart by this.';
-- Modify "forge_review_threads" table
ALTER TABLE "forge_review_threads" ADD COLUMN "accepted_thread_id" uuid NULL, ADD CONSTRAINT "forge_review_threads_accepted_thread_id_fkey" FOREIGN KEY ("engine_id", "accepted_thread_id") REFERENCES "review_threads" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("accepted_thread_id");
-- Set comment to column: "accepted_thread_id" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."accepted_thread_id" IS 'The review thread the person accepted this thread into, null while they never have. Deleting that review thread, as discarding an unsent review does, makes this an offer again.';
