-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "held" boolean NOT NULL DEFAULT false, ADD COLUMN "accepted_from" uuid NULL, ADD CONSTRAINT "review_messages_engine_id_id_key" UNIQUE ("engine_id", "id"), ADD CONSTRAINT "review_messages_accepted_from_fkey" FOREIGN KEY ("engine_id", "accepted_from") REFERENCES "review_messages" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("accepted_from");
-- Create index "review_messages_accepted_from" to table: "review_messages"
CREATE INDEX "review_messages_accepted_from" ON "review_messages" ("accepted_from");
-- Set comment to column: "held" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."held" IS 'Whether the forge author could not write to the repository when the text was written or last edited, which keeps it from the agent. A held first message holds its whole thread until the person accepts it.';
-- Set comment to column: "accepted_from" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."accepted_from" IS 'The held forge message the person accepted this copy of, which releases that message''s thread to the agent once the copy''s review is sent. Null on everything else.';

CREATE FUNCTION review_thread_root_readable(thread uuid)
    RETURNS boolean
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$
    SELECT
        NOT root.held
        OR EXISTS (
            SELECT
                1
            FROM
                public.review_messages copy
                JOIN public.reviews r ON r.id = copy.review_id
            WHERE
                copy.accepted_from = root.id
                AND r.released_at IS NOT NULL)
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

CREATE POLICY forge_held ON review_messages AS RESTRICTIVE FOR SELECT TO komrad_reader USING (NOT held AND (review_id IS NULL OR author_kind <> 'human' OR EXISTS (SELECT 1 FROM reviews r WHERE r.id = review_messages.review_id AND r.released_at IS NOT NULL)) AND (author_kind = 'agent' OR review_thread_root_readable(thread_id)));
CREATE POLICY forge_held ON review_threads AS RESTRICTIVE FOR SELECT TO komrad_reader USING (EXISTS (SELECT 1 FROM review_messages m WHERE m.thread_id = review_threads.id));
