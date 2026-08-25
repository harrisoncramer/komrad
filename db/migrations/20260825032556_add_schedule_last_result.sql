-- Modify "schedules" table
ALTER TABLE "schedules" ADD COLUMN "last_result_status" text NULL, ADD COLUMN "last_result_message" text NULL, ADD COLUMN "last_result_at" timestamptz NULL;
