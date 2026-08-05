-- Create "agent_messages" table
CREATE TABLE "agent_messages" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "from_agent_id" uuid NULL,
  "from_name" text NOT NULL DEFAULT '',
  "to_agent_id" uuid NOT NULL,
  "body" text NOT NULL,
  "reply_to" uuid NULL,
  "claimed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_messages_from_agent_id_fkey" FOREIGN KEY ("from_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "agent_messages_reply_to_fkey" FOREIGN KEY ("reply_to") REFERENCES "agent_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "agent_messages_to_agent_id_fkey" FOREIGN KEY ("to_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_messages_from_agent_id" to table: "agent_messages"
CREATE INDEX "agent_messages_from_agent_id" ON "agent_messages" ("from_agent_id");
-- Create index "agent_messages_pending" to table: "agent_messages"
CREATE INDEX "agent_messages_pending" ON "agent_messages" ("to_agent_id", "id") WHERE (claimed_at IS NULL);
-- Create index "agent_messages_reply_to" to table: "agent_messages"
CREATE INDEX "agent_messages_reply_to" ON "agent_messages" ("reply_to");
-- Set comment to table: "agent_messages"
COMMENT ON TABLE "agent_messages" IS 'One message from one agent to another, held until the recipient claims it.';

SELECT attach_updated_at_triggers();
