-- Create text search configuration "english_nostop": english stemming with no stop word list
CREATE TEXT SEARCH DICTIONARY "english_nostop" (TEMPLATE = snowball, Language = english);
CREATE TEXT SEARCH CONFIGURATION "english_nostop" (COPY = english);
ALTER TEXT SEARCH CONFIGURATION "english_nostop" ALTER MAPPING FOR asciiword, asciihword, hword_asciipart, word, hword, hword_part WITH "english_nostop";
-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" DROP COLUMN "tsv_simple", DROP COLUMN "tsv", ADD COLUMN "tsv" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english_nostop'::regconfig, content)) STORED;
-- Create index "agent_transcript_chunks_tsv" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_tsv" ON "agent_transcript_chunks" USING GIN ("tsv");
