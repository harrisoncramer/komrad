-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "cwd" text NOT NULL DEFAULT '', ADD COLUMN "flags" text NOT NULL DEFAULT '';
-- Carry each agent's config onto the agent before the table holding it goes. A
-- diff cannot infer this, so it is written by hand: without it every agent loses
-- the directory a restore relaunches it in.
UPDATE
    "agents"
SET
    "cwd" = "agent_configs"."cwd",
    "flags" = "agent_configs"."flags"
FROM
    "agent_configs"
WHERE
    "agent_configs"."id" = "agents"."agent_config_id";
-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "agent_config_id";
-- Drop "agent_configs" table
DROP TABLE "agent_configs";
