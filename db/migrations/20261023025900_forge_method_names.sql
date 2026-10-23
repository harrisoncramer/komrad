-- Set comment to column: "method" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."method" IS 'How the credential was obtained: token for one a person supplied, rotating-token for a GitLab token komrad rotates, github-app for one minted from the engine''s App.';
