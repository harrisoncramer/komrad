-- Create enum type "mail_kind"
CREATE TYPE "mail_kind" AS ENUM ('plain', 'review_activity', 'exchange_capped');
-- Create "mail" table
CREATE TABLE "mail" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "from_agent_id" uuid NULL,
  "from_name" text NOT NULL DEFAULT '',
  "to_agent_id" uuid NOT NULL,
  "body" text NOT NULL,
  "reply_to" uuid NULL,
  "claimed_at" timestamptz NULL,
  "reported_at" timestamptz NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "kind" "mail_kind" NOT NULL DEFAULT 'plain',
  "about_agent_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "mail_from_agent_id_fkey" FOREIGN KEY ("from_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mail_reply_to_fkey" FOREIGN KEY ("reply_to") REFERENCES "mail" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mail_to_agent_id_fkey" FOREIGN KEY ("to_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "mail_from_agent_id" to table: "mail"
CREATE INDEX "mail_from_agent_id" ON "mail" ("from_agent_id", "to_agent_id");
-- Create index "mail_pending" to table: "mail"
CREATE INDEX "mail_pending" ON "mail" ("to_agent_id", "id") WHERE (claimed_at IS NULL);
-- Create index "mail_reply_to" to table: "mail"
CREATE INDEX "mail_reply_to" ON "mail" ("reply_to");
-- Set comment to table: "mail"
COMMENT ON TABLE "mail" IS 'One piece of mail from one agent to another, held until the recipient claims it.';
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'Who wrote the mail and why. Plain is mail an agent sent. The other values are notices komrad raised itself, which the panel draws differently. Komrad also counts unread notices by this value. One unread notice of a kind is enough however much is waiting behind it.';
-- Set comment to column: "about_agent_id" on table: "mail"
COMMENT ON COLUMN "mail"."about_agent_id" IS 'The other agent a komrad notice is about, null on ordinary mail. Counting unread notices by this column gives the recipient one notice per subject rather than one per kind, so an agent that hits the exchange cap with a second partner is told about that partner too. There is no foreign key here on purpose. The notice has to keep naming the agent whose exchange was cut off after that agent is gone.';
-- Set comment to index: "mail_from_agent_id" on table: "mail"
COMMENT ON INDEX "mail_from_agent_id" IS 'Counts the mail two agents have exchanged. A send is refused once that count reaches the cap.';
-- Set comment to index: "mail_pending" on table: "mail"
COMMENT ON INDEX "mail_pending" IS 'Reads the mail an agent has not claimed yet. A send is refused when that is already full, and a further review notice joins the unread one found here.';
