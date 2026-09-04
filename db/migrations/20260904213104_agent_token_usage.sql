-- Create "agent_token_usage" table
CREATE TABLE "agent_token_usage" (
  "agent_id" uuid NOT NULL,
  "key" text NOT NULL,
  "at" timestamptz NOT NULL,
  "input_tokens" bigint NOT NULL,
  "cache_read_tokens" bigint NOT NULL,
  "cache_write_tokens" bigint NOT NULL,
  "output_tokens" bigint NOT NULL,
  PRIMARY KEY ("agent_id", "key"),
  CONSTRAINT "agent_token_usage_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_token_usage_at" to table: "agent_token_usage"
CREATE INDEX "agent_token_usage_at" ON "agent_token_usage" ("at" DESC);
-- Set comment to table: "agent_token_usage"
COMMENT ON TABLE "agent_token_usage" IS 'One exchange an agent''s CLI reported the token counts for, summed over a window to say which agent is spending.';
