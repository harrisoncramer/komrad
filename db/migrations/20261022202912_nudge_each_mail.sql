-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "nudged_at";
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "nudged_at" timestamptz NULL;
-- Set comment to column: "nudged_at" on table: "mail"
COMMENT ON COLUMN "mail"."nudged_at" IS 'When komrad told the recipient this mail was waiting, null until then. A rewrite or the recipient''s next launch clears it, so the recipient is told again.';
