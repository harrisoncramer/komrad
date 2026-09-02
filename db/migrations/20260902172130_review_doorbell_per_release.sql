-- Modify "mail" table
ALTER TABLE "mail" DROP COLUMN "review_message_id";
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'What the mail is, which is what lets the panel tell a doorbell weave raised itself from something an agent wrote. Ordinary mail is plain. It is also what a doorbell is counted by, since an agent needs only one unread doorbell of a kind however many comments are waiting behind it.';
-- Set comment to index: "mail_pending" on table: "mail"
COMMENT ON INDEX "mail_pending" IS 'Reads what an agent is holding unread, which is both what a send is refused by the size of and where the doorbell a further release joins is found.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, null until it has written one. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see. There is no mirror of it for the person''s side, since what the agent has been told is recorded per message by review_messages.sent_at and the doorbell it points at.';
-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "doorbell_mail_id" uuid NULL, ADD CONSTRAINT "review_messages_doorbell_mail_id_fkey" FOREIGN KEY ("doorbell_mail_id") REFERENCES "mail" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "review_messages_doorbell_mail_id" to table: "review_messages"
CREATE INDEX "review_messages_doorbell_mail_id" ON "review_messages" ("doorbell_mail_id");
-- Set comment to column: "doorbell_mail_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."doorbell_mail_id" IS 'The mail that told the agent this comment was waiting, null while the comment is unsent. One doorbell covers every comment released under it, since they all say the same thing, and the delivery state the panel draws for a comment is read off whichever doorbell it points at. It is set null rather than cascaded when that mail is deleted, which is what makes a comment whose doorbell the agent threw away read as needing another.';
-- Set comment to index: "review_messages_doorbell_mail_id" on table: "review_messages"
COMMENT ON INDEX "review_messages_doorbell_mail_id" IS 'Finds the comments a piece of mail is the doorbell for, which is what the foreign key walks when that mail is deleted.';
