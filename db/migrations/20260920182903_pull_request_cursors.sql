-- Modify "branch_scans" table
ALTER TABLE "branch_scans" ALTER COLUMN "next_page" DROP DEFAULT;
ALTER TABLE "branch_scans" RENAME COLUMN "next_page" TO "next_cursor";
ALTER TABLE "branch_scans" ALTER COLUMN "next_cursor" TYPE text USING '';
ALTER TABLE "branch_scans" ALTER COLUMN "next_cursor" SET DEFAULT '';
-- Set comment to column: "next_cursor" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."next_cursor" IS 'Where the listing reads its next page from, empty for the first.';
-- Modify "pull_request_syncs" table
ALTER TABLE "pull_request_syncs" DROP COLUMN "etag", DROP COLUMN "sweep_etag";
ALTER TABLE "pull_request_syncs" RENAME COLUMN "validated_at" TO "listed_at";
ALTER TABLE "pull_request_syncs" RENAME COLUMN "sweep_next_page" TO "sweep_cursor";
ALTER TABLE "pull_request_syncs" ALTER COLUMN "sweep_cursor" TYPE text USING CASE WHEN "sweep_cursor" IS NULL THEN NULL ELSE '' END;
-- Set comment to table: "pull_request_syncs"
COMMENT ON TABLE "pull_request_syncs" IS 'Where the pull request sync is for one repository: the high-water mark the changes have been read through, the first sweep in progress if one is, and what the last pass cost.';
-- Set comment to column: "updated_through" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."updated_through" IS 'The newest forge updated_at the changes have been read through, by the first sweep and then by every search since. Null until the first sweep has completed.';
-- Set comment to column: "listed_at" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."listed_at" IS 'The last time page one of the repository''s own listing was read and written, which is the clock for the hourly listing behind the search.';
-- Set comment to column: "sweep_newest" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_newest" IS 'The moment the sweep in progress covers up to, when its page one was read or the newest row''s stamp when the forge''s clock is ahead, which becomes updated_through when it completes.';
-- Set comment to column: "sweep_cursor" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_cursor" IS 'Where the first sweep in progress reads its next page from, empty for page one and null between sweeps.';
-- Modify "pull_requests" table
UPDATE "pull_requests" SET "state" = CASE WHEN "merged_at" IS NOT NULL THEN 'MERGED' WHEN "state" = 'closed' THEN 'CLOSED' WHEN "state" IN ('open', 'opened') THEN 'OPEN' ELSE upper("state") END;
-- Set comment to column: "state" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."state" IS 'Where the forge says it stands, as OPEN, CLOSED or MERGED, a cross-check on the timestamps.';
