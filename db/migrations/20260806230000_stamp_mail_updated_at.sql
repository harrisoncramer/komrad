-- The rename that created "mail" did not end with the call, so the table carries an
-- updated_at nothing stamps. Attaching is idempotent, so this covers mail and leaves
-- every table that already has its trigger alone.
SELECT attach_updated_at_triggers();
