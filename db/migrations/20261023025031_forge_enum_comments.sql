-- Set comment to column: "kind" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."kind" IS 'Which forge the host runs, as a forge.v1.Kind name such as KIND_GITHUB.';
-- Set comment to column: "method" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."method" IS 'How the credential was obtained, as a forge.v1.CredentialMethod name such as CREDENTIAL_METHOD_TOKEN.';
-- Set comment to column: "forge" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."forge" IS 'Which kind of forge the host runs, as a forge.v1.Kind name such as KIND_GITHUB.';
