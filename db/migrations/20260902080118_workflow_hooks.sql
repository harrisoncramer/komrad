-- Create "workflow_hooks" table
CREATE TABLE "workflow_hooks" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "workflow_id" uuid NOT NULL,
  "event" text NOT NULL,
  "action" text NOT NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "workflow_hooks_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "workflow_hooks_workflow_id_position_idx" to table: "workflow_hooks"
CREATE INDEX "workflow_hooks_workflow_id_position_idx" ON "workflow_hooks" ("workflow_id", "position");
-- Set comment to table: "workflow_hooks"
COMMENT ON TABLE "workflow_hooks" IS 'The scripts a workflow hangs on one moment in its agents'' lives, run in position order.';
