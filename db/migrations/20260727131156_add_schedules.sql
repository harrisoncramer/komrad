-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "schedule_id" uuid NULL, ADD COLUMN "idempotency_key" text NULL, ALTER COLUMN "workflow_id" SET NOT NULL;
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Create index "agents_schedule_id" to table: "agents"
CREATE INDEX "agents_schedule_id" ON "agents" ("schedule_id");
-- Create "schedules" table
CREATE TABLE "schedules" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "cron" text NOT NULL,
  "timezone" text NOT NULL DEFAULT '',
  "enabled" boolean NOT NULL DEFAULT true,
  "prompt" text NOT NULL DEFAULT '',
  "workflow_id" uuid NOT NULL,
  "is_deleted" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "schedules_name_active" to table: "schedules"
CREATE UNIQUE INDEX "schedules_name_active" ON "schedules" ("name") WHERE (is_deleted = false);
-- Create index "schedules_workflow_id" to table: "schedules"
CREATE INDEX "schedules_workflow_id" ON "schedules" ("workflow_id");
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A cron-driven agent launch: when to fire and what to ask for.';

-- Written by hand: Atlas does not manage triggers, so a new table's updated_at
-- would stay frozen at insert time without this. The function is already
-- defined by the initial migration.
CREATE TRIGGER trg_schedules_updated_at
    BEFORE UPDATE ON "schedules"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at ();
