-- Create "pull_request_check_readings" table
CREATE TABLE "pull_request_check_readings" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "pull_request_id" uuid NOT NULL,
  "head_sha" text NOT NULL,
  "blank" boolean NOT NULL,
  "first_read_at" timestamptz NOT NULL,
  "read_at" timestamptz NOT NULL,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_readings_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_check_readings_head_key" UNIQUE ("engine_id", "pull_request_id", "head_sha"),
  CONSTRAINT "pull_request_check_readings_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_check_readings_pull_request_id_fkey" FOREIGN KEY ("engine_id", "pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "pull_request_check_readings_pull_request_id" to table: "pull_request_check_readings"
CREATE INDEX "pull_request_check_readings_pull_request_id" ON "pull_request_check_readings" ("pull_request_id");
-- Set comment to table: "pull_request_check_readings"
COMMENT ON TABLE "pull_request_check_readings" IS 'What the forge last said about the checks on one head commit of a pull request, kept for every head it was read on until the pull request goes.';
-- Set comment to column: "head_sha" on table: "pull_request_check_readings"
COMMENT ON COLUMN "pull_request_check_readings"."head_sha" IS 'The head commit the checks were read on.';
-- Set comment to column: "blank" on table: "pull_request_check_readings"
COMMENT ON COLUMN "pull_request_check_readings"."blank" IS 'Whether the latest read for this head came back without a check list, which leaves the head''s last listed checks in place.';
-- Set comment to column: "first_read_at" on table: "pull_request_check_readings"
COMMENT ON COLUMN "pull_request_check_readings"."first_read_at" IS 'When the checks on this head were first read.';
-- Set comment to column: "read_at" on table: "pull_request_check_readings"
COMMENT ON COLUMN "pull_request_check_readings"."read_at" IS 'When the checks on this head were last read, stamped by every read whether or not anything changed.';
-- Set comment to column: "engine_id" on table: "pull_request_check_readings"
COMMENT ON COLUMN "pull_request_check_readings"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Create "pull_request_checks" table
CREATE TABLE "pull_request_checks" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "reading_id" uuid NOT NULL,
  "provider_id" text NOT NULL,
  "kind" text NOT NULL,
  "source" text NOT NULL,
  "name" text NOT NULL,
  "state" text NOT NULL,
  "conclusion" text NOT NULL DEFAULT '',
  "started_at" timestamptz NULL,
  "finished_at" timestamptz NULL,
  "required" boolean NOT NULL,
  "url" text NOT NULL DEFAULT '',
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_checks_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_checks_provider_id_key" UNIQUE ("engine_id", "reading_id", "provider_id"),
  CONSTRAINT "pull_request_checks_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_checks_reading_id_fkey" FOREIGN KEY ("engine_id", "reading_id") REFERENCES "pull_request_check_readings" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "pull_request_checks_reading_id" to table: "pull_request_checks"
CREATE INDEX "pull_request_checks_reading_id" ON "pull_request_checks" ("reading_id");
-- Set comment to table: "pull_request_checks"
COMMENT ON TABLE "pull_request_checks" IS 'One check the forge listed on a head commit of a pull request, as the latest listing for that head said it stands.';
-- Set comment to column: "reading_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."reading_id" IS 'The reading of the head commit the check was listed on.';
-- Set comment to column: "provider_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."provider_id" IS 'The forge''s own identifier for the check run or commit status, which a rerun replaces.';
-- Set comment to column: "kind" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."kind" IS 'What the forge reported the check as: check_run for a check run, status for an old-style commit status.';
-- Set comment to column: "source" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."source" IS 'The workflow that ran the check, or the app when no workflow did, empty for a commit status.';
-- Set comment to column: "name" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."name" IS 'The check''s name, or a commit status''s context.';
-- Set comment to column: "state" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."state" IS 'Where the check stands, in komrad''s own words: queued or running while in flight, waiting when it cannot start on its own, or completed.';
-- Set comment to column: "conclusion" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."conclusion" IS 'How a completed check ended, in the forge''s own word lowercased, such as success, failure, error, neutral, cancelled, skipped or timed_out, empty until it completes.';
-- Set comment to column: "started_at" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."started_at" IS 'When the check started, null before it has.';
-- Set comment to column: "finished_at" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."finished_at" IS 'When the check completed, null before it has.';
-- Set comment to column: "required" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."required" IS 'Whether the pull request''s base branch requires the check to pass before merging.';
-- Set comment to column: "url" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."url" IS 'The web address of the check''s details or logs, empty when the forge gave none.';
-- Set comment to column: "engine_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Isolate "pull_request_check_readings" by engine
CREATE POLICY engine_isolation ON pull_request_check_readings USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));
ALTER TABLE pull_request_check_readings ENABLE ROW LEVEL SECURITY;
GRANT SELECT, INSERT, UPDATE, DELETE ON pull_request_check_readings TO komrad_engine;
GRANT SELECT ON pull_request_check_readings TO komrad_reader;
-- Isolate "pull_request_checks" by engine
CREATE POLICY engine_isolation ON pull_request_checks USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));
ALTER TABLE pull_request_checks ENABLE ROW LEVEL SECURITY;
GRANT SELECT, INSERT, UPDATE, DELETE ON pull_request_checks TO komrad_engine;
GRANT SELECT ON pull_request_checks TO komrad_reader;
