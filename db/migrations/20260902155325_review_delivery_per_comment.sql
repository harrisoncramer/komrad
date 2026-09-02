-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "sent_at" timestamptz NULL;
-- Set comment to column: "sent_at" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."sent_at" IS 'When this message was released to the agent, null while it is still only written down. It is the durable record that a comment was let go of, kept here rather than read off the mail raised for it, since an agent may delete that mail and mail expires.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" DROP COLUMN "last_human_activity_at", DROP COLUMN "agent_read_at";
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'When the person reviewing last read this thread, stamped from last_agent_activity_at rather than from the clock, so a reply written mid-read is not marked read without being shown. It is what says whether the agent has replied since they looked.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, null until it has written one. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see. There is no mirror of it for the person''s side, since what the agent has been told is recorded per message by review_messages.sent_at and the mail raised against it.';
-- Drop index "mail_pending_kind" from table: "mail"
DROP INDEX "mail_pending_kind";
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "review_message_id" uuid NULL, ADD CONSTRAINT "mail_review_message_id_fkey" FOREIGN KEY ("review_message_id") REFERENCES "review_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "mail_pending_review_message" to table: "mail"
CREATE UNIQUE INDEX "mail_pending_review_message" ON "mail" ("review_message_id") WHERE (claimed_at IS NULL);
-- Create index "mail_review_message_id" to table: "mail"
CREATE INDEX "mail_review_message_id" ON "mail" ("review_message_id");
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'What the mail is, which is what lets the panel tell a doorbell weave raised itself from something an agent wrote. Ordinary mail is plain. It is not what decides whether a second copy may stack up: a doorbell is counted by the review comment it points at rather than by its kind.';
-- Set comment to column: "review_message_id" on table: "mail"
COMMENT ON COLUMN "mail"."review_message_id" IS 'The review comment this mail is the doorbell for, null for anything else. It is what one doorbell per comment is counted by, and it is set null rather than cascaded when that comment is deleted, since the agent may already have read the doorbell for a comment the person reviewing then took back.';
-- Set comment to index: "mail_pending_review_message" on table: "mail"
COMMENT ON INDEX "mail_pending_review_message" IS 'At most one unclaimed doorbell per review comment. A doorbell that is still waiting to be read is already saying what a second one would say, so releasing again must not stack another copy behind it. The predicate cannot also ask that the doorbell still be deliverable, since Postgres will not index on NOW(), so InsertMail carries that half itself and rings the row again once it has expired. Mail that is not a doorbell has a null here, and nulls do not collide, so ordinary mail is untouched by it.';
-- Set comment to index: "mail_review_message_id" on table: "mail"
COMMENT ON INDEX "mail_review_message_id" IS 'Reads the doorbells raised for a comment, claimed ones included, which is what the delivery state of that comment is read off.';
