-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'review_comment';
-- Drop index "reviews_open_human_task" from table: "reviews"
DROP INDEX "reviews_open_human_task";
-- Modify "reviews" table
ALTER TABLE "reviews" DROP COLUMN "doorbell_mail_id";
-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "mail_id" uuid NULL, ADD COLUMN "read_at" timestamptz NULL, ADD COLUMN "read_snapshot_id" uuid NULL, ADD CONSTRAINT "review_messages_mail_id_fkey" FOREIGN KEY ("engine_id", "mail_id") REFERENCES "mail" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("mail_id"), ADD CONSTRAINT "review_messages_read_snapshot_id_fkey" FOREIGN KEY ("engine_id", "read_snapshot_id") REFERENCES "review_snapshots" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("read_snapshot_id");
-- Create index "review_messages_mail_id" to table: "review_messages"
CREATE INDEX "review_messages_mail_id" ON "review_messages" ("mail_id");
-- Set comment to column: "accepted_from" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."accepted_from" IS 'The held forge message the person accepted this copy of, which releases that message''s thread to the agent. Null on everything else.';
-- Set comment to column: "mail_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."mail_id" IS 'The mail that carried a person''s comment to the writer, null on an agent''s or a forge user''s comment. Deleting that mail clears this column and nothing else.';
-- Set comment to column: "read_at" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."read_at" IS 'When the writer read the mail carrying a person''s comment, null until then and on every other comment.';
-- Set comment to column: "read_snapshot_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."read_snapshot_id" IS 'The snapshot taken when the writer read a person''s comment, which an addressed reply is compared against.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" ALTER COLUMN "review_id" DROP NOT NULL, ADD COLUMN "author_kind" "review_author_kind" NULL, ADD COLUMN "author_agent_id" uuid NULL, ADD CONSTRAINT "review_threads_author_agent_id_fkey" FOREIGN KEY ("engine_id", "author_agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("author_agent_id");
UPDATE "review_threads" t SET "author_kind" = v."author_kind", "author_agent_id" = v."author_agent_id" FROM "reviews" v WHERE v."id" = t."review_id";
ALTER TABLE "review_threads" ALTER COLUMN "author_kind" SET NOT NULL;
-- Keep every thread and comment a person wrote and drop only the reviews that held them
UPDATE "review_threads" SET "review_id" = NULL WHERE "review_id" IN (SELECT "id" FROM "reviews" WHERE "author_kind" = 'human');
UPDATE "review_messages" SET "review_id" = NULL WHERE "review_id" IN (SELECT "id" FROM "reviews" WHERE "author_kind" = 'human');
DELETE FROM "reviews" WHERE "author_kind" = 'human';
-- Set comment to column: "review_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."review_id" IS 'The review the thread was opened in, null on a thread a person opened, which belongs to no review.';
-- Set comment to column: "author_kind" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."author_kind" IS 'Who opened the thread.';
-- Set comment to column: "author_agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."author_agent_id" IS 'The agent that opened the thread, null when a person or a forge user did, or once the agent was deleted.';
-- Hide only a held forge message, now that a person's comment is released as it is written
CREATE OR REPLACE FUNCTION review_thread_root_readable(thread uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    SELECT
        NOT root.held
        OR EXISTS (
            SELECT
                1
            FROM
                public.review_messages copy
            WHERE
                copy.accepted_from = root.id)
    FROM (
        SELECT
            m.id,
            m.held
        FROM
            public.review_messages m
        WHERE
            m.thread_id = thread
        ORDER BY
            m.id
        LIMIT 1) root;
$$;
DROP POLICY forge_held ON review_messages;
CREATE POLICY forge_held ON review_messages AS RESTRICTIVE FOR SELECT TO komrad_reader USING (NOT held AND (author_kind = 'agent' OR review_thread_root_readable(thread_id)));
