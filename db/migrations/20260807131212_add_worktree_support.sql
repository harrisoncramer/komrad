ALTER TABLE "agents"
    ADD COLUMN "branch" text NULL;

CREATE TABLE "workflow_action_configs"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "setup_action" text NOT NULL DEFAULT '',
    "cleanup_action" text NOT NULL DEFAULT '',
    "worktree_enabled" boolean NOT NULL DEFAULT FALSE,
    "worktree_base_branch" text NOT NULL DEFAULT '',
    "worktree_copy_paths" text[] NOT NULL DEFAULT '{}',
    "worktree_symlink_paths" text[] NOT NULL DEFAULT '{}',
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id")
);

COMMENT ON TABLE "workflow_action_configs" IS 'The setup/cleanup actions a workflow''s agents run around their lives, and the worktree config those actions can point at instead.';

INSERT INTO "workflow_action_configs"(
    "id",
    "setup_action",
    "cleanup_action")
SELECT
    "id",
    "setup_action",
    "cleanup_action"
FROM
    "workflows";

ALTER TABLE "workflows"
    ADD COLUMN "action_config_id" uuid;

UPDATE
    "workflows"
SET
    "action_config_id" = "id";

ALTER TABLE "workflows"
    ALTER COLUMN "action_config_id" SET NOT NULL,
    ADD CONSTRAINT "workflows_action_config_id_fkey" FOREIGN KEY ("action_config_id") REFERENCES "workflow_action_configs"("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
    DROP COLUMN "setup_action",
    DROP COLUMN "cleanup_action";

CREATE INDEX "workflows_action_config_id" ON "workflows"("action_config_id");

SELECT
    attach_updated_at_triggers();
