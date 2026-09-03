DO $$
DECLARE
    role_name text := CURRENT_SETTING('weave.readonly_role');
    role_pass text := CURRENT_SETTING('weave.readonly_password');
    owner_name text := CURRENT_USER;
BEGIN
    IF NOT EXISTS (
        SELECT
            1
        FROM
            pg_roles
        WHERE
            rolname = role_name) THEN
    BEGIN
        EXECUTE FORMAT('CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', role_name, role_pass);
    EXCEPTION
        WHEN duplicate_object THEN
            NULL;
    END;
END IF;
EXECUTE FORMAT('GRANT CONNECT ON DATABASE %I TO %I', CURRENT_DATABASE(), role_name);
EXECUTE FORMAT('GRANT USAGE ON SCHEMA public TO %I', role_name);
EXECUTE FORMAT('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', role_name);
EXECUTE FORMAT('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO %I', owner_name, role_name);
END
$$;
