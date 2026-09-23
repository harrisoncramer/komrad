DO $$
DECLARE
    engine_name text := CURRENT_SETTING('weave.engine_name');
    engine_password text := CURRENT_SETTING('weave.engine_password');
    login_name text := 'weave_engine_' || engine_name;
    reader_name text := login_name || '_reader';
    engine_tenant uuid;
    mapped uuid;
    login record;
BEGIN
    SELECT
        id INTO engine_tenant
    FROM
        tenants
    WHERE
        name = engine_name;
    IF engine_tenant IS NULL THEN
        INSERT INTO tenants (name)
            VALUES (engine_name)
        RETURNING
            id INTO engine_tenant;
    END IF;
    FOR login IN
    SELECT
        *
    FROM (
        VALUES (login_name, 'weave_engine', FALSE),
            (reader_name, 'weave_reader', TRUE)) AS wanted (role_name, group_name, is_reader)
        LOOP
            IF NOT EXISTS (
                SELECT
                    1
                FROM
                    pg_roles
                WHERE
                    rolname = login.role_name) THEN
            EXECUTE FORMAT('CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', login.role_name, engine_password);
        ELSE
            EXECUTE FORMAT('ALTER ROLE %I PASSWORD %L', login.role_name, engine_password);
        END IF;
            EXECUTE FORMAT('GRANT %I TO %I', login.group_name, login.role_name);
            EXECUTE FORMAT('GRANT CONNECT ON DATABASE %I TO %I', CURRENT_DATABASE(), login.role_name);
            SELECT
                tenant_id INTO mapped
            FROM
                tenant_logins
            WHERE
                ROLE = login.role_name;
            IF mapped IS NULL THEN
                INSERT INTO tenant_logins (ROLE, tenant_id, reader)
                    VALUES (login.role_name, engine_tenant, login.is_reader);
            ELSIF mapped <> engine_tenant THEN
                RAISE EXCEPTION '% already belongs to another engine', login.role_name;
            END IF;
        END LOOP;
    EXECUTE FORMAT('CREATE SCHEMA IF NOT EXISTS %I AUTHORIZATION %I', 'river_' || engine_name, login_name);
END
$$;
