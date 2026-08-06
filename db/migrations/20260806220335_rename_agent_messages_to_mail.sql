-- Modify "agent_messages" table
ALTER TABLE "agent_messages" DROP CONSTRAINT "agent_messages_from_agent_id_fkey", DROP CONSTRAINT "agent_messages_to_agent_id_fkey";
-- Create "mail" table
CREATE TABLE "mail" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "from_agent_id" uuid NULL,
  "from_name" text NOT NULL DEFAULT '',
  "to_agent_id" uuid NOT NULL,
  "body" text NOT NULL,
  "reply_to" uuid NULL,
  "hops" integer NOT NULL DEFAULT 1,
  "claimed_at" timestamptz NULL,
  "reported_at" timestamptz NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mail_reply_to_fkey" FOREIGN KEY ("reply_to") REFERENCES "mail" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "mail_from_agent_id" to table: "mail"
CREATE INDEX "mail_from_agent_id" ON "mail" ("from_agent_id");
-- Create index "mail_pending" to table: "mail"
CREATE INDEX "mail_pending" ON "mail" ("to_agent_id", "id") WHERE (claimed_at IS NULL);
-- Create index "mail_reply_to" to table: "mail"
CREATE INDEX "mail_reply_to" ON "mail" ("reply_to");
-- Set comment to table: "mail"
COMMENT ON TABLE "mail" IS 'One piece of mail from one agent to another, held until the recipient claims it.';
-- Modify "mail" table
ALTER TABLE "mail" ADD CONSTRAINT "mail_from_agent_id_fkey" FOREIGN KEY ("from_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "mail_to_agent_id_fkey" FOREIGN KEY ("to_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop "agent_messages" table
DROP TABLE "agent_messages";
