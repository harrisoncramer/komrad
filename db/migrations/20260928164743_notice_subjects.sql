-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'pull_request_changed';
-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'worktree_rebased';
-- Modify "mail" table
ALTER TABLE "mail" DROP COLUMN "about_agent_id", DROP COLUMN "about_document_id", ADD COLUMN "subject_id" uuid NULL, ADD COLUMN "subject_state" jsonb NULL;
-- Create index "mail_pending_subject" to table: "mail"
CREATE UNIQUE INDEX "mail_pending_subject" ON "mail" ("to_agent_id", "kind", "subject_id") WHERE ((claimed_at IS NULL) AND (subject_id IS NOT NULL));
-- Set comment to column: "subject_id" on table: "mail"
COMMENT ON COLUMN "mail"."subject_id" IS 'The thing a komrad notice is about, null on ordinary mail. Its kind says what sort of thing it is: another agent, a document, a pull request or a worktree. A recipient holds at most one unread notice of a kind about one subject, and a later notice rewrites it. There is no foreign key here on purpose. The notice has to keep naming its subject after the subject is gone.';
-- Set comment to column: "subject_state" on table: "mail"
COMMENT ON COLUMN "mail"."subject_state" IS 'What a notice last said about its subject, kept so that a later notice about the same subject can say what moved since the recipient last heard.';
-- Set comment to index: "mail_pending_subject" on table: "mail"
COMMENT ON INDEX "mail_pending_subject" IS 'Holds a recipient to one unread notice of a kind about one subject, which a later notice rewrites in place.';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "last_known_checks" text NULL, ADD COLUMN "last_known_mergeability" text NULL, ADD COLUMN "last_known_decision" text NULL;
-- Set comment to column: "last_known_checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_checks" IS 'The checks as the forge last reported them when it reported anything, which a blank reading leaves alone. A notice compares against this so that a moment without an answer is not taken for a change.';
-- Set comment to column: "last_known_mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_mergeability" IS 'The mergeability as the forge last reported it when it reported anything, which a blank reading leaves alone.';
-- Set comment to column: "last_known_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_decision" IS 'The review decision as the forge last reported it when it reported anything, which a blank reading leaves alone.';
