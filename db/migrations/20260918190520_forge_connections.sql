-- Create "forge_connections" table
CREATE TABLE "forge_connections" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "kind" text NOT NULL,
  "host" text NOT NULL,
  "api_url" text NOT NULL DEFAULT '',
  "method" text NOT NULL,
  "login" text NOT NULL,
  "verified_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "forge_connections_host_key" UNIQUE ("host")
);
-- Set comment to table: "forge_connections"
COMMENT ON TABLE "forge_connections" IS 'A forge weave has been connected to, one row per host, holding who the credential was verified as. The credential itself is a file under weave''s state directory rather than a column.';
-- Set comment to column: "kind" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."kind" IS 'Which forge the host runs: github or gitlab.';
-- Set comment to column: "host" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."host" IS 'The host agents'' remotes name, such as github.com or ghe.example.com. It is unique because one machine acts as one identity on one forge.';
-- Set comment to column: "api_url" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."api_url" IS 'Where the forge answers API calls when it is not the public one, such as https://ghe.example.com/api/v3. Empty for github.com and gitlab.com.';
-- Set comment to column: "method" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."method" IS 'How the credential was obtained: token for one a person supplied, github_app for one minted from the engine''s App.';
-- Set comment to column: "login" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."login" IS 'Who the forge said the credential belongs to when it was verified, a user login or an App slug.';
-- Set comment to column: "verified_at" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."verified_at" IS 'When the forge last confirmed the credential. Cleared when the forge refuses it, so the row says the connection needs remaking.';
