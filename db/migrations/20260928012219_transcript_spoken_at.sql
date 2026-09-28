-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" ADD COLUMN "spoken_at" timestamptz NULL;
-- Create index "agent_transcript_chunks_spoken_at" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_spoken_at" ON "agent_transcript_chunks" ("spoken_at");
-- Set comment to column: "spoken_at" on table: "agent_transcript_chunks"
COMMENT ON COLUMN "agent_transcript_chunks"."spoken_at" IS 'When the turn was written, read from its line in the log, null for a turn whose log was gone before it was read with times.';
