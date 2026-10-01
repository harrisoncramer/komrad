-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "notices" text[] NOT NULL DEFAULT '{}';
-- Set comment to column: "notices" on table: "mail"
COMMENT ON COLUMN "mail"."notices" IS 'The notices mail from komrad named, whose workflow prompts were appended to its body. A rewrite keeps those that still hold.';
-- Create "workflow_prompts" table
CREATE TABLE "workflow_prompts" (
  "workflow_id" uuid NOT NULL,
  "notice" text NOT NULL,
  "prompt" text NOT NULL,
  "position" integer NOT NULL,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("workflow_id", "notice"),
  CONSTRAINT "workflow_prompts_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "workflow_prompts_workflow_id_fkey" FOREIGN KEY ("engine_id", "workflow_id") REFERENCES "workflows" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "workflow_prompts"
COMMENT ON TABLE "workflow_prompts" IS 'What a workflow asks of its agents when komrad sends them a notice, appended to that mail.';
-- Set comment to column: "notice" on table: "workflow_prompts"
COMMENT ON COLUMN "workflow_prompts"."notice" IS 'The notice the prompt rides on, such as pull_request.checks_failing.';
-- Set comment to column: "engine_id" on table: "workflow_prompts"
COMMENT ON COLUMN "workflow_prompts"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON workflow_prompts FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE workflow_prompts ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON workflow_prompts USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));
