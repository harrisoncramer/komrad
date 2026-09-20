-- Modify "forge_connections" table
ALTER TABLE "forge_connections" ADD COLUMN "throttled_until" timestamptz NULL;
-- Set comment to column: "throttled_until" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."throttled_until" IS 'When the forge said this credential''s rate limit resets, after a refusal or a response whose remaining budget was zero. The pull request sync makes no request through the connection until it passes.';
-- Create "branch_scans" table
CREATE TABLE "branch_scans" (
  "repository_url" text NOT NULL,
  "branch" text NOT NULL,
  "next_page" integer NOT NULL DEFAULT 1,
  "completed_at" timestamptz NULL,
  PRIMARY KEY ("repository_url", "branch")
);
-- Set comment to table: "branch_scans"
COMMENT ON TABLE "branch_scans" IS 'A branch weave has listed the pull requests of on purpose, once, which is what finds a pull request older than the sync''s horizon on a branch an agent adopted.';
-- Set comment to column: "next_page" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."next_page" IS 'The next page not yet written.';
-- Set comment to column: "completed_at" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."completed_at" IS 'When the listing answered its last page, after which the branch is never scanned again.';
-- Create "pull_request_syncs" table
CREATE TABLE "pull_request_syncs" (
  "repository_url" text NOT NULL,
  "updated_through" timestamptz NULL,
  "etag" text NOT NULL DEFAULT '',
  "validated_at" timestamptz NULL,
  "sweep_next_page" integer NULL,
  "sweep_newest" timestamptz NULL,
  "sweep_etag" text NULL,
  "read_at" timestamptz NULL,
  "calls" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("repository_url")
);
-- Set comment to table: "pull_request_syncs"
COMMENT ON TABLE "pull_request_syncs" IS 'Where the pull request sync is for one repository: the high-water mark of the last completed sweep, the sweep in progress if one is, and what the last pass cost.';
-- Set comment to column: "updated_through" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."updated_through" IS 'The newest forge updated_at the last completed sweep covered. Null until one has.';
-- Set comment to column: "etag" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."etag" IS 'The ETag page one answered on the last completed sweep, for the exact query the sync trusts. Empty for a forge that sends none.';
-- Set comment to column: "validated_at" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."validated_at" IS 'The last time page one was read without an ETag and answered with a body, which is the clock for the hourly unconditional re-read. A 304 does not touch it.';
-- Set comment to column: "sweep_next_page" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_next_page" IS 'The next page a sweep in progress has not yet written, null between sweeps.';
-- Set comment to column: "sweep_newest" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_newest" IS 'The updated_at of the first pull request on page one of the sweep in progress, which becomes updated_through when it completes.';
-- Set comment to column: "sweep_etag" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."sweep_etag" IS 'The ETag page one answered for the sweep in progress, promoted to etag when it completes.';
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
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "merged_at" timestamptz NULL,
  "closed_at" timestamptz NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "pull_requests_host_provider_id_key" UNIQUE ("host", "provider_id"),
  CONSTRAINT "pull_requests_url_key" UNIQUE ("url")
);
-- Create index "pull_requests_attribution" to table: "pull_requests"
CREATE INDEX "pull_requests_attribution" ON "pull_requests" ("repository_url", "head_repository_url", "head_branch");
-- Create index "pull_requests_unfinished_head" to table: "pull_requests"
CREATE INDEX "pull_requests_unfinished_head" ON "pull_requests" ("repository_url", "head_branch") WHERE ((merged_at IS NULL) AND (closed_at IS NULL));
-- Set comment to table: "pull_requests"
COMMENT ON TABLE "pull_requests" IS 'A pull request weave has read from a forge, one row per forge identity and none owned by an agent. Finished is derived: merged when merged_at is set, closed when closed_at is set and merged_at is not, open otherwise.';
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
COMMENT ON COLUMN "pull_requests"."state" IS 'The forge''s own word for where it stands, kept verbatim as a cross-check on the timestamps.';
-- Set comment to column: "created_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."created_at" IS 'When the forge says it was opened, which is what orders the pull requests on one branch.';
-- Set comment to column: "updated_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."updated_at" IS 'The forge''s last-change stamp, which is what the sync pages by.';
-- Set comment to column: "first_seen_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."first_seen_at" IS 'When weave first inserted the row.';
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
