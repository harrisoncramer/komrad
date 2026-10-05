CREATE SCHEMA identity;

REVOKE ALL ON SCHEMA identity FROM PUBLIC;

CREATE TYPE identity.principal_kind AS ENUM ('person', 'service');

CREATE TYPE identity.provider AS ENUM ('cloudflare_access', 'github', 'google');

CREATE TYPE identity.principal_role AS ENUM ('viewer', 'operator', 'automation');

CREATE TABLE identity.principals (
    id uuid NOT NULL DEFAULT uuidv7(),
    name text NOT NULL,
    kind identity.principal_kind NOT NULL,
    provider identity.provider,
    subject text,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT principals_name_key UNIQUE (name),
    CONSTRAINT principals_provider_subject_key UNIQUE (provider, subject),
    CONSTRAINT principals_name_check CHECK (name ~ '^[a-z][a-z0-9_-]{0,62}$'),
    CONSTRAINT principals_subject_check CHECK (subject <> ''),
    CONSTRAINT principals_identity_check CHECK ((provider IS NULL) = (subject IS NULL)),
    CONSTRAINT principals_person_identity_check CHECK (kind <> 'person' OR provider IS NOT NULL)
);

COMMENT ON TABLE identity.principals IS 'A person or service that may call an engine''s api, which no engine can read.';

COMMENT ON COLUMN identity.principals.provider IS 'The outside provider that vouches for this principal, required of a person.';

COMMENT ON COLUMN identity.principals.subject IS 'The provider''s stable id for this principal, never a display name or an email.';

CREATE TABLE identity.bindings (
    principal_id uuid NOT NULL,
    engine_id uuid NOT NULL,
    role identity.principal_role NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, engine_id),
    CONSTRAINT bindings_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES identity.principals (id) ON DELETE CASCADE,
    CONSTRAINT bindings_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines (id) ON DELETE CASCADE
);

COMMENT ON TABLE identity.bindings IS 'The one role a principal holds on one engine.';

CREATE INDEX bindings_engine_id ON identity.bindings (engine_id);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON identity.principals FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON identity.bindings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();
