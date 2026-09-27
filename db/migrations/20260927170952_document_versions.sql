-- Create "document_versions" table
CREATE TABLE "document_versions" (
  "document_id" uuid NOT NULL,
  "version" integer NOT NULL,
  "name" text NOT NULL,
  "kind" "document_kind" NOT NULL,
  "body" text NOT NULL,
  "by_person" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("document_id", "version"),
  CONSTRAINT "document_versions_document_id_fkey" FOREIGN KEY ("engine_id", "document_id") REFERENCES "documents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "document_versions_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Set comment to table: "document_versions"
COMMENT ON TABLE "document_versions" IS 'Every write of a document as it was written, so a person can step back through it.';
-- Set comment to column: "by_person" on table: "document_versions"
COMMENT ON COLUMN "document_versions"."by_person" IS 'Whether a person wrote this version in the panel rather than the agent that owns the document.';
-- Set comment to column: "engine_id" on table: "document_versions"
COMMENT ON COLUMN "document_versions"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON document_versions FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE document_versions ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON document_versions USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));

INSERT INTO document_versions (document_id, version, name, kind, body, created_at, engine_id)
SELECT
    id,
    version,
    name,
    kind,
    body,
    updated_at,
    engine_id
FROM
    documents;
