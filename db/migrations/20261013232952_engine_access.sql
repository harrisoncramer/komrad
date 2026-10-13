-- Create "engine_access" table
CREATE TABLE "engine_access" (
  "engine_id" uuid NOT NULL,
  "email" text NOT NULL,
  "issuer" text NULL,
  "subject" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("engine_id", "email"),
  CONSTRAINT "engine_access_engine_id_issuer_subject_key" UNIQUE ("engine_id", "issuer", "subject"),
  CONSTRAINT "engine_access_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "engine_access_email_lower" CHECK ((email = lower(email)) AND (email <> ''::text)),
  CONSTRAINT "engine_access_signed_in_whole" CHECK (((issuer IS NULL) AND (subject IS NULL)) OR ((issuer <> ''::text) AND (subject <> ''::text)))
);
-- Set comment to table: "engine_access"
COMMENT ON TABLE "engine_access" IS 'The people the owner has let into an engine, granted by email and matched by the provider id their first sign-in recorded.';
-- Set comment to column: "email" on table: "engine_access"
COMMENT ON COLUMN "engine_access"."email" IS 'The email the owner granted access to, in lower case.';
-- Set comment to column: "issuer" on table: "engine_access"
COMMENT ON COLUMN "engine_access"."issuer" IS 'The identity provider that vouched for the person at their first sign-in, null until they sign in.';
-- Set comment to column: "subject" on table: "engine_access"
COMMENT ON COLUMN "engine_access"."subject" IS 'The person''s permanent id at that provider, null until they sign in.';
-- Create "claim_engine_access" function
CREATE FUNCTION "claim_engine_access"(email text, issuer text, subject text) RETURNS boolean
    LANGUAGE sql VOLATILE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    WITH claimed AS (
        UPDATE public.engine_access a
        SET issuer = claim_engine_access.issuer, subject = claim_engine_access.subject
        WHERE a.engine_id = public.current_engine()
            AND a.email = lower(claim_engine_access.email)
            AND a.subject IS NULL
            AND claim_engine_access.issuer <> ''
            AND claim_engine_access.subject <> ''
            AND NOT EXISTS (
                SELECT FROM public.engine_access held
                WHERE held.engine_id = a.engine_id
                    AND held.issuer = claim_engine_access.issuer
                    AND held.subject = claim_engine_access.subject)
        RETURNING 1
    )
    SELECT EXISTS (SELECT FROM claimed);
$$;
-- Create "engine_access_granted" function
CREATE FUNCTION "engine_access_granted"(issuer text, subject text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    SELECT EXISTS (
        SELECT FROM public.engine_access a
        WHERE a.engine_id = public.current_engine()
            AND a.issuer = engine_access_granted.issuer
            AND a.subject = engine_access_granted.subject);
$$;
-- Let only engines call the access functions
REVOKE EXECUTE ON FUNCTION "claim_engine_access"(text, text, text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION "engine_access_granted"(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION "claim_engine_access"(text, text, text) TO komrad_engine;
GRANT EXECUTE ON FUNCTION "engine_access_granted"(text, text) TO komrad_engine;
