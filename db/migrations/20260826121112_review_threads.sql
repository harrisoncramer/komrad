-- Create enum type "review_author_kind"
CREATE TYPE "review_author_kind" AS ENUM ('human', 'agent');
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "kind" text NULL;
-- Create index "mail_pending_kind" to table: "mail"
CREATE UNIQUE INDEX "mail_pending_kind" ON "mail" ("to_agent_id", "kind") WHERE ((claimed_at IS NULL) AND (kind IS NOT NULL));
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'What sort of notice this is, for the mail weave sends itself and only wants one of waiting at a time, such as the review doorbell. Null is ordinary mail, of which an agent may hold many.';
-- Set comment to index: "mail_pending_kind" on table: "mail"
COMMENT ON INDEX "mail_pending_kind" IS 'At most one unclaimed notice of a kind per agent, so a hundred edits to a review do not queue a hundred doorbells the agent has to read through.';
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
  "resolved_at" timestamptz NULL,
  "last_human_activity_at" timestamptz NULL,
  "last_agent_activity_at" timestamptz NULL,
  "agent_read_at" timestamptz NULL,
  "human_read_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "review_threads_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "review_threads_agent_id" to table: "review_threads"
CREATE INDEX "review_threads_agent_id" ON "review_threads" ("agent_id", "file_path", "new_start", "old_start");
-- Set comment to table: "review_threads"
COMMENT ON TABLE "review_threads" IS 'One conversation pinned to a place in an agent''s diff.';
-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it: relative to the repository root, where the change left it if it moved it.';
-- Set comment to column: "old_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_start" IS 'First line of the range on the old side of the diff, or zero for a thread pinned to lines that exist only on the new side.';
-- Set comment to column: "old_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_end" IS 'Last line of the range on the old side, equal to old_start for a thread on a single line.';
-- Set comment to column: "new_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_start" IS 'First line of the range on the new side of the diff, or zero for a thread pinned to lines the change deleted.';
-- Set comment to column: "new_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_end" IS 'Last line of the range on the new side, equal to new_start for a thread on a single line.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They say what the thread was about once the agent has rewritten that code, and are what the viewer compares the file against to decide whether the anchor still holds.';
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When the thread was settled. A message written afterwards reopens it, since late activity in a closed thread would otherwise be hidden.';
-- Set comment to column: "last_human_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_human_activity_at" IS 'When the person reviewing last wrote or rewrote a message here, taken from that message''s own timestamp so the watermark the agent reads can equal it exactly. Null means they have said nothing here yet.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, the mirror of last_human_activity_at, so what the person has left to read is read off the thread rather than by searching its messages.';
-- Set comment to column: "agent_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_read_at" IS 'The value of last_human_activity_at the agent was last shown, stamped from that column rather than from the clock, so activity written mid-read stays unread instead of being marked read without being returned.';
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'The value of last_agent_activity_at the person reviewing was last shown, which is what says whether the agent has replied since they opened the thread.';
-- Create "review_messages" table
CREATE TABLE "review_messages" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "thread_id" uuid NOT NULL,
  "author_kind" "review_author_kind" NOT NULL,
  "author_agent_id" uuid NULL,
  "body" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "review_messages_author_agent_id_fkey" FOREIGN KEY ("author_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "review_messages_thread_id_fkey" FOREIGN KEY ("thread_id") REFERENCES "review_threads" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "review_messages_thread_id" to table: "review_messages"
CREATE INDEX "review_messages_thread_id" ON "review_messages" ("thread_id", "id");
-- Set comment to table: "review_messages"
COMMENT ON TABLE "review_messages" IS 'One message in a review thread, written either by the person reviewing or by the agent replying.';
-- Set comment to column: "author_kind" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_kind" IS 'Its own column rather than being read off a null author_agent_id, since deleting an agent nulls that column and would otherwise turn the agent''s reply into the reviewer''s.';
-- Set comment to column: "author_agent_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_agent_id" IS 'The agent that wrote this message, kept as a link to who it was and nulled if that agent is deleted. Null does not mean the person reviewing wrote it; author_kind says that.';
-- Set comment to column: "body" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."body" IS 'What was written, as markdown.';

SELECT
    attach_updated_at_triggers();
