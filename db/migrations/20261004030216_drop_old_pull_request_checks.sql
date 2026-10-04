-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "latest_checks", DROP COLUMN "known_checks", DROP COLUMN "known_checks_sha";
