-- Modify "documents" table
ALTER TABLE "documents" ADD COLUMN "version" integer NOT NULL DEFAULT 1;
-- Set comment to column: "version" on table: "documents"
COMMENT ON COLUMN "documents"."version" IS 'How many times the document has been written, which a rewrite names to say what it replaces.';
