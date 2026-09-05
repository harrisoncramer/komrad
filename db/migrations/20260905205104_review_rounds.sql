-- Create enum type "review_verdict"
CREATE TYPE "review_verdict" AS ENUM ('approved', 'changes_requested', 'commented');
-- Create "review_rounds" table
CREATE TABLE "review_rounds" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "ordinal" integer NOT NULL,
  "base_sha" text NOT NULL,
  "snapshot_sha" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "review_rounds_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "review_rounds_agent_ordinal" to table: "review_rounds"
CREATE UNIQUE INDEX "review_rounds_agent_ordinal" ON "review_rounds" ("agent_id", "ordinal");
-- Set comment to table: "review_rounds"
COMMENT ON TABLE "review_rounds" IS 'One immutable snapshot of what an agent has written, which is what a review is given against.';
-- Set comment to column: "ordinal" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."ordinal" IS 'Which round this is for its agent, counting from one, which is what the panel names it by.';
-- Set comment to column: "base_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."base_sha" IS 'The commit the agent''s branch left its base at, resolved when the round was taken so that a base moving afterwards cannot change what this round says.';
-- Set comment to column: "snapshot_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."snapshot_sha" IS 'A commit written out of band over the agent''s whole worktree, tracked and untracked alike, held by a ref under refs/weave/rounds so it survives collection. The agent''s own index, worktree and branch are untouched by it.';
-- Create "reviews" table
CREATE TABLE "reviews" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "round_id" uuid NOT NULL,
  "author_kind" "review_author_kind" NOT NULL,
  "author_agent_id" uuid NULL,
  "verdict" "review_verdict" NULL,
  "released_at" timestamptz NULL,
  "path" text NOT NULL DEFAULT '',
  "doorbell_mail_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "reviews_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "reviews_author_agent_id_fkey" FOREIGN KEY ("author_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "reviews_doorbell_mail_id_fkey" FOREIGN KEY ("doorbell_mail_id") REFERENCES "mail" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "reviews_round_id_fkey" FOREIGN KEY ("round_id") REFERENCES "review_rounds" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "reviews_doorbell_mail_id" to table: "reviews"
CREATE INDEX "reviews_doorbell_mail_id" ON "reviews" ("doorbell_mail_id");
-- Create index "reviews_open_agent" to table: "reviews"
CREATE UNIQUE INDEX "reviews_open_agent" ON "reviews" ("agent_id", "author_agent_id") WHERE ((released_at IS NULL) AND (author_kind = 'agent'::review_author_kind));
-- Create index "reviews_open_human" to table: "reviews"
CREATE UNIQUE INDEX "reviews_open_human" ON "reviews" ("agent_id") WHERE ((released_at IS NULL) AND (author_kind = 'human'::review_author_kind));
-- Create index "reviews_round_id" to table: "reviews"
CREATE INDEX "reviews_round_id" ON "reviews" ("round_id", "id");
-- Set comment to table: "reviews"
COMMENT ON TABLE "reviews" IS 'One author''s batch of comments against one round, private while it is being written and released as a whole.';
-- Set comment to column: "agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."agent_id" IS 'The agent being reviewed, carried here as well as on the round so that one open review per author can be enforced without a join.';
-- Set comment to column: "author_kind" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_kind" IS 'The type of the author giving the review.';
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when the person reviewing is. Null does not itself mean the person, author_kind says that.';
-- Set comment to column: "verdict" on table: "reviews"
COMMENT ON COLUMN "reviews"."verdict" IS 'What the author concluded, null while the review is still open. It is what tells the agent whether it is done, and what gates opening a pull request.';
-- Set comment to column: "released_at" on table: "reviews"
COMMENT ON COLUMN "reviews"."released_at" IS 'When the review was let go of, null while it is still being written. Nothing in it reaches the agent before this is stamped.';
-- Set comment to column: "path" on table: "reviews"
COMMENT ON COLUMN "reviews"."path" IS 'Where the released review was written for the agent to read, relative to its worktree, empty while the review is still open.';
-- Set comment to column: "doorbell_mail_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."doorbell_mail_id" IS 'The mail that told the agent the review was waiting. It is set null rather than cascaded when that mail is deleted, which is what makes a review the agent threw the notice away for read as undelivered.';
-- Set comment to index: "reviews_doorbell_mail_id" on table: "reviews"
COMMENT ON INDEX "reviews_doorbell_mail_id" IS 'Finds the review a piece of mail is the doorbell for, which is what the foreign key walks when that mail is deleted.';
-- Set comment to index: "reviews_open_agent" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent" IS 'One open review at a time for each agent reviewing, so a reviewer cannot draft two at once.';
-- Set comment to index: "reviews_open_human" on table: "reviews"
COMMENT ON INDEX "reviews_open_human" IS 'One open review at a time for the person, the way a forge holds one pending review per reviewer.';
-- Modify "review_messages" table
ALTER TABLE "review_messages" DROP COLUMN "sent_at", DROP COLUMN "doorbell_mail_id", ADD COLUMN "review_id" uuid NULL, ADD COLUMN "suggestion" text[] NOT NULL DEFAULT '{}', ADD CONSTRAINT "review_messages_review_id_fkey" FOREIGN KEY ("review_id") REFERENCES "reviews" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "review_messages_review_id" to table: "review_messages"
CREATE INDEX "review_messages_review_id" ON "review_messages" ("review_id");
-- Set comment to table: "review_messages"
COMMENT ON TABLE "review_messages" IS 'One message in a review thread, written either by the author giving the review or by the agent answering it.';
-- Set comment to column: "review_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."review_id" IS 'The review this message went out with, null for an answer, which belongs to no batch and reaches the reviewer as soon as it is written.';
-- Set comment to column: "suggestion" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."suggestion" IS 'Replacement lines the author is proposing for the range the thread is pinned to, empty for an ordinary comment. The agent applies them rather than reasoning about them.';
-- Threads and marks made before rounds existed are pinned to nothing a round can name, so they
-- go rather than being carried forward.
DELETE FROM "review_threads";
DELETE FROM "review_viewed_files";
-- The notices raised for those threads would send an agent to look up a review that is no longer
-- there, so they go with the threads they were about.
DELETE FROM "mail"
WHERE "kind" = 'review_activity'
    AND "claimed_at" IS NULL;
-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "review_id" uuid NOT NULL, ADD CONSTRAINT "review_threads_review_id_fkey" FOREIGN KEY ("review_id") REFERENCES "reviews" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "review_threads_review_id" to table: "review_threads"
CREATE INDEX "review_threads_review_id" ON "review_threads" ("review_id", "id");
-- Set comment to table: "review_threads"
COMMENT ON TABLE "review_threads" IS 'One conversation pinned to a place in the round its review was given against.';
-- Set comment to column: "agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_id" IS 'The agent being reviewed, carried here as well as on the review since neither ever moves and almost every read is by agent.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They say what the thread was about once the agent has rewritten that code, and are what a later round is searched for to decide whether the thread has gone outdated.';
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'When the person reviewing last read this thread, stamped from last_agent_activity_at rather than from the clock, so a reply written mid-read is not marked read without being shown.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, null until it has written one. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see.';
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When the person reviewing settled this thread, null while it is still owed an answer. Settling stays theirs even for a thread an agent opened, since deciding the work is done is not the reviewer''s call.';
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_pkey", DROP COLUMN "agent_id", DROP COLUMN "fingerprint", ADD COLUMN "round_id" uuid NOT NULL, ADD PRIMARY KEY ("round_id", "file_path"), ADD CONSTRAINT "review_viewed_files_round_id_fkey" FOREIGN KEY ("round_id") REFERENCES "review_rounds" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to table: "review_viewed_files"
COMMENT ON TABLE "review_viewed_files" IS 'One file of a round the person reviewing has read. A later round is a different row, which is what makes an agent rewriting the file undo the mark.';

SELECT
    attach_updated_at_triggers();
