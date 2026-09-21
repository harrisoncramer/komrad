-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" ADD COLUMN "tsv_simple" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, content)) STORED;
-- Create index "agent_transcript_chunks_tsv_simple" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_tsv_simple" ON "agent_transcript_chunks" USING GIN ("tsv_simple");
