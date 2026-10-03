-- Create enum type "forge_reply_state"
CREATE TYPE "forge_reply_state" AS ENUM ('draft', 'approved', 'sending', 'sent', 'failed');
-- Create enum type "forge_reply_approver_kind"
CREATE TYPE "forge_reply_approver_kind" AS ENUM ('human');
-- Create "review_forge_replies" table
CREATE TABLE "review_forge_replies" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "thread_id" uuid NOT NULL,
  "state" "forge_reply_state" NOT NULL,
  "author_kind" "review_author_kind" NOT NULL,
  "author_agent_id" uuid NULL,
  "proposed_body" text NULL,
  "body" text NOT NULL,
  "approver_kind" "forge_reply_approver_kind" NULL,
  "approved_at" timestamptz NULL,
  "sending_started_at" timestamptz NULL,
  "sent_at" timestamptz NULL,
  "forge_comment_provider_id" text NULL,
  "error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "review_forge_replies_engine_id_id_key" UNIQUE ("engine_id", "id"),
  CONSTRAINT "review_forge_replies_author_agent_id_fkey" FOREIGN KEY ("engine_id", "author_agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("author_agent_id"),
  CONSTRAINT "review_forge_replies_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "review_forge_replies_thread_id_fkey" FOREIGN KEY ("engine_id", "thread_id") REFERENCES "review_threads" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "review_forge_replies_approval_recorded" CHECK (((state = 'draft'::forge_reply_state) = (approver_kind IS NULL)) AND ((approver_kind IS NULL) = (approved_at IS NULL))),
  CONSTRAINT "review_forge_replies_author_not_forge" CHECK (author_kind = ANY (ARRAY['human'::review_author_kind, 'agent'::review_author_kind])),
  CONSTRAINT "review_forge_replies_body_written" CHECK (body <> ''::text),
  CONSTRAINT "review_forge_replies_failure_recorded" CHECK ((state = 'failed'::forge_reply_state) = (error IS NOT NULL)),
  CONSTRAINT "review_forge_replies_human_skips_draft" CHECK ((author_kind = 'agent'::review_author_kind) OR (state <> 'draft'::forge_reply_state)),
  CONSTRAINT "review_forge_replies_proposal_from_agent" CHECK ((author_kind = 'agent'::review_author_kind) = (proposed_body IS NOT NULL)),
  CONSTRAINT "review_forge_replies_sending_started" CHECK ((state = ANY (ARRAY['draft'::forge_reply_state, 'approved'::forge_reply_state])) OR (sending_started_at IS NOT NULL)),
  CONSTRAINT "review_forge_replies_sent_recorded" CHECK (((state = 'sent'::forge_reply_state) = (sent_at IS NOT NULL)) AND ((state = 'sent'::forge_reply_state) = (forge_comment_provider_id IS NOT NULL)))
);
-- Create index "review_forge_replies_open_proposal" to table: "review_forge_replies"
CREATE UNIQUE INDEX "review_forge_replies_open_proposal" ON "review_forge_replies" ("thread_id") WHERE ((author_kind = 'agent'::review_author_kind) AND (state <> 'sent'::forge_reply_state));
-- Create index "review_forge_replies_sending" to table: "review_forge_replies"
CREATE INDEX "review_forge_replies_sending" ON "review_forge_replies" ("id") WHERE (state = 'sending'::forge_reply_state);
-- Create index "review_forge_replies_thread_forge_comment" to table: "review_forge_replies"
CREATE UNIQUE INDEX "review_forge_replies_thread_forge_comment" ON "review_forge_replies" ("thread_id", "forge_comment_provider_id") WHERE (forge_comment_provider_id IS NOT NULL);
-- Create index "review_forge_replies_thread_id" to table: "review_forge_replies"
CREATE INDEX "review_forge_replies_thread_id" ON "review_forge_replies" ("thread_id", "id");
-- Set comment to table: "review_forge_replies"
COMMENT ON TABLE "review_forge_replies" IS 'A reply komrad posts to the forge on a mirrored thread, from the agent''s proposal or the person''s own words through to the forge comment it became.';
-- Set comment to column: "thread_id" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."thread_id" IS 'The mirrored review thread the reply answers on the forge.';
-- Set comment to column: "state" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."state" IS 'Where the reply is on its way to the forge. A draft waits for the person, an approved reply waits to be posted, and a sending one may already be on the forge.';
-- Set comment to column: "author_kind" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."author_kind" IS 'Whether the person or the agent wrote the reply.';
-- Set comment to column: "author_agent_id" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."author_agent_id" IS 'The agent that proposed the reply, null when the person wrote it or when that agent has since been deleted.';
-- Set comment to column: "proposed_body" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."proposed_body" IS 'The agent''s text as it proposed it, kept so the agent can learn whether its draft went out as written. Null on a reply the person wrote.';
-- Set comment to column: "body" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."body" IS 'The text to post, as markdown, which the person may have edited from the proposal. Once sent it is what the forge received.';
-- Set comment to column: "approver_kind" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."approver_kind" IS 'Who approved the reply for posting, null while it is a draft.';
-- Set comment to column: "approved_at" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."approved_at" IS 'When the reply was approved for posting, null while it is a draft.';
-- Set comment to column: "sending_started_at" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."sending_started_at" IS 'When the latest attempt to post the reply began, null until one has.';
-- Set comment to column: "sent_at" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."sent_at" IS 'When the forge accepted the reply, null until it has.';
-- Set comment to column: "forge_comment_provider_id" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."forge_comment_provider_id" IS 'The forge''s own id for the comment the reply became, which the sync matches so it does not mirror the reply back as a new comment. Null until the forge accepts it.';
-- Set comment to column: "error" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."error" IS 'What the forge said when the latest attempt to post failed, null otherwise.';
-- Set comment to column: "engine_id" on table: "review_forge_replies"
COMMENT ON COLUMN "review_forge_replies"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Set comment to index: "review_forge_replies_open_proposal" on table: "review_forge_replies"
COMMENT ON INDEX "review_forge_replies_open_proposal" IS 'Holds each thread to one agent proposal that has not yet reached the forge.';
-- Set comment to index: "review_forge_replies_sending" on table: "review_forge_replies"
COMMENT ON INDEX "review_forge_replies_sending" IS 'Finds the replies an interrupted send may have left on the forge.';
-- Set comment to index: "review_forge_replies_thread_forge_comment" on table: "review_forge_replies"
COMMENT ON INDEX "review_forge_replies_thread_forge_comment" IS 'Holds each forge comment to one reply that became it.';
