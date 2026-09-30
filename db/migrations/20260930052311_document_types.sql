-- Modify "document_versions" table
ALTER TABLE "document_versions" DROP COLUMN "kind", ADD COLUMN "type" text NOT NULL;
-- Set comment to column: "type" on table: "document_versions"
COMMENT ON COLUMN "document_versions"."type" IS 'The name of the document type this version was written as.';
-- Create "document_types" table
CREATE TABLE "document_types" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "context" text NOT NULL,
  "version" integer NOT NULL DEFAULT 1,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "document_types_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "document_types_engine_id_name_key" UNIQUE ("engine_id", "name"),
  CONSTRAINT "document_types_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "document_types_name_given" CHECK (name <> ''::text)
);
-- Set comment to table: "document_types"
COMMENT ON TABLE "document_types" IS 'A sort of document a person defines, with the instructions an agent follows when writing one.';
-- Set comment to column: "name" on table: "document_types"
COMMENT ON COLUMN "document_types"."name" IS 'What agents and people call the type, unique within an engine.';
-- Set comment to column: "context" on table: "document_types"
COMMENT ON COLUMN "document_types"."context" IS 'The markdown an agent reads before writing a document of this type.';
-- Set comment to column: "version" on table: "document_types"
COMMENT ON COLUMN "document_types"."version" IS 'How many times the type has been written, which a rewrite names to say what it replaces.';
-- Set comment to column: "engine_id" on table: "document_types"
COMMENT ON COLUMN "document_types"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "documents" table
ALTER TABLE "documents" DROP COLUMN "kind", ADD COLUMN "type" text NOT NULL, ADD CONSTRAINT "documents_type_fkey" FOREIGN KEY ("engine_id", "type") REFERENCES "document_types" ("engine_id", "name") ON UPDATE CASCADE ON DELETE RESTRICT;
-- Set comment to column: "type" on table: "documents"
COMMENT ON COLUMN "documents"."type" IS 'The name of the document type it was written as.';
-- Drop enum type "document_kind"
DROP TYPE "document_kind";

CREATE TRIGGER stamp_engine BEFORE INSERT ON document_types FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE document_types ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON document_types USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));

SELECT attach_updated_at_triggers();

CREATE OR REPLACE FUNCTION seed_document_types()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO document_types (engine_id, name, context)
        VALUES (NEW.id, 'RFC', $context$A proposal for a change, written before building it.

Number it after the highest-numbered RFC already written, and title it the way the others are, such as "RFC 0012: A short title".

Say why the change is needed and what is wrong today, then how the change works. Include a mockup of any screen it changes.

An RFC lives in the panel as a document. Never write one as a file in the repository.
$context$);
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER seed_document_types AFTER INSERT ON engines FOR EACH ROW EXECUTE FUNCTION seed_document_types();
