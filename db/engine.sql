DO $$
DECLARE
    engine_name text := CURRENT_SETTING('komrad.engine_name');
    engine_password text := CURRENT_SETTING('komrad.engine_password');
    login_name text := 'komrad_engine_' || engine_name;
    river_schema text := 'river_' || engine_name;
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
        VALUES (login_name, 'komrad_engine', FALSE),
            (login_name || '_reader', 'komrad_reader', TRUE)) AS wanted (role_name, group_name, is_reader)
        LOOP
            SELECT
                tenant_id INTO mapped
            FROM
                tenant_logins
            WHERE
                ROLE = login.role_name;
            IF mapped IS NOT NULL AND mapped <> engine_tenant THEN
                RAISE EXCEPTION '% already belongs to another engine', login.role_name;
            END IF;
            IF EXISTS (
                SELECT
                    1
                FROM
                    pg_roles
                WHERE
                    rolname = login.role_name) THEN
            IF mapped IS NULL THEN
                RAISE EXCEPTION '% already exists and belongs to no engine, so it is not adopted', login.role_name;
            END IF;
            IF EXISTS (
                SELECT
                    1
                FROM
                    pg_auth_members m
                WHERE
                    m.member = login.role_name::regrole
                    AND m.roleid <> login.group_name::regrole) THEN
            RAISE EXCEPTION '% is a member of a role other than %', login.role_name, login.group_name;
        END IF;
            IF EXISTS (
                SELECT
                    1
                FROM
                    pg_roles
                WHERE
                    rolname = login.role_name
                    AND (rolsuper
                        OR rolbypassrls
                        OR rolreplication
                        OR rolcreaterole
                        OR rolcreatedb)) THEN
            RAISE EXCEPTION '% holds privileges an engine login must not have', login.role_name;
        END IF;
            EXECUTE FORMAT('ALTER ROLE %I LOGIN PASSWORD %L VALID UNTIL %L', login.role_name, engine_password, 'infinity');
            EXECUTE FORMAT('ALTER ROLE %I RESET ALL', login.role_name);
        ELSE
            EXECUTE FORMAT('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L', login.role_name, engine_password);
        END IF;
            EXECUTE FORMAT('GRANT %I TO %I', login.group_name, login.role_name);
            EXECUTE FORMAT('GRANT CONNECT ON DATABASE %I TO %I', CURRENT_DATABASE(), login.role_name);
            IF mapped IS NULL THEN
                INSERT INTO tenant_logins (ROLE, tenant_id, reader)
                    VALUES (login.role_name, engine_tenant, login.is_reader);
            END IF;
        END LOOP;
    IF EXISTS (
        SELECT
            1
        FROM
            pg_namespace
        WHERE
            nspname = river_schema
            AND nspowner <> login_name::regrole) THEN
    RAISE EXCEPTION 'schema % already exists and belongs to another role', river_schema;
END IF;
    EXECUTE FORMAT('GRANT %I TO %I WITH INHERIT FALSE, SET TRUE', login_name, CURRENT_USER);
    EXECUTE FORMAT('CREATE SCHEMA IF NOT EXISTS %I AUTHORIZATION %I', river_schema, login_name);
END
$$;
