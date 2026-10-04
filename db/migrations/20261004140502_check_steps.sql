-- Create "pull_request_check_steps" table
CREATE TABLE "pull_request_check_steps" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "check_id" uuid NOT NULL,
  "reading_id" uuid NOT NULL,
  "number" integer NOT NULL,
  "name" text NOT NULL,
  "state" text NOT NULL,
  "conclusion" text NOT NULL DEFAULT '',
  "started_at" timestamptz NULL,
  "finished_at" timestamptz NULL,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_steps_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_check_steps_number_key" UNIQUE ("engine_id", "check_id", "number"),
  CONSTRAINT "pull_request_check_steps_check_id_fkey" FOREIGN KEY ("engine_id", "check_id") REFERENCES "pull_request_checks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "pull_request_check_steps_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_check_steps_reading_id_fkey" FOREIGN KEY ("engine_id", "reading_id") REFERENCES "pull_request_check_readings" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "pull_request_check_steps_reading_id" to table: "pull_request_check_steps"
CREATE INDEX "pull_request_check_steps_reading_id" ON "pull_request_check_steps" ("reading_id");
-- Set comment to table: "pull_request_check_steps"
COMMENT ON TABLE "pull_request_check_steps" IS 'One step of a check run, as the latest listing of the check''s head commit said it stands.';
-- Set comment to column: "check_id" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."check_id" IS 'The check run the step belongs to.';
-- Set comment to column: "reading_id" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."reading_id" IS 'The reading of the head commit the step''s check was listed on.';
-- Set comment to column: "number" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."number" IS 'Where the step falls in its check run, counting from one.';
-- Set comment to column: "name" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."name" IS 'The step''s name.';
-- Set comment to column: "state" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."state" IS 'Where the step stands, in komrad''s own words: queued or running while in flight, waiting when it cannot start on its own, or completed.';
-- Set comment to column: "conclusion" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."conclusion" IS 'How a completed step ended, in the forge''s own word lowercased, such as success, failure, cancelled or skipped, empty until it completes.';
-- Set comment to column: "started_at" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."started_at" IS 'When the step started, null before it has.';
-- Set comment to column: "finished_at" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."finished_at" IS 'When the step completed, null before it has.';
-- Set comment to column: "engine_id" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
