-- Create enum type "document_kind"
CREATE TYPE "document_kind" AS ENUM ('rfc', 'report', 'explainer', 'notes');
-- Create "documents" table
CREATE TABLE "documents" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "name" text NOT NULL,
  "kind" "document_kind" NOT NULL,
  "body" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "documents_agent_id_name_key" UNIQUE ("engine_id", "agent_id", "name"),
  CONSTRAINT "documents_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "documents_agent_id_fkey" FOREIGN KEY ("engine_id", "agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "documents_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Set comment to table: "documents"
COMMENT ON TABLE "documents" IS 'A markdown document an agent wrote for a person to read, kept after its worktree is gone.';
-- Set comment to column: "name" on table: "documents"
COMMENT ON COLUMN "documents"."name" IS 'The title a person reads, unique among one agent''s documents.';
-- Set comment to column: "engine_id" on table: "documents"
COMMENT ON COLUMN "documents"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON documents FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON documents USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));

SELECT attach_updated_at_triggers();
