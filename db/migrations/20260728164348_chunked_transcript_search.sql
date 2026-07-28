-- Drop index "agent_transcripts_content_trgm" from table: "agent_transcripts"
DROP INDEX "agent_transcripts_content_trgm";
-- Create "agent_transcript_chunks" table
CREATE TABLE "agent_transcript_chunks" (
  "agent_id" uuid NOT NULL,
  "seq" integer NOT NULL,
  "role" text NOT NULL,
  "content" text NOT NULL,
  "content_offset" bigint NOT NULL,
  "tsv" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english'::regconfig, content)) STORED,
  PRIMARY KEY ("agent_id", "seq"),
  CONSTRAINT "agent_transcript_chunks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_transcript_chunks_content_trgm" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_content_trgm" ON "agent_transcript_chunks" USING GIN ("content" gin_trgm_ops);
-- Create index "agent_transcript_chunks_tsv" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_tsv" ON "agent_transcript_chunks" USING GIN ("tsv");
-- Set comment to table: "agent_transcript_chunks"
COMMENT ON TABLE "agent_transcript_chunks" IS 'One turn of an agent''s transcript, word-indexed for search.';
