-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "events" text[] NOT NULL DEFAULT '{}';
-- Set comment to column: "events" on table: "mail"
COMMENT ON COLUMN "mail"."events" IS 'The events mail from komrad named, whose workflow prompts were appended to its body. A rewrite keeps those that still hold.';
-- Create "workflow_event_prompts" table
CREATE TABLE "workflow_event_prompts" (
  "workflow_id" uuid NOT NULL,
  "event" text NOT NULL,
  "prompt" text NOT NULL,
  "position" integer NOT NULL,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("workflow_id", "event"),
  CONSTRAINT "workflow_event_prompts_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "workflow_event_prompts_workflow_id_fkey" FOREIGN KEY ("engine_id", "workflow_id") REFERENCES "workflows" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "workflow_event_prompts"
COMMENT ON TABLE "workflow_event_prompts" IS 'What a workflow asks of its agents when komrad sends them an event, appended to that mail.';
-- Set comment to column: "event" on table: "workflow_event_prompts"
COMMENT ON COLUMN "workflow_event_prompts"."event" IS 'The event the prompt rides on, such as pull_request.checks_failing.';
-- Set comment to column: "engine_id" on table: "workflow_event_prompts"
COMMENT ON COLUMN "workflow_event_prompts"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON workflow_event_prompts FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE workflow_event_prompts ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON workflow_event_prompts USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));
