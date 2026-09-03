-- Create "agent_runs" table
CREATE TABLE "agent_runs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "trigger" text NOT NULL,
  "script" text NOT NULL,
  "log_number" integer NULL,
  "started_at" timestamptz NOT NULL DEFAULT now(),
  "finished_at" timestamptz NULL,
  "exit_code" integer NULL,
  "failure" text NOT NULL DEFAULT '',
  "viewed_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_runs_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_runs_agent_id_started_at" to table: "agent_runs"
CREATE INDEX "agent_runs_agent_id_started_at" ON "agent_runs" ("agent_id", "started_at" DESC);
-- Create index "agent_runs_unread_failures" to table: "agent_runs"
CREATE INDEX "agent_runs_unread_failures" ON "agent_runs" ("agent_id") WHERE ((failure <> ''::text) AND (viewed_at IS NULL));
-- Set comment to table: "agent_runs"
COMMENT ON TABLE "agent_runs" IS 'One run of a script weave fired for an agent, indexing the output it left on disk.';
