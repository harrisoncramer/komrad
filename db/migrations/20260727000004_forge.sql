-- Create "branch_scans" table
CREATE TABLE "branch_scans" (
  "repository_url" text NOT NULL,
  "branch" text NOT NULL,
  "next_cursor" text NOT NULL DEFAULT '',
  "completed_at" timestamptz NULL,
  PRIMARY KEY ("repository_url", "branch")
);
-- Set comment to table: "branch_scans"
COMMENT ON TABLE "branch_scans" IS 'A branch komrade has listed the pull requests of on purpose, once, which is what finds a pull request older than the sync''s horizon on a branch an agent adopted.';
-- Set comment to column: "next_cursor" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."next_cursor" IS 'Where the listing reads its next page from, empty for the first.';
-- Set comment to column: "completed_at" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."completed_at" IS 'When the listing answered its last page, after which the branch is never scanned again.';
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
  "throttled_until" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "forge_connections_host_key" UNIQUE ("host")
);
-- Set comment to table: "forge_connections"
COMMENT ON TABLE "forge_connections" IS 'A forge komrade has been connected to, one row per host, holding who the credential was verified as. The credential itself is a file under komrade''s state directory rather than a column.';
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
-- Set comment to column: "throttled_until" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."throttled_until" IS 'When the forge said this credential''s rate limit resets, after a refusal or a response whose remaining budget was zero. The pull request sync makes no request through the connection until it passes.';
-- Create "pull_request_syncs" table
CREATE TABLE "pull_request_syncs" (
  "repository_url" text NOT NULL,
  "updated_through" timestamptz NULL,
  "listed_at" timestamptz NULL,
  "sweep_cursor" text NULL,
  "sweep_newest" timestamptz NULL,
  "read_at" timestamptz NULL,
  "calls" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("repository_url")
);
-- Set comment to table: "pull_request_syncs"
COMMENT ON TABLE "pull_request_syncs" IS 'Where the pull request sync is for one repository: the high-water mark the changes have been read through, the first sweep in progress if one is, and what the last pass cost.';
-- Set comment to column: "updated_through" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."updated_through" IS 'The newest forge updated_at the changes have been read through, by the first sweep and then by every search since. Null until the first sweep has completed.';
-- Set comment to column: "listed_at" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."listed_at" IS 'The last time page one of the repository''s own listing was read and written, which is the clock for the hourly listing behind the search.';
-- Set comment to column: "sweep_cursor" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_cursor" IS 'Where the first sweep in progress reads its next page from, empty for page one and null between sweeps.';
-- Set comment to column: "sweep_newest" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_newest" IS 'The moment the sweep in progress covers up to, when its page one was read or the newest row''s stamp when the forge''s clock is ahead, which becomes updated_through when it completes.';
-- Set comment to column: "read_at" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."read_at" IS 'The last time any request was made for this repository, whatever it answered.';
-- Set comment to column: "calls" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."calls" IS 'How many requests the most recent pass made for this repository.';
-- Create "pull_requests" table
CREATE TABLE "pull_requests" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "host" text NOT NULL,
  "forge" text NOT NULL,
  "provider_id" text NOT NULL,
  "url" text NOT NULL,
  "repository_url" text NOT NULL,
  "head_repository_url" text NOT NULL,
  "number" integer NOT NULL,
  "title" text NOT NULL,
  "head_branch" text NOT NULL,
  "head_sha" text NOT NULL,
  "base_branch" text NOT NULL,
  "base_sha" text NOT NULL,
  "draft" boolean NOT NULL,
  "state" text NOT NULL,
  "opened_at" timestamptz NOT NULL,
  "changed_at" timestamptz NOT NULL,
  "merged_at" timestamptz NULL,
  "closed_at" timestamptz NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT now(),
  "review_decision" text NULL,
  "decision_changed_at" timestamptz NULL,
  "checks" text NULL,
  "checks_sha" text NULL,
  "checks_read_at" timestamptz NULL,
  "mergeability" text NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_requests_host_provider_id_key" UNIQUE ("host", "provider_id"),
  CONSTRAINT "pull_requests_url_key" UNIQUE ("url")
);
-- Create index "pull_requests_attribution" to table: "pull_requests"
CREATE INDEX "pull_requests_attribution" ON "pull_requests" ("repository_url", "head_repository_url", "head_branch");
-- Create index "pull_requests_unfinished_head" to table: "pull_requests"
CREATE INDEX "pull_requests_unfinished_head" ON "pull_requests" ("repository_url", "head_branch") WHERE ((merged_at IS NULL) AND (closed_at IS NULL));
-- Set comment to table: "pull_requests"
COMMENT ON TABLE "pull_requests" IS 'A pull request komrade has read from a forge, one row per forge identity and none owned by an agent. Finished is derived: merged when merged_at is set, closed when closed_at is set and merged_at is not, open otherwise.';
-- Set comment to column: "host" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."host" IS 'The forge host the repository lives on, such as github.com, taken from the repository URL. Two installations of one forge hand out overlapping identifiers, so the host is part of the identity.';
-- Set comment to column: "forge" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."forge" IS 'Which kind of forge the host runs: github or gitlab.';
-- Set comment to column: "provider_id" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."provider_id" IS 'The forge''s own stable identifier: GitHub''s node_id, GitLab''s global id. A rename on the forge changes the URL and not this.';
-- Set comment to column: "url" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."url" IS 'The web address a person opens. Unique, and a locator rather than the identity.';
-- Set comment to column: "repository_url" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."repository_url" IS 'The canonical URL of the repository the pull request targets.';
-- Set comment to column: "head_repository_url" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."head_repository_url" IS 'The canonical URL of the repository the branch lives in. Empty when the forge no longer has it, which GitHub answers after a fork is deleted.';
-- Set comment to column: "number" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."number" IS 'The per-repository number a person sees: GitHub''s number, GitLab''s iid.';
-- Set comment to column: "state" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."state" IS 'Where the forge says it stands, as OPEN, CLOSED or MERGED, a cross-check on the timestamps.';
-- Set comment to column: "opened_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."opened_at" IS 'When the forge says it was opened, which is what orders the pull requests on one branch.';
-- Set comment to column: "changed_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."changed_at" IS 'The forge''s own last-change stamp, which is what the sync pages by. It is the forge''s clock and not komrade''s, so it is not named updated_at and carries no trigger.';
-- Set comment to column: "first_seen_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."first_seen_at" IS 'When komrade first inserted the row.';
-- Set comment to column: "review_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."review_decision" IS 'Where the forge says the reviews stand, in komrade''s own words: pending, approved or changes_requested, null while it has said nothing.';
-- Set comment to column: "decision_changed_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."decision_changed_at" IS 'The changed_at the review decision was read for, stale once changed_at has moved past it.';
-- Set comment to column: "checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks" IS 'Where the forge says the checks on the head commit stand, in komrade''s own words: pending, passing or failing, null while it reports none. A check finishing does not move changed_at, so the checks are kept fresh by checks_sha and checks_read_at rather than by the change stamp.';
-- Set comment to column: "checks_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_sha" IS 'The head commit the checks were read for, stale once head_sha has moved past it.';
-- Set comment to column: "checks_read_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_read_at" IS 'When the checks and mergeability were last read, which is what paces every re-read.';
-- Set comment to column: "mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."mergeability" IS 'Where the forge says the pull request stands against its base and its branch rules, in komrade''s own words: clean, unstable, blocked, behind or conflicting, null while the forge has not computed it. Read with the checks and kept fresh the same way.';
-- Create "agent_pull_requests" table
CREATE TABLE "agent_pull_requests" (
  "agent_id" uuid NOT NULL,
  "pull_request_id" uuid NOT NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT now(),
  "last_seen_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("agent_id", "pull_request_id"),
  CONSTRAINT "agent_pull_requests_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_pull_requests_pull_request_id_fkey" FOREIGN KEY ("pull_request_id") REFERENCES "pull_requests" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_pull_requests_pull_request_id" to table: "agent_pull_requests"
CREATE INDEX "agent_pull_requests_pull_request_id" ON "agent_pull_requests" ("pull_request_id");
-- Set comment to table: "agent_pull_requests"
COMMENT ON TABLE "agent_pull_requests" IS 'Which agents were seen with which pull requests, one row per pair, never deleted or re-pointed. It is history rather than ownership: the live view derives the association from the branch and never reads this, and the archive reads nothing else.';
-- Set comment to column: "last_seen_at" on table: "agent_pull_requests"
COMMENT ON COLUMN "agent_pull_requests"."last_seen_at" IS 'When the pair was last seen together, moved at most once an hour.';
