-- Modify "pull_request_checks" table
ALTER TABLE "pull_request_checks" ADD COLUMN "job_id" bigint NULL;
-- Set comment to column: "job_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."job_id" IS 'The forge''s own number for the Actions job that ran the check, which its log is fetched by, null for a commit status or a check run no workflow ran.';
-- Create "pull_request_check_logs" table
CREATE TABLE "pull_request_check_logs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "check_id" uuid NOT NULL,
  "fetched_at" timestamptz NOT NULL,
  "outcome" text NOT NULL,
  "split" boolean NOT NULL DEFAULT false,
  "truncated" boolean NOT NULL DEFAULT false,
  "line_count" integer NOT NULL DEFAULT 0,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_logs_check_id_key" UNIQUE ("engine_id", "check_id"),
  CONSTRAINT "pull_request_check_logs_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_check_logs_check_id_fkey" FOREIGN KEY ("engine_id", "check_id") REFERENCES "pull_request_checks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "pull_request_check_logs_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_check_logs_outcome_known" CHECK (outcome = ANY (ARRAY['fetched'::text, 'missing'::text, 'expired'::text, 'failed'::text]))
);
-- Set comment to table: "pull_request_check_logs"
COMMENT ON TABLE "pull_request_check_logs" IS 'The log of a check''s Actions job as komrad last fetched it, one row per check, its text kept split into parts.';
-- Set comment to column: "check_id" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."check_id" IS 'The check whose job the log is of.';
-- Set comment to column: "fetched_at" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."fetched_at" IS 'When komrad last asked the forge for the log.';
-- Set comment to column: "outcome" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."outcome" IS 'What the forge answered: fetched when it gave the log, missing when it never had one ready, expired when it no longer keeps it, failed when asking went wrong.';
-- Set comment to column: "split" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."split" IS 'Whether the log was split into one part per step, rather than kept whole as part 0.';
-- Set comment to column: "truncated" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."truncated" IS 'Whether the log ran past the most komrad keeps, and was cut there.';
-- Set comment to column: "line_count" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."line_count" IS 'How many lines the log holds across its parts, zero when none was fetched.';
-- Set comment to column: "engine_id" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Create "pull_request_check_log_parts" table
CREATE TABLE "pull_request_check_log_parts" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "log_id" uuid NOT NULL,
  "step_number" integer NOT NULL,
  "text" text NOT NULL,
  "line_count" integer NOT NULL,
  "error_count" integer NOT NULL DEFAULT 0,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_log_parts_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_check_log_parts_step_number_key" UNIQUE ("engine_id", "log_id", "step_number"),
  CONSTRAINT "pull_request_check_log_parts_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_check_log_parts_log_id_fkey" FOREIGN KEY ("engine_id", "log_id") REFERENCES "pull_request_check_logs" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "pull_request_check_log_parts"
COMMENT ON TABLE "pull_request_check_log_parts" IS 'The lines of one step of a check''s log, or of the whole log when it could not be split by step.';
-- Set comment to column: "log_id" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."log_id" IS 'The log the part belongs to.';
-- Set comment to column: "step_number" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."step_number" IS 'The number of the step the lines were printed in, or 0 for the whole log when it could not be split.';
-- Set comment to column: "text" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."text" IS 'The part''s lines, cleaned of timestamps and escape codes, joined by newlines.';
-- Set comment to column: "line_count" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."line_count" IS 'How many lines the part holds.';
-- Set comment to column: "error_count" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."error_count" IS 'How many of the part''s lines the job marked as errors.';
-- Set comment to column: "engine_id" on table: "pull_request_check_log_parts"
COMMENT ON COLUMN "pull_request_check_log_parts"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
