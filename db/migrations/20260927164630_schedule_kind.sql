-- Create enum type "schedule_kind"
CREATE TYPE "schedule_kind" AS ENUM ('implementer', 'orchestrator');
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "idempotency_key" text NULL;
-- Create index "mail_idempotency_key" to table: "mail"
CREATE UNIQUE INDEX "mail_idempotency_key" ON "mail" ("engine_id", "idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Set comment to column: "idempotency_key" on table: "mail"
COMMENT ON COLUMN "mail"."idempotency_key" IS 'The key a retried send is recognised by, null on mail sent once.';
-- Modify "schedules" table
ALTER TABLE "schedules" ADD CONSTRAINT "schedules_orchestrator_matches_kind" CHECK ((kind = 'orchestrator'::schedule_kind) = (orchestrator_agent_id IS NOT NULL)), ADD COLUMN "kind" "schedule_kind" NOT NULL DEFAULT 'implementer', ADD COLUMN "orchestrator_agent_id" uuid NULL;
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A recurring firing, holding when to fire and the prompt it launches an implementer with or mails an orchestrator.';
-- Set comment to column: "kind" on table: "schedules"
COMMENT ON COLUMN "schedules"."kind" IS 'Whether a firing launches an implementer into the workflow or mails the prompt to an orchestrator.';
-- Set comment to column: "orchestrator_agent_id" on table: "schedules"
COMMENT ON COLUMN "schedules"."orchestrator_agent_id" IS 'The orchestrator an orchestrator schedule mails.';
