-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "comments_mark" text NULL, ADD COLUMN "comments_read_at" timestamptz NULL;
-- Set comment to column: "comments_mark" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."comments_mark" IS 'What the forge last said about the pull request''s comments in its cheap check, the thread count and the latest comment''s stamp run together. The comment sync reads the threads again only when this moves.';
-- Set comment to column: "comments_read_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."comments_read_at" IS 'When the comment sync last read the pull request''s review threads whole, which paces the re-read that catches a resolution the cheap check cannot see.';
-- Create "forge_review_threads" table
CREATE TABLE "forge_review_threads" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "pull_request_id" uuid NOT NULL,
  "provider_id" text NOT NULL,
  "path" text NOT NULL DEFAULT '',
  "side" text NOT NULL DEFAULT '',
  "commit_sha" text NOT NULL DEFAULT '',
  "base_sha" text NOT NULL DEFAULT '',
  "start_line" integer NULL,
  "end_line" integer NULL,
  "diff_hunk" text NOT NULL DEFAULT '',
  "quoted_lines" text[] NOT NULL DEFAULT '{}',
  "resolved" boolean NOT NULL DEFAULT false,
  "outdated" boolean NOT NULL DEFAULT false,
  "written_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "forge_review_threads_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "forge_review_threads_provider_id_key" UNIQUE ("engine_id", "pull_request_id", "provider_id"),
  CONSTRAINT "forge_review_threads_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "forge_review_threads_pull_request_id_fkey" FOREIGN KEY ("engine_id", "pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "forge_review_threads_pull_request_id" to table: "forge_review_threads"
CREATE INDEX "forge_review_threads_pull_request_id" ON "forge_review_threads" ("pull_request_id");
-- Set comment to table: "forge_review_threads"
COMMENT ON TABLE "forge_review_threads" IS 'A review thread as the forge holds it on a pull request a live agent is linked to, which the comment sync owns and overwrites. Nothing here reaches an agent, and the reader role has no grant on it.';
-- Set comment to column: "provider_id" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."provider_id" IS 'The forge''s own identifier for the thread: a review thread''s node id on GitHub, a discussion id on GitLab, or the comment''s own id for a comment on the change as a whole.';
-- Set comment to column: "path" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."path" IS 'The file the thread was written on, empty for a comment on the change as a whole.';
-- Set comment to column: "side" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."side" IS 'Which side of the diff the lines were counted on: old, new, or empty for a comment on the change as a whole.';
-- Set comment to column: "commit_sha" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."commit_sha" IS 'The pushed commit the forge pinned the thread to, empty when it named none.';
-- Set comment to column: "base_sha" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."base_sha" IS 'The commit the pull request measured from when the thread was read, which is the other end of the diff the thread was written against.';
-- Set comment to column: "diff_hunk" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."diff_hunk" IS 'The stretch of the diff the forge quotes above the thread, ending on the line it was written against.';
-- Set comment to column: "quoted_lines" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."quoted_lines" IS 'The lines the thread was written against, read out of the diff hunk without their diff markers. The panel places the thread where these lines appear exactly once.';
-- Set comment to column: "outdated" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."outdated" IS 'Whether the forge says the code under the thread has changed since it was written.';
-- Set comment to column: "written_at" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."written_at" IS 'When the thread''s first comment was written on the forge.';
-- Set comment to column: "engine_id" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Create "forge_review_comments" table
CREATE TABLE "forge_review_comments" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "thread_id" uuid NOT NULL,
  "provider_id" text NOT NULL,
  "author_login" text NOT NULL,
  "author_is_bot" boolean NOT NULL DEFAULT false,
  "body" text NOT NULL,
  "written_at" timestamptz NOT NULL,
  "edited_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "forge_review_comments_provider_id_key" UNIQUE ("engine_id", "thread_id", "provider_id"),
  CONSTRAINT "forge_review_comments_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "forge_review_comments_thread_id_fkey" FOREIGN KEY ("engine_id", "thread_id") REFERENCES "forge_review_threads" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "forge_review_comments_thread_id" to table: "forge_review_comments"
CREATE INDEX "forge_review_comments_thread_id" ON "forge_review_comments" ("thread_id");
-- Set comment to table: "forge_review_comments"
COMMENT ON TABLE "forge_review_comments" IS 'One comment in a forge review thread, which the comment sync owns and overwrites. Nothing here reaches an agent, and the reader role has no grant on it.';
-- Set comment to column: "provider_id" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."provider_id" IS 'The forge''s own identifier for the comment.';
-- Set comment to column: "author_login" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."author_login" IS 'The forge login of whoever wrote the comment, empty when the forge no longer knows them.';
-- Set comment to column: "author_is_bot" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."author_is_bot" IS 'Whether the forge says the author is a bot. Logins the config names as bots are recognised when the row is drawn, not here.';
-- Set comment to column: "written_at" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."written_at" IS 'When the comment was written on the forge.';
-- Set comment to column: "edited_at" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."edited_at" IS 'When the comment was last edited on the forge, null while it never has been.';
-- Set comment to column: "engine_id" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON forge_review_threads FOR EACH ROW EXECUTE FUNCTION stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON forge_review_comments FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE forge_review_threads ENABLE ROW LEVEL SECURITY;

ALTER TABLE forge_review_comments ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON forge_review_threads USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));

CREATE POLICY engine_isolation ON forge_review_comments USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));

REVOKE ALL ON forge_review_threads, forge_review_comments FROM komrad_reader;

SELECT attach_updated_at_triggers();
