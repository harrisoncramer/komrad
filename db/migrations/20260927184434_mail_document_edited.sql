-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'document_edited';
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "about_document_id" uuid NULL;
-- Set comment to column: "about_document_id" on table: "mail"
COMMENT ON COLUMN "mail"."about_document_id" IS 'The document a komrad notice is about, null on ordinary mail. A person editing the same document again joins the unread notice about it rather than adding another.';
