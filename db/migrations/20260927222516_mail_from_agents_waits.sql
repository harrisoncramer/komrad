-- Modify "mail" table
ALTER TABLE "mail" ALTER COLUMN "expires_at" DROP NOT NULL;
-- Set comment to column: "expires_at" on table: "mail"
COMMENT ON COLUMN "mail"."expires_at" IS 'When the mail stops being deliverable, null on mail from an agent, which waits until it is claimed.';
