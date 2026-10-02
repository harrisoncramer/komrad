-- Add value to enum type: "review_author_kind"
ALTER TYPE "review_author_kind" ADD VALUE 'forge';
-- Modify "forge_review_comments" table
ALTER TABLE "forge_review_comments" ADD COLUMN "trusted" boolean NOT NULL DEFAULT false, ADD COLUMN "forge_review_id" text NOT NULL DEFAULT '';
-- Set comment to table: "forge_review_comments"
COMMENT ON TABLE "forge_review_comments" IS 'One comment in a forge review thread, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it.';
-- Set comment to column: "trusted" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."trusted" IS 'Whether the forge says the author may write to the repository, which releases their comments to the agent without the person sending them.';
-- Set comment to column: "forge_review_id" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."forge_review_id" IS 'The forge''s identifier for the review the comment was submitted in, empty when it names none.';
-- Modify "forge_review_threads" table
ALTER TABLE "forge_review_threads" DROP COLUMN "dismissed_at", DROP COLUMN "accepted_thread_id";
-- Set comment to table: "forge_review_threads"
COMMENT ON TABLE "forge_review_threads" IS 'A review thread as the forge holds it on a pull request a live agent is linked to, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it.';
-- Set comment to column: "external_login" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."external_login" IS 'The forge login of whoever wrote this on the forge, for a comment that came from there, empty when the forge no longer knew them. Null for anything written in komrad.';
-- Set comment to column: "forge_comment_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."forge_comment_id" IS 'The forge comment this mirrors, null for anything written in komrad. It names no row once the forge deletes the comment, which is how the copy is known to be gone there.';
-- Set comment to column: "forge_edited_at" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."forge_edited_at" IS 'When the forge comment had last been edited as of the last sync, null when it never had been.';
-- Modify "reviews" table
ALTER TABLE "reviews" ADD COLUMN "external_login" text NULL;
-- Set comment to column: "external_login" on table: "reviews"
COMMENT ON COLUMN "reviews"."external_login" IS 'The forge login of whoever wrote a review on the forge, empty when the forge no longer knew them. Null for a review written in komrad.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "forge_thread_id" uuid NULL, ADD CONSTRAINT "review_threads_forge_thread_id_fkey" FOREIGN KEY ("engine_id", "forge_thread_id") REFERENCES "forge_review_threads" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("forge_thread_id");
-- Create index "review_threads_forge_thread_id" to table: "review_threads"
CREATE INDEX "review_threads_forge_thread_id" ON "review_threads" ("forge_thread_id");
-- Set comment to column: "forge_thread_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."forge_thread_id" IS 'The forge thread this thread mirrors, null for a thread written in komrad or once the forge has deleted it.';
-- Create index "review_threads_agent_forge_thread" to table: "review_threads"
CREATE UNIQUE INDEX "review_threads_agent_forge_thread" ON "review_threads" ("agent_id", "forge_thread_id") WHERE (forge_thread_id IS NOT NULL);
-- Set comment to index: "review_threads_agent_forge_thread" on table: "review_threads"
COMMENT ON INDEX "review_threads_agent_forge_thread" IS 'Holds each agent to one mirror of a forge thread.';
-- Create index "review_messages_thread_forge_comment" to table: "review_messages"
CREATE UNIQUE INDEX "review_messages_thread_forge_comment" ON "review_messages" ("thread_id", "forge_comment_id") WHERE (forge_comment_id IS NOT NULL);
-- Set comment to index: "review_messages_thread_forge_comment" on table: "review_messages"
COMMENT ON INDEX "review_messages_thread_forge_comment" IS 'Holds each mirrored thread to one copy of a forge comment.';
CREATE POLICY forge_released ON reviews AS RESTRICTIVE FOR SELECT TO komrad_reader USING (author_kind IN ('human', 'agent') OR released_at IS NOT NULL);
CREATE POLICY forge_released ON review_threads AS RESTRICTIVE FOR SELECT TO komrad_reader USING (EXISTS (SELECT 1 FROM reviews WHERE reviews.id = review_threads.review_id));
CREATE POLICY forge_released ON review_messages AS RESTRICTIVE FOR SELECT TO komrad_reader USING (review_messages.review_id IS NULL OR EXISTS (SELECT 1 FROM reviews WHERE reviews.id = review_messages.review_id));
SELECT attach_updated_at_triggers();
