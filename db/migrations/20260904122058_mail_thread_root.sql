-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "thread_root" uuid NULL;
-- Create index "mail_thread_root" to table: "mail"
CREATE INDEX "mail_thread_root" ON "mail" ("thread_root");
-- Set comment to column: "thread_root" on table: "mail"
COMMENT ON COLUMN "mail"."thread_root" IS 'The mail that opened the thread this belongs to, null on the opening mail itself. It carries no foreign key on purpose: deleting the mail a thread started from must not detach what followed it, since the hop count is read across the whole thread.';
-- Set comment to index: "mail_thread_root" on table: "mail"
COMMENT ON INDEX "mail_thread_root" IS 'Reads the whole thread a reply belongs to, which is what the hop count is measured across.';
