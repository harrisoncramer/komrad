-- Modify "agents" table
ALTER TABLE "agents" ALTER COLUMN "state" SET DEFAULT 'idle';
-- Fold the two states that both meant not running into the one that replaces them.
UPDATE
    "agents"
SET
    "state" = 'idle'
WHERE
    "state" IN ('new', 'done');
