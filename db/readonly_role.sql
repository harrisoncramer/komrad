DO $$
DECLARE
    role_name text := current_setting('weave.readonly_role');
    role_pass text := current_setting('weave.readonly_password');
    owner_name text := current_user;
BEGIN
    BEGIN
        EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', role_name, role_pass);
    EXCEPTION
        WHEN duplicate_object THEN
            EXECUTE format('ALTER ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', role_name, role_pass);
    END;

    EXECUTE format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), role_name);
    EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', role_name);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', role_name);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO %I', owner_name, role_name);
END
$$;
