-- Create "agent_transcript_sessions" table
CREATE TABLE "agent_transcript_sessions" (
  "id" uuid NOT NULL,
  "agent_id" uuid NOT NULL,
  "session_id" text NOT NULL,
  "synced_offset" bigint NOT NULL DEFAULT 0,
  "synced_at" timestamptz NOT NULL DEFAULT now(),
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_transcript_sessions_agent_id_session_id_key" UNIQUE ("agent_id", "session_id"),
  CONSTRAINT "agent_transcript_sessions_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "agent_transcript_sessions"
COMMENT ON TABLE "agent_transcript_sessions" IS 'One conversation an agent has held, holding how far its log has been copied into the store.';
-- Carry every agent that has already been synced over to a session of its own.
INSERT INTO "agent_transcript_sessions" ("id", "agent_id", "session_id", "synced_offset", "synced_at", "created_at")
SELECT
  gen_random_uuid(),
  a."id",
  a."session_id",
  COALESCE(t."synced_offset", 0),
  COALESCE(t."synced_at", now()),
  now()
FROM "agents" a
LEFT JOIN "agent_transcripts" t ON t."agent_id" = a."id"
WHERE t."agent_id" IS NOT NULL
   OR EXISTS (SELECT 1 FROM "agent_transcript_chunks" c WHERE c."agent_id" = a."id");
-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" ADD COLUMN "session_id" uuid;
UPDATE "agent_transcript_chunks" c
SET "session_id" = s."id"
FROM "agent_transcript_sessions" s
WHERE s."agent_id" = c."agent_id";
ALTER TABLE "agent_transcript_chunks" ALTER COLUMN "session_id" SET NOT NULL;
ALTER TABLE "agent_transcript_chunks" ADD CONSTRAINT "agent_transcript_chunks_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "agent_transcript_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "agent_transcript_chunks_session_id" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_session_id" ON "agent_transcript_chunks" ("session_id");
-- Drop "agent_transcripts" table
DROP TABLE "agent_transcripts";
