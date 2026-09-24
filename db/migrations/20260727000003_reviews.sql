-- Create enum type "review_author_kind"
CREATE TYPE "review_author_kind" AS ENUM ('human', 'agent');
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
COMMENT ON TABLE "review_rounds" IS 'One frozen snapshot of what an agent has written. A review is always given against a round rather than against the working files.';
-- Set comment to column: "ordinal" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."ordinal" IS 'Which round this is for its agent, counting from one. The panel names the round by this number.';
-- Set comment to column: "base_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."base_sha" IS 'The commit the agent''s branch started from. It is resolved once, when the round is taken, so later movement on the base branch cannot change what the round says.';
-- Set comment to column: "snapshot_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."snapshot_sha" IS 'A commit covering the agent''s whole checkout, tracked and untracked files alike. A ref under refs/komrade/rounds keeps it alive so git will not collect it. Writing it leaves the agent''s own branch, index and files alone.';
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
  "body" text NOT NULL DEFAULT '',
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
COMMENT ON TABLE "reviews" IS 'One reviewer''s batch of comments on one round. Nobody else sees it while it is being written, and it is released all at once.';
-- Set comment to column: "agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."agent_id" IS 'The agent being reviewed. The round already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join.';
-- Set comment to column: "author_kind" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_kind" IS 'Whether a person or an agent is giving the review.';
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when a person is giving it. Do not read null as meaning a person, because author_kind is what says that. Deleting the reviewing agent clears this column rather than deleting the review. Reviewer agents are deleted as a matter of course, and the agent they reviewed is still working from what they said.';
-- Set comment to column: "verdict" on table: "reviews"
COMMENT ON COLUMN "reviews"."verdict" IS 'What the reviewer concluded, null while the review is still open. It is how the agent learns whether the work is done, and only an approval lets it open a pull request.';
-- Set comment to column: "released_at" on table: "reviews"
COMMENT ON COLUMN "reviews"."released_at" IS 'When the review was released, null while it is still being written. None of it reaches the agent until this is set.';
-- Set comment to column: "path" on table: "reviews"
COMMENT ON COLUMN "reviews"."path" IS 'The absolute path of the file the released review was written to, empty while the review is still open. The file lives in komrade''s own state directory rather than in the agent''s checkout.';
-- Set comment to column: "doorbell_mail_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."doorbell_mail_id" IS 'The mail that told the agent a review was waiting. Deleting that mail clears this column rather than deleting the review, so a review whose notice was thrown away reads as undelivered.';
-- Set comment to column: "body" on table: "reviews"
COMMENT ON COLUMN "reviews"."body" IS 'What the reviewer said about the change as a whole rather than about any one line, empty when they said nothing. It goes at the top of the released review file, above the line comments, because it is what the agent should read first.';
-- Set comment to index: "reviews_doorbell_mail_id" on table: "reviews"
COMMENT ON INDEX "reviews_doorbell_mail_id" IS 'Finds the review a piece of mail was the notice for. Deleting that mail walks this index.';
-- Set comment to index: "reviews_open_agent" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent" IS 'Holds each reviewing agent to one unreleased review at a time. Postgres counts two nulls in this column as different values, and the index relies on that. If nulls collided, deleting a second reviewer agent would fail, because the foreign key clears this column and would land on the null the first delete left behind.';
-- Set comment to index: "reviews_open_human" on table: "reviews"
COMMENT ON INDEX "reviews_open_human" IS 'Holds the person to one unreleased review at a time, the way GitHub holds one pending review per reviewer.';
-- Create "review_threads" table
CREATE TABLE "review_threads" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "file_path" text NOT NULL,
  "old_start" integer NOT NULL DEFAULT 0,
  "old_end" integer NOT NULL DEFAULT 0,
  "new_start" integer NOT NULL DEFAULT 0,
  "new_end" integer NOT NULL DEFAULT 0,
  "quoted_lines" text[] NOT NULL DEFAULT '{}',
  "human_read_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "last_agent_activity_at" timestamptz NULL,
  "resolved_at" timestamptz NULL,
  "review_id" uuid NOT NULL,
  "round_id" uuid NOT NULL,
  "base_sha" text NOT NULL DEFAULT '',
  "resolved_by_kind" "review_author_kind" NULL,
  "resolved_by_agent_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "review_threads_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "review_threads_resolved_by_agent_id_fkey" FOREIGN KEY ("resolved_by_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "review_threads_review_id_fkey" FOREIGN KEY ("review_id") REFERENCES "reviews" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "review_threads_round_id_fkey" FOREIGN KEY ("round_id") REFERENCES "review_rounds" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "review_threads_agent_id" to table: "review_threads"
CREATE INDEX "review_threads_agent_id" ON "review_threads" ("agent_id", "file_path", "new_start", "old_start");
-- Create index "review_threads_review_id" to table: "review_threads"
CREATE INDEX "review_threads_review_id" ON "review_threads" ("review_id", "id");
-- Set comment to table: "review_threads"
COMMENT ON TABLE "review_threads" IS 'One conversation pinned to a file and a line range in the round its review was given against.';
-- Set comment to column: "agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_id" IS 'The agent being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by agent.';
-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it.';
-- Set comment to column: "old_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_start" IS 'First line of the range on the old side of the diff. Zero means the thread is pinned to lines that exist only on the new side.';
-- Set comment to column: "old_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_end" IS 'Last line of the range on the old side. It equals old_start for a thread on a single line.';
-- Set comment to column: "new_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_start" IS 'First line of the range on the new side of the diff. Zero means the thread is pinned to lines the change deleted.';
-- Set comment to column: "new_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_end" IS 'Last line of the range on the new side. It equals new_start for a thread on a single line.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later round is searched for these lines to decide whether the thread has gone outdated.';
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'When the person reviewing last read this thread. It is copied from last_agent_activity_at rather than taken from the clock, so a reply written while they were reading is not marked read without having been shown.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, null until it has written one. It is a column rather than something read off the messages. An agent that rewrites its own reply is activity the person reviewing has not seen, and comparing against the message''s created_at would miss that.';
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When this thread was settled, null while it is still owed an answer.';
-- Set comment to column: "round_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."round_id" IS 'The round this thread''s line numbers were measured against. It is not read through the review, because a review stays open across several rounds and would name the wrong one.';
-- Set comment to column: "base_sha" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."base_sha" IS 'The commit the old side of this thread''s line numbers was read against. For a thread written on the whole change that is the round''s own base. For a thread written while the diff was narrowed to one round it is the earlier round''s snapshot.';
-- Set comment to column: "resolved_by_kind" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_by_kind" IS 'Whether a person or an agent settled this thread, null while nobody has. Two parties can settle one: the agent that wrote the comment, saying the answer was good enough, and the person, who may settle anything including a thread whose author is gone.';
-- Set comment to column: "resolved_by_agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_by_agent_id" IS 'The agent that settled this thread, null when a person did or when nobody has. Do not read null as meaning a person, because resolved_by_kind is what says that. Deleting that agent clears this column rather than the settling.';
-- Create "review_messages" table
CREATE TABLE "review_messages" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "thread_id" uuid NOT NULL,
  "author_kind" "review_author_kind" NOT NULL,
  "author_agent_id" uuid NULL,
  "body" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "review_id" uuid NULL,
  "suggestion" text[] NOT NULL DEFAULT '{}',
  PRIMARY KEY ("id"),
  CONSTRAINT "review_messages_author_agent_id_fkey" FOREIGN KEY ("author_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "review_messages_review_id_fkey" FOREIGN KEY ("review_id") REFERENCES "reviews" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "review_messages_thread_id_fkey" FOREIGN KEY ("thread_id") REFERENCES "review_threads" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "review_messages_review_id" to table: "review_messages"
CREATE INDEX "review_messages_review_id" ON "review_messages" ("review_id");
-- Create index "review_messages_thread_id" to table: "review_messages"
CREATE INDEX "review_messages_thread_id" ON "review_messages" ("thread_id", "id");
-- Set comment to table: "review_messages"
COMMENT ON TABLE "review_messages" IS 'One message in a review thread, written either by the reviewer or by the agent answering them.';
-- Set comment to column: "author_kind" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_kind" IS 'Whether a person or an agent wrote the message.';
-- Set comment to column: "author_agent_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_agent_id" IS 'The agent that wrote this message, null when a person wrote it or when that agent has since been deleted.';
-- Set comment to column: "body" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."body" IS 'What was written, as markdown.';
-- Set comment to column: "review_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."review_id" IS 'The review this message was released with, null for an answer from the agent. An answer belongs to no batch and reaches the reviewer as soon as it is written.';
-- Set comment to column: "suggestion" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."suggestion" IS 'Replacement lines the reviewer is proposing for the range the thread is pinned to, empty on an ordinary comment. The agent applies them as written rather than treating them as a description of what to do.';
-- Create "review_viewed_files" table
CREATE TABLE "review_viewed_files" (
  "file_path" text NOT NULL,
  "viewed_at" timestamptz NOT NULL DEFAULT now(),
  "agent_id" uuid NOT NULL,
  "fingerprint" text NOT NULL,
  PRIMARY KEY ("agent_id", "file_path"),
  CONSTRAINT "review_viewed_files_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "review_viewed_files"
COMMENT ON TABLE "review_viewed_files" IS 'One file of an agent''s diff the person reviewing has read, and the shape that file had when they read it.';
-- Set comment to column: "file_path" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."file_path" IS 'The file that was read, named the way the diff names it: relative to the repository root, where the change left it if it moved it.';
-- Set comment to column: "fingerprint" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."fingerprint" IS 'A hash of the file''s own slice of the patch as it stood when it was read. The panel hashes the file again on every load and shows it as unread once the two disagree, which is what makes an agent writing to the file undo the mark. It is the diff that is hashed rather than the file, since the hunks change when the base moves under a file nothing has touched.';
