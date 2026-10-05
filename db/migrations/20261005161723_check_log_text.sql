-- Modify "pull_request_check_logs" table
ALTER TABLE "pull_request_check_logs" DROP CONSTRAINT "pull_request_check_logs_outcome_known", ADD CONSTRAINT "pull_request_check_logs_outcome_known" CHECK (outcome = ANY (ARRAY['fetched'::text, 'too_large'::text, 'missing'::text, 'expired'::text, 'failed'::text])), DROP COLUMN "split", DROP COLUMN "truncated", ADD COLUMN "text" text NOT NULL DEFAULT '', ADD COLUMN "error_count" integer NOT NULL DEFAULT 0;
-- Set comment to table: "pull_request_check_logs"
COMMENT ON TABLE "pull_request_check_logs" IS 'The log of a check''s Actions job as komrad last fetched it, one row per check, kept whole and cleaned.';
-- Set comment to column: "outcome" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."outcome" IS 'What the forge answered: fetched when it gave the log, too_large when the log ran past the most komrad keeps, missing when it never had one ready, expired when it no longer keeps it, failed when asking went wrong.';
-- Set comment to column: "line_count" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."line_count" IS 'How many lines the log holds, zero unless it was fetched.';
-- Set comment to column: "text" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."text" IS 'The whole log, cleaned of timestamps and escape codes, empty unless it was fetched.';
-- Set comment to column: "error_count" on table: "pull_request_check_logs"
COMMENT ON COLUMN "pull_request_check_logs"."error_count" IS 'How many of the log''s lines the job marked as errors.';
-- Create "pull_request_check_log_steps" table
CREATE TABLE "pull_request_check_log_steps" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "log_id" uuid NOT NULL,
  "number" integer NOT NULL,
  "name" text NOT NULL,
  "conclusion" text NOT NULL DEFAULT '',
  "first_line" integer NOT NULL,
  "line_count" integer NOT NULL,
  "error_count" integer NOT NULL DEFAULT 0,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_request_check_log_steps_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "pull_request_check_log_steps_number_key" UNIQUE ("engine_id", "log_id", "number"),
  CONSTRAINT "pull_request_check_log_steps_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "pull_request_check_log_steps_log_id_fkey" FOREIGN KEY ("engine_id", "log_id") REFERENCES "pull_request_check_logs" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "pull_request_check_log_steps"
COMMENT ON TABLE "pull_request_check_log_steps" IS 'Where one step of a check run sits in the check''s log, as the steps stood when the log was fetched.';
-- Set comment to column: "log_id" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."log_id" IS 'The log the step sits in.';
-- Set comment to column: "number" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."number" IS 'Where the step falls in its check run, counting from one.';
-- Set comment to column: "name" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."name" IS 'The step''s name when the log was fetched.';
-- Set comment to column: "conclusion" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."conclusion" IS 'How the step ended when the log was fetched, in the forge''s own word lowercased.';
-- Set comment to column: "first_line" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."first_line" IS 'The log line the step starts on, counting from one.';
-- Set comment to column: "line_count" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."line_count" IS 'How many lines of the log the step printed.';
-- Set comment to column: "error_count" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."error_count" IS 'How many of the step''s lines the job marked as errors.';
-- Set comment to column: "engine_id" on table: "pull_request_check_log_steps"
COMMENT ON COLUMN "pull_request_check_log_steps"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Drop "pull_request_check_log_parts" table
DROP TABLE "pull_request_check_log_parts";
