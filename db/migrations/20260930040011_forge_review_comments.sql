-- Create "forge_review_threads" table
CREATE TABLE "forge_review_threads" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "pull_request_id" uuid NOT NULL,
  "provider_id" text NOT NULL,
  "path" text NOT NULL DEFAULT '',
  "line" integer NULL,
  "url" text NOT NULL DEFAULT '',
  "resolved" boolean NOT NULL DEFAULT false,
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
COMMENT ON COLUMN "forge_review_threads"."provider_id" IS 'The forge''s own identifier for the thread: a review thread''s node id, or the comment''s own id for a comment on the change as a whole.';
-- Set comment to column: "path" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."path" IS 'The file the thread was written on, empty for a comment on the change as a whole.';
-- Set comment to column: "line" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."line" IS 'The line of the file the thread was written against, null for a comment on the change as a whole.';
-- Set comment to column: "url" on table: "forge_review_threads"
COMMENT ON COLUMN "forge_review_threads"."url" IS 'The web address of the thread''s first comment on the forge.';
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
  "yours" boolean NOT NULL DEFAULT false,
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
COMMENT ON COLUMN "forge_review_comments"."author_is_bot" IS 'Whether the forge says the author is a bot.';
-- Set comment to column: "yours" on table: "forge_review_comments"
COMMENT ON COLUMN "forge_review_comments"."yours" IS 'Whether the author is the login the forge connection was verified as.';
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
