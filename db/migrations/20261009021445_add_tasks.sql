-- Create "tasks" table
CREATE TABLE "tasks" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "workflow_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "title" text NOT NULL,
  "goal" text NOT NULL DEFAULT '',
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "tasks_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "tasks_engine_id_slug_key" UNIQUE ("engine_id", "slug"),
  CONSTRAINT "tasks_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "tasks_workflow_id_fkey" FOREIGN KEY ("engine_id", "workflow_id") REFERENCES "workflows" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tasks_slug_kebab" CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text)
);
-- Create index "tasks_workflow_id" to table: "tasks"
CREATE INDEX "tasks_workflow_id" ON "tasks" ("workflow_id");
-- Set comment to table: "tasks"
COMMENT ON TABLE "tasks" IS 'One outcome a person wants, which the agents linked to it work toward.';
-- Set comment to column: "slug" on table: "tasks"
COMMENT ON COLUMN "tasks"."slug" IS 'The lowercase kebab-case name agents and people call the task by, unique within an engine and never reused.';
-- Set comment to column: "goal" on table: "tasks"
COMMENT ON COLUMN "tasks"."goal" IS 'What the task is meant to achieve, as markdown.';
-- Set comment to column: "engine_id" on table: "tasks"
COMMENT ON COLUMN "tasks"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "agents_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("task_id") NOT VALID;
-- Create index "agents_task_id" to table: "agents"
CREATE INDEX "agents_task_id" ON "agents" ("task_id");
-- Set comment to column: "task_id" on table: "agents"
COMMENT ON COLUMN "agents"."task_id" IS 'The task the agent works on, null for an orchestrator or once its task is deleted.';
