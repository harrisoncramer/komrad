-- Modify "workflow_event_prompts" table
ALTER TABLE "workflow_event_prompts" ADD COLUMN "muted" boolean NOT NULL DEFAULT false;
-- Set comment to column: "muted" on table: "workflow_event_prompts"
COMMENT ON COLUMN "workflow_event_prompts"."muted" IS 'Whether the event is kept from opening mail of its own.';
