-- Create enum type "mail_kind"
CREATE TYPE "mail_kind" AS ENUM ('plain', 'review_activity');
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "kind" "mail_kind" NOT NULL DEFAULT 'plain';
-- Create index "mail_pending_kind" to table: "mail"
CREATE UNIQUE INDEX "mail_pending_kind" ON "mail" ("to_agent_id", "kind") WHERE ((claimed_at IS NULL) AND (kind <> 'plain'::mail_kind));
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'What the mail is, which is what lets a notice that only says there is something to look at be sent again without stacking up. Ordinary mail is plain and never deduplicated, since two agents saying the same thing twice are two things to read.';
-- Set comment to index: "mail_pending_kind" on table: "mail"
COMMENT ON INDEX "mail_pending_kind" IS 'At most one unclaimed notice of a given kind per agent. A doorbell that is still waiting to be read is already saying what a second one would say, so pressing the key again must not stack another copy behind it.';
-- Set comment to column: "author_kind" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_kind" IS 'The type of the author who wrote the message.';
-- Set comment to column: "author_agent_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_agent_id" IS 'The agent that wrote this message, if relevant, kept as a link to who it was.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" ALTER COLUMN "last_human_activity_at" DROP NOT NULL, ALTER COLUMN "last_human_activity_at" DROP DEFAULT, ADD COLUMN "last_agent_activity_at" timestamptz NULL;
-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it: relative to the repository root, where the change left it if it moved it.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They say what the thread was about once the agent has rewritten that code, and are what the viewer compares the file against to decide whether the anchor still holds.';
-- Set comment to column: "last_human_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_human_activity_at" IS 'When the person reviewing last wrote or rewrote a message here, null until they have written one. Compared against agent_read_at to decide what the agent has not been told, so that rewriting a message counts as activity rather than hiding behind its original created_at.';
-- Set comment to column: "agent_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_read_at" IS 'When the agent last read this thread, stamped from the last_human_activity_at of the row it was shown rather than from the clock, so a message written mid-read is not marked read without being returned.';
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'When the person reviewing last read this thread, stamped from last_agent_activity_at the same way, which is what says whether the agent has replied since they looked.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'The mirror of last_human_activity_at for the agent''s side, null until the agent has written. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see.';
