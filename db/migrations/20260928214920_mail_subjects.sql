-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'pull_request_changed';
-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'worktree_rebased';
-- Modify "mail" table
ALTER TABLE "mail" DROP COLUMN "about_agent_id", DROP COLUMN "about_document_id", ADD COLUMN "subject_id" uuid NULL;
-- Create index "mail_pending_subject" to table: "mail"
CREATE UNIQUE INDEX "mail_pending_subject" ON "mail" ("to_agent_id", "kind", "subject_id") WHERE ((claimed_at IS NULL) AND (subject_id IS NOT NULL));
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'Who wrote the mail and why. Plain is mail an agent sent. The other values are mail komrad wrote itself, which the panel draws differently. Komrad also counts unread mail by this value. One unread mail of a kind is enough however much is waiting behind it.';
-- Set comment to column: "subject_id" on table: "mail"
COMMENT ON COLUMN "mail"."subject_id" IS 'The thing mail from komrad is about, null on mail an agent sent. Its kind says what sort of thing it is: another agent, a document, a pull request or a worktree. A recipient holds at most one unread mail of a kind about one subject, and later mail rewrites it. There is no foreign key here on purpose. The mail has to keep naming its subject after the subject is gone.';
-- Set comment to index: "mail_pending" on table: "mail"
COMMENT ON INDEX "mail_pending" IS 'Reads the mail an agent has not claimed yet. A send is refused when that is already full, and further review mail joins the unread one found here.';
-- Set comment to index: "mail_pending_subject" on table: "mail"
COMMENT ON INDEX "mail_pending_subject" IS 'Holds a recipient to one unread mail from komrad of a kind about one subject, which later mail rewrites in place.';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "last_known_checks" text NULL, ADD COLUMN "last_known_mergeability" text NULL, ADD COLUMN "last_known_decision" text NULL;
-- Set comment to column: "last_known_checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_checks" IS 'The checks as the forge last reported them when it reported anything, which a blank reading leaves alone. An event compares against this so that a moment without an answer is not taken for a change.';
-- Set comment to column: "last_known_mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_mergeability" IS 'The mergeability as the forge last reported it when it reported anything, which a blank reading leaves alone.';
-- Set comment to column: "last_known_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_decision" IS 'The review decision as the forge last reported it when it reported anything, which a blank reading leaves alone.';
-- Set comment to column: "doorbell_mail_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."doorbell_mail_id" IS 'The mail that told the agent a review was waiting. Deleting that mail clears this column rather than deleting the review, so a review whose mail was thrown away reads as undelivered.';
-- Set comment to index: "reviews_doorbell_mail_id" on table: "reviews"
COMMENT ON INDEX "reviews_doorbell_mail_id" IS 'Finds the review a piece of mail told the agent about. Deleting that mail walks this index.';
