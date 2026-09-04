-- Add value to enum type: "mail_kind"
ALTER TYPE "mail_kind" ADD VALUE 'exchange_capped';
-- Drop index "mail_from_agent_id" from table: "mail"
DROP INDEX "mail_from_agent_id";
-- Modify "mail" table
ALTER TABLE "mail" DROP COLUMN "hops", DROP COLUMN "thread_root", ADD COLUMN "about_agent_id" uuid NULL;
-- Create index "mail_from_agent_id" to table: "mail"
CREATE INDEX "mail_from_agent_id" ON "mail" ("from_agent_id", "to_agent_id");
-- Set comment to column: "about_agent_id" on table: "mail"
COMMENT ON COLUMN "mail"."about_agent_id" IS 'The other agent a notice weave raised is about, null on ordinary mail. It is what holds a recipient to one unread notice per subject rather than one per kind, so an agent capped with a second partner is told about that one too. It carries no foreign key, since the notice has to keep saying which exchange was cut off after the agent it names is gone.';
-- Set comment to index: "mail_from_agent_id" on table: "mail"
COMMENT ON INDEX "mail_from_agent_id" IS 'Counts the mail one pair of agents has exchanged, which is what a send is refused by the size of.';
