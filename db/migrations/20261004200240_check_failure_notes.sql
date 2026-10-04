-- Create "pull_request_check_failure_notes" table
CREATE TABLE "pull_request_check_failure_notes" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "check_id" uuid NOT NULL,
  "finished_at" timestamptz NULL,
  "noted_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_failure_notes_agent_id_fkey" FOREIGN KEY ("engine_id", "agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "pull_request_check_failure_notes_check_id_fkey" FOREIGN KEY ("engine_id", "check_id") REFERENCES "pull_request_checks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "pull_request_check_failure_notes_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "pull_request_check_failure_notes_check_id" to table: "pull_request_check_failure_notes"
CREATE INDEX "pull_request_check_failure_notes_check_id" ON "pull_request_check_failure_notes" ("check_id");
-- Create index "pull_request_check_failure_notes_failure_key" to table: "pull_request_check_failure_notes"
CREATE UNIQUE INDEX "pull_request_check_failure_notes_failure_key" ON "pull_request_check_failure_notes" ("engine_id", "agent_id", "check_id", "finished_at") NULLS NOT DISTINCT;
-- Set comment to table: "pull_request_check_failure_notes"
COMMENT ON TABLE "pull_request_check_failure_notes" IS 'A failed check komrad told an agent about, one row per agent, check and finish time, so a failure is mailed once.';
-- Set comment to column: "agent_id" on table: "pull_request_check_failure_notes"
COMMENT ON COLUMN "pull_request_check_failure_notes"."agent_id" IS 'The agent that was told.';
-- Set comment to column: "check_id" on table: "pull_request_check_failure_notes"
COMMENT ON COLUMN "pull_request_check_failure_notes"."check_id" IS 'The check that failed.';
-- Set comment to column: "finished_at" on table: "pull_request_check_failure_notes"
COMMENT ON COLUMN "pull_request_check_failure_notes"."finished_at" IS 'When the check finished in the failure the agent was told about, so a second failure of the same check is told again. Null when the forge gave no finish time.';
-- Set comment to column: "noted_at" on table: "pull_request_check_failure_notes"
COMMENT ON COLUMN "pull_request_check_failure_notes"."noted_at" IS 'When the agent was told, on the same clock that stamps the mail it was told in.';
-- Set comment to column: "engine_id" on table: "pull_request_check_failure_notes"
COMMENT ON COLUMN "pull_request_check_failure_notes"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
