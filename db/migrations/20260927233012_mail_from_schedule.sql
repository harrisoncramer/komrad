-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "schedule_id" uuid NULL;
-- Set comment to column: "schedule_id" on table: "mail"
COMMENT ON COLUMN "mail"."schedule_id" IS 'The schedule that sent the mail, null on mail no schedule sent. A schedule firing while its last mail is still unread joins that mail rather than adding another.';
