-- Maintain updated_at in the database rather than in each statement.
--
-- Written by hand because this Atlas edition does not model functions or
-- triggers, so they cannot be declared in the desired-state HCL. Every table
-- that carries updated_at gets the same trigger, which means a row's timestamp
-- is correct no matter which query wrote it, including the statements the store
-- issues directly rather than through sqlc.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_sessions_updated_at BEFORE UPDATE ON "sessions"
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_session_configs_updated_at BEFORE UPDATE ON "session_configs"
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_workflows_updated_at BEFORE UPDATE ON "workflows"
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_workflow_configs_updated_at BEFORE UPDATE ON "workflow_configs"
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
