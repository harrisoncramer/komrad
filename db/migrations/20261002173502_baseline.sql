CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

DO $$
BEGIN
    BEGIN
        CREATE ROLE komrad_engine NOLOGIN;
    EXCEPTION
        WHEN duplicate_object OR unique_violation THEN
            NULL;
    END;
    BEGIN
        CREATE ROLE komrad_reader NOLOGIN;
    EXCEPTION
        WHEN duplicate_object OR unique_violation THEN
            NULL;
    END;
END;
$$;

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['komrad_engine', 'komrad_reader']
    LOOP
        IF EXISTS (
            SELECT
                1
            FROM
                pg_roles
            WHERE
                rolname = target
                AND (rolcanlogin
                    OR rolsuper
                    OR rolcreatedb
                    OR rolcreaterole
                    OR rolreplication
                    OR rolbypassrls)) THEN
        EXECUTE FORMAT('ALTER ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', target);
    END IF;
        IF EXISTS (
            SELECT
                1
            FROM
                pg_auth_members
            WHERE
                member = target::regrole) THEN
        RAISE EXCEPTION '% is a member of another role, which every engine login would inherit', target;
    END IF;
END LOOP;
END;
$$;

CREATE TYPE public.agent_role AS ENUM (
    'implementer',
    'reviewer',
    'orchestrator'
);

CREATE TYPE public.agent_sequence AS ENUM (
    'none',
    'rebase',
    'am',
    'merge',
    'cherry_pick',
    'revert'
);

CREATE TYPE public.agent_state AS ENUM (
    'idle',
    'thinking',
    'waiting',
    'setting_up',
    'failed_to_create',
    'failed_to_cleanup',
    'uncertain',
    'errored'
);

CREATE TYPE public.mail_kind AS ENUM (
    'plain',
    'review_activity',
    'exchange_capped',
    'document_edited',
    'pull_request_changed',
    'worktree_rebased',
    'worktree_pushed',
    'worktree_aborted'
);

CREATE TYPE public.review_author_kind AS ENUM (
    'human',
    'agent',
    'forge'
);

CREATE TYPE public.review_verdict AS ENUM (
    'approved',
    'changes_requested',
    'commented'
);

CREATE TYPE public.schedule_kind AS ENUM (
    'implementer',
    'orchestrator'
);

CREATE TEXT SEARCH DICTIONARY public.english_nostop (
    TEMPLATE = pg_catalog.snowball,
    language = 'english' );

CREATE TEXT SEARCH CONFIGURATION public.english_nostop (
    PARSER = pg_catalog."default" );

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR asciiword WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR word WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR numword WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR email WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR url WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR host WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR sfloat WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR version WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR hword_numpart WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR hword_part WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR hword_asciipart WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR numhword WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR asciihword WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR hword WITH public.english_nostop;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR url_path WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR file WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR "float" WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR "int" WITH simple;

ALTER TEXT SEARCH CONFIGURATION public.english_nostop
    ADD MAPPING FOR uint WITH simple;

CREATE TABLE public.agent_pull_requests (
    agent_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    first_seen_at timestamptz DEFAULT now() NOT NULL,
    last_seen_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.agent_pull_requests IS 'Which agents were seen with which pull requests, one row per pair, never re-pointed. The watch carries every pull request with a row here.';

COMMENT ON COLUMN public.agent_pull_requests.last_seen_at IS 'When the pair was last seen together, moved at most once an hour.';

COMMENT ON COLUMN public.agent_pull_requests.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.agent_runs (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    trigger text NOT NULL,
    script text NOT NULL,
    log_number integer,
    started_at timestamptz DEFAULT now() NOT NULL,
    finished_at timestamptz,
    exit_code integer,
    failure text DEFAULT ''::text NOT NULL,
    viewed_at timestamptz,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.agent_runs IS 'One run of a script komrad fired for an agent. The output stays on disk and this row says where to find it.';

COMMENT ON COLUMN public.agent_runs.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.agent_screens (
    agent_id uuid NOT NULL,
    frame text NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    captured_at timestamptz NOT NULL,
    engine_id uuid NOT NULL,
    CONSTRAINT agent_screens_sized CHECK (((width > 0) AND (height > 0)))
);

COMMENT ON TABLE public.agent_screens IS 'The last screen an agent''s pane drew while it was at rest, kept so the panel can show it once the pane is gone.';

COMMENT ON COLUMN public.agent_screens.frame IS 'The pane''s visible grid as tmux captured it, colour escapes included.';

COMMENT ON COLUMN public.agent_screens.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.agent_transcript_chunks (
    agent_id uuid NOT NULL,
    seq integer NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    content_offset bigint NOT NULL,
    session_id uuid NOT NULL,
    tsv tsvector GENERATED ALWAYS AS (to_tsvector('public.english_nostop'::regconfig, content)) STORED NOT NULL,
    engine_id uuid NOT NULL,
    spoken_at timestamptz
);

COMMENT ON TABLE public.agent_transcript_chunks IS 'One turn of an agent''s conversation, indexed so it can be searched by word.';

COMMENT ON COLUMN public.agent_transcript_chunks.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.agent_transcript_chunks.spoken_at IS 'When the turn was written, read from its line in the log, null for a turn whose log was gone before it was read with times.';

CREATE TABLE public.agent_transcript_sessions (
    id uuid NOT NULL,
    agent_id uuid NOT NULL,
    session_id text NOT NULL,
    synced_offset bigint DEFAULT 0 NOT NULL,
    synced_at timestamptz DEFAULT now() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.agent_transcript_sessions IS 'One conversation an agent has held, holding how far its log has been copied into the store.';

COMMENT ON COLUMN public.agent_transcript_sessions.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.agents (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    cwd text DEFAULT ''::text NOT NULL,
    flags text DEFAULT ''::text NOT NULL,
    prompt text DEFAULT ''::text NOT NULL,
    pane_id text DEFAULT ''::text NOT NULL,
    state public.agent_state DEFAULT 'idle'::public.agent_state NOT NULL,
    state_changed_at timestamptz DEFAULT now() NOT NULL,
    harness text DEFAULT 'claude'::text NOT NULL,
    session_id text DEFAULT ''::text NOT NULL,
    workflow_id uuid NOT NULL,
    schedule_id uuid,
    idempotency_key text,
    spawned_by_agent_id uuid,
    archived_at timestamptz,
    is_hidden boolean DEFAULT false NOT NULL,
    nudged_at timestamptz,
    branch text,
    worktree_removed boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    state_seen_at timestamptz DEFAULT now() NOT NULL,
    interrupted_at timestamptz,
    adopted_branch text DEFAULT ''::text NOT NULL,
    parent_agent_id uuid,
    turn_ended_at timestamptz,
    provision_enabled boolean,
    reported_reason text DEFAULT ''::text NOT NULL,
    role public.agent_role DEFAULT 'implementer'::public.agent_role NOT NULL,
    held_at timestamptz,
    held_mid_turn boolean DEFAULT false NOT NULL,
    nudge_sent_at timestamptz,
    repository_url text,
    deleting_at timestamptz,
    alive boolean DEFAULT false NOT NULL,
    engine_id uuid NOT NULL,
    head_sha text,
    moved_since_round text DEFAULT ''::text NOT NULL,
    failure_reason text DEFAULT ''::text NOT NULL,
    last_rebase jsonb,
    archive_batch_id uuid,
    turn_failed_at timestamptz,
    turn_error text DEFAULT ''::text NOT NULL,
    lines_added integer,
    lines_removed integer,
    held_by_agent_id uuid,
    sequence public.agent_sequence DEFAULT 'none'::public.agent_sequence NOT NULL
);

COMMENT ON TABLE public.agents IS 'One background agent, holding everything needed to start it again.';

COMMENT ON COLUMN public.agents.state IS 'What the agent is doing. Komrad captures the agent''s terminal every couple of seconds and writes here what it read there. Uncertain means the terminal was captured but could not be understood. Errored means the terminal is at rest after an API error ended the last turn. The setup and failure values never come from reading a terminal, so something else sets those.';

COMMENT ON COLUMN public.agents.reported_reason IS 'What the agent''s own CLI last said it was doing. It only decorates the state read off the terminal and never sets that state. It is cleared whenever the state changes and when a turn ends, so an empty value means there is nothing to say.';

COMMENT ON COLUMN public.agents.role IS 'What the agent was launched to do. It decides which review briefing the agent is given. A reviewer reads the work of the agent it is a child of and never writes any code of its own.';

COMMENT ON COLUMN public.agents.held_at IS 'When a person stopped this agent and took the tree, null while nobody is holding it. Held is not a state, since the states are decided by reading the pane and would overwrite it.';

COMMENT ON COLUMN public.agents.held_mid_turn IS 'Whether the hold cut a turn that was still running, which is what decides whether letting go has to type the agent back into motion or merely stop holding it.';

COMMENT ON COLUMN public.agents.repository_url IS 'The canonical web address of the repository the agent''s checkout pushes to, such as https://github.com/harrisoncramer/komrad. Null when the checkout is gone or its origin is not a forge. The cwd syncer rewrites it on the same pass that rewrites branch.';

COMMENT ON COLUMN public.agents.deleting_at IS 'When a delete claimed the row. A claimed row is left out of every listing until the delete either removes it or fails, and a failure clears the claim as it records itself. The server clears every claim when it starts, since nothing survives a restart to finish one.';

COMMENT ON COLUMN public.agents.alive IS 'Whether the agent still owns the pane its row names.';

COMMENT ON COLUMN public.agents.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.agents.head_sha IS 'The commit the agent''s checkout has checked out, null when the checkout is gone or git cannot read it. The cwd syncer rewrites it on the same pass that rewrites branch.';

COMMENT ON COLUMN public.agents.moved_since_round IS 'The snapshot of the agent''s newest review round when its worktree no longer hashes to it, empty while it still does or when there is no round. The cwd syncer rewrites it every pass.';

COMMENT ON COLUMN public.agents.failure_reason IS 'Why the agent''s create or cleanup failed, empty unless it is in one of the failed states.';

COMMENT ON COLUMN public.agents.last_rebase IS 'What the latest rebase of the agent''s checkout came to, as the result or the failure; null until its first rebase.';

COMMENT ON COLUMN public.agents.archive_batch_id IS 'The archive this agent was taken out of live listings by, shared by every agent that archive took.';

COMMENT ON COLUMN public.agents.turn_failed_at IS 'When an API error last ended one of the agent''s turns. The last turn failed while this is no older than turn_ended_at.';

COMMENT ON COLUMN public.agents.turn_error IS 'What the agent''s CLI printed when an API error last ended one of its turns.';

COMMENT ON COLUMN public.agents.lines_added IS 'How many lines the agent''s checkout adds since it left its base, committed and uncommitted alike, null when there is no base to measure against. The cwd syncer rewrites it every pass while the agent is live and leaves it as it was once the agent is archived.';

COMMENT ON COLUMN public.agents.lines_removed IS 'How many lines the agent''s checkout removes since it left its base, null whenever lines_added is.';

COMMENT ON COLUMN public.agents.held_by_agent_id IS 'The agent that took the hold, null when the panel took it.';

COMMENT ON COLUMN public.agents.sequence IS 'What git has part-way through the agent''s checkout and waiting on somebody: a rebase, an am, a merge, a cherry-pick or a revert, or none. Komrad reads the checkout every few seconds and writes what it found here, so settling the sequence by hand clears this on its own.';

CREATE TABLE public.document_type_versions (
    document_type_id uuid NOT NULL,
    version integer NOT NULL,
    name text NOT NULL,
    context text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    CONSTRAINT document_type_versions_context_written CHECK ((context <> ''::text))
);

COMMENT ON TABLE public.document_type_versions IS 'Every write of a document type as it was written, so a person can step back through its context.';

COMMENT ON COLUMN public.document_type_versions.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.document_types (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    context text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    CONSTRAINT document_types_name_given CHECK ((name <> ''::text))
);

COMMENT ON TABLE public.document_types IS 'A sort of document a person defines, with the instructions an agent follows when writing one.';

COMMENT ON COLUMN public.document_types.name IS 'What agents and people call the type, unique within an engine.';

COMMENT ON COLUMN public.document_types.context IS 'The markdown an agent reads before writing a document of this type.';

COMMENT ON COLUMN public.document_types.version IS 'How many times the type has been written, which a rewrite names to say what it replaces.';

COMMENT ON COLUMN public.document_types.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.document_versions (
    document_id uuid NOT NULL,
    version integer NOT NULL,
    name text NOT NULL,
    body text NOT NULL,
    by_person boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    type text NOT NULL,
    CONSTRAINT document_versions_body_written CHECK ((body <> ''::text))
);

COMMENT ON TABLE public.document_versions IS 'Every write of a document as it was written, so a person can step back through it.';

COMMENT ON COLUMN public.document_versions.by_person IS 'Whether a person wrote this version in the panel rather than the agent that owns the document.';

COMMENT ON COLUMN public.document_versions.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.document_versions.type IS 'The name of the document type this version was written as.';

CREATE TABLE public.documents (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    name text NOT NULL,
    body text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    type text NOT NULL,
    CONSTRAINT documents_body_written CHECK ((body <> ''::text))
);

COMMENT ON TABLE public.documents IS 'A markdown document an agent wrote for a person to read, kept after its worktree is gone.';

COMMENT ON COLUMN public.documents.name IS 'The title a person reads, unique among one agent''s documents.';

COMMENT ON COLUMN public.documents.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.documents.version IS 'How many times the document has been written, which a rewrite names to say what it replaces.';

COMMENT ON COLUMN public.documents.type IS 'The name of the document type it was written as.';

CREATE TABLE public.engine_logins (
    role text NOT NULL,
    engine_id uuid NOT NULL,
    reader boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

COMMENT ON TABLE public.engine_logins IS 'The Postgres logins an engine connects as, which is the only way an engine''s identity is known.';

COMMENT ON COLUMN public.engine_logins.reader IS 'Whether this is the engine''s read-only login rather than the one it writes with.';

CREATE TABLE public.engines (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

COMMENT ON TABLE public.engines IS 'An engine, the owner of every row it writes.';

CREATE TABLE public.forge_connections (
    id uuid DEFAULT uuidv7() NOT NULL,
    kind text NOT NULL,
    host text NOT NULL,
    api_url text DEFAULT ''::text NOT NULL,
    method text NOT NULL,
    login text NOT NULL,
    verified_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    throttled_until timestamptz,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.forge_connections IS 'A forge komrad has been connected to, one row per host, holding who the credential was verified as. The credential itself is a file under komrad''s state directory rather than a column.';

COMMENT ON COLUMN public.forge_connections.kind IS 'Which forge the host runs: github or gitlab.';

COMMENT ON COLUMN public.forge_connections.host IS 'The host agents'' remotes name, such as github.com or ghe.example.com. It is unique because one machine acts as one identity on one forge.';

COMMENT ON COLUMN public.forge_connections.api_url IS 'Where the forge answers API calls when it is not the public one, such as https://ghe.example.com/api/v3. Empty for github.com and gitlab.com.';

COMMENT ON COLUMN public.forge_connections.method IS 'How the credential was obtained: token for one a person supplied, github_app for one minted from the engine''s App.';

COMMENT ON COLUMN public.forge_connections.login IS 'Who the forge said the credential belongs to when it was verified, a user login or an App slug.';

COMMENT ON COLUMN public.forge_connections.verified_at IS 'When the forge last confirmed the credential. Cleared when the forge refuses it, so the row says the connection needs remaking.';

COMMENT ON COLUMN public.forge_connections.throttled_until IS 'When the forge said this credential''s rate limit resets, after a refusal or a response whose remaining budget was zero. The pull request sync makes no request through the connection until it passes.';

COMMENT ON COLUMN public.forge_connections.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.forge_review_comments (
    id uuid DEFAULT uuidv7() NOT NULL,
    thread_id uuid NOT NULL,
    provider_id text NOT NULL,
    author_login text NOT NULL,
    author_is_bot boolean DEFAULT false NOT NULL,
    yours boolean DEFAULT false NOT NULL,
    body text NOT NULL,
    written_at timestamptz NOT NULL,
    edited_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    trusted boolean DEFAULT false NOT NULL,
    forge_review_id text DEFAULT ''::text NOT NULL
);

COMMENT ON TABLE public.forge_review_comments IS 'One comment in a forge review thread, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it.';

COMMENT ON COLUMN public.forge_review_comments.provider_id IS 'The forge''s own identifier for the comment.';

COMMENT ON COLUMN public.forge_review_comments.author_login IS 'The forge login of whoever wrote the comment, empty when the forge no longer knows them.';

COMMENT ON COLUMN public.forge_review_comments.author_is_bot IS 'Whether the forge says the author is a bot.';

COMMENT ON COLUMN public.forge_review_comments.yours IS 'Whether the author is the login the forge connection was verified as.';

COMMENT ON COLUMN public.forge_review_comments.written_at IS 'When the comment was written on the forge.';

COMMENT ON COLUMN public.forge_review_comments.edited_at IS 'When the comment was last edited on the forge, null while it never has been.';

COMMENT ON COLUMN public.forge_review_comments.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.forge_review_comments.trusted IS 'Whether the forge says the author may write to the repository, which releases their comments to the agent without the person sending them.';

COMMENT ON COLUMN public.forge_review_comments.forge_review_id IS 'The forge''s identifier for the review the comment was submitted in, empty when it names none.';

CREATE TABLE public.forge_review_threads (
    id uuid DEFAULT uuidv7() NOT NULL,
    pull_request_id uuid NOT NULL,
    provider_id text NOT NULL,
    path text DEFAULT ''::text NOT NULL,
    line integer,
    url text DEFAULT ''::text NOT NULL,
    resolved boolean DEFAULT false NOT NULL,
    written_at timestamptz NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    old_side boolean DEFAULT false NOT NULL,
    commit_sha text DEFAULT ''::text NOT NULL,
    quote text DEFAULT ''::text NOT NULL
);

COMMENT ON TABLE public.forge_review_threads IS 'A review thread as the forge holds it on a pull request a live agent is linked to, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it.';

COMMENT ON COLUMN public.forge_review_threads.provider_id IS 'The forge''s own identifier for the thread: a review thread''s node id, or the comment''s own id for a comment on the change as a whole.';

COMMENT ON COLUMN public.forge_review_threads.path IS 'The file the thread was written on, empty for a comment on the change as a whole.';

COMMENT ON COLUMN public.forge_review_threads.line IS 'The line of the file the thread was written against, counted in commit_sha on the side old_side names, null for a comment on the change as a whole or on a file.';

COMMENT ON COLUMN public.forge_review_threads.url IS 'The web address of the thread''s first comment on the forge.';

COMMENT ON COLUMN public.forge_review_threads.written_at IS 'When the thread''s first comment was written on the forge.';

COMMENT ON COLUMN public.forge_review_threads.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.forge_review_threads.old_side IS 'Whether the line is counted on the old side of the forge''s diff rather than the new one.';

COMMENT ON COLUMN public.forge_review_threads.commit_sha IS 'The commit the forge counted the line in, empty when it names none.';

COMMENT ON COLUMN public.forge_review_threads.quote IS 'The text of the line the thread was written on as the forge''s diff drew it, with its diff marker taken off.';

CREATE TABLE public.mail (
    id uuid DEFAULT uuidv7() NOT NULL,
    from_agent_id uuid,
    from_name text DEFAULT ''::text NOT NULL,
    to_agent_id uuid NOT NULL,
    body text NOT NULL,
    reply_to uuid,
    claimed_at timestamptz,
    reported_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    kind public.mail_kind DEFAULT 'plain'::public.mail_kind NOT NULL,
    engine_id uuid NOT NULL,
    idempotency_key text,
    subject_id uuid,
    events text[] DEFAULT '{}'::text[] NOT NULL
);

COMMENT ON TABLE public.mail IS 'One piece of mail from one agent to another, held until the recipient claims it.';

COMMENT ON COLUMN public.mail.kind IS 'Who wrote the mail and why. Plain is mail an agent sent. The other values are mail komrad wrote itself, which the panel draws differently. Komrad also counts unread mail by this value. One unread mail of a kind is enough however much is waiting behind it.';

COMMENT ON COLUMN public.mail.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.mail.idempotency_key IS 'The key a retried send is recognised by, null on mail sent once.';

COMMENT ON COLUMN public.mail.subject_id IS 'The thing mail from komrad is about, null on mail an agent sent. Its kind says what sort of thing it is: another agent, a document, a pull request or a worktree. A recipient holds at most one unread mail of a kind about one subject, and later mail rewrites it. There is no foreign key here on purpose. The mail has to keep naming its subject after the subject is gone.';

COMMENT ON COLUMN public.mail.events IS 'The events mail from komrad named, whose workflow prompts were appended to its body. A rewrite keeps those that still hold.';

CREATE TABLE public.pull_request_syncs (
    repository_url text NOT NULL,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.pull_request_syncs IS 'A repository the pull request sync has read at least once, which is what turns its pull request state from unread to none when it has no pull requests.';

COMMENT ON COLUMN public.pull_request_syncs.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.pull_requests (
    id uuid DEFAULT uuidv7() NOT NULL,
    host text NOT NULL,
    forge text NOT NULL,
    provider_id text NOT NULL,
    url text NOT NULL,
    repository_url text NOT NULL,
    head_repository_url text NOT NULL,
    number integer NOT NULL,
    title text NOT NULL,
    head_branch text NOT NULL,
    head_sha text NOT NULL,
    base_branch text NOT NULL,
    base_sha text NOT NULL,
    draft boolean NOT NULL,
    state text NOT NULL,
    opened_at timestamptz NOT NULL,
    changed_at timestamptz NOT NULL,
    merged_at timestamptz,
    closed_at timestamptz,
    first_seen_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    mergeability_blank_since timestamptz,
    auto_merge boolean,
    auto_mergeable boolean DEFAULT false NOT NULL,
    latest_decision text,
    latest_checks text,
    latest_head_sha text,
    latest_mergeability text,
    known_checks text,
    known_checks_sha text,
    known_mergeability text,
    known_mergeability_sha text,
    known_decision text,
    author text DEFAULT ''::text NOT NULL,
    body text DEFAULT ''::text NOT NULL
);

COMMENT ON TABLE public.pull_requests IS 'A pull request komrad has read from a forge, one row per forge identity and none owned by an agent. Finished is derived: merged when merged_at is set, closed when closed_at is set and merged_at is not, open otherwise.';

COMMENT ON COLUMN public.pull_requests.host IS 'The forge host the repository lives on, such as github.com, taken from the repository URL. Two installations of one forge hand out overlapping identifiers, so the host is part of the identity.';

COMMENT ON COLUMN public.pull_requests.forge IS 'Which kind of forge the host runs: github or gitlab.';

COMMENT ON COLUMN public.pull_requests.provider_id IS 'The forge''s own stable identifier: GitHub''s node_id, GitLab''s global id. A rename on the forge changes the URL and not this.';

COMMENT ON COLUMN public.pull_requests.url IS 'The web address a person opens. Unique, and a locator rather than the identity.';

COMMENT ON COLUMN public.pull_requests.repository_url IS 'The canonical URL of the repository the pull request targets.';

COMMENT ON COLUMN public.pull_requests.head_repository_url IS 'The canonical URL of the repository the branch lives in. Empty when the forge no longer has it, which GitHub answers after a fork is deleted.';

COMMENT ON COLUMN public.pull_requests.number IS 'The per-repository number a person sees: GitHub''s number, GitLab''s iid.';

COMMENT ON COLUMN public.pull_requests.state IS 'Where the forge says it stands, as OPEN, CLOSED or MERGED, a cross-check on the timestamps.';

COMMENT ON COLUMN public.pull_requests.opened_at IS 'When the forge says it was opened, which is what orders the pull requests on one branch.';

COMMENT ON COLUMN public.pull_requests.changed_at IS 'The forge''s own last-change stamp, which is what the sync pages by. It is the forge''s clock and not komrad''s, so it is not named updated_at and carries no trigger.';

COMMENT ON COLUMN public.pull_requests.first_seen_at IS 'When komrad first inserted the row.';

COMMENT ON COLUMN public.pull_requests.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.pull_requests.mergeability_blank_since IS 'When the forge first answered a blank mergeability for latest_head_sha, which bounds how long a blank is read again quickly.';

COMMENT ON COLUMN public.pull_requests.auto_merge IS 'Whether the forge says the pull request will merge on its own once its checks and reviews pass, null until it has been read. Read with the checks.';

COMMENT ON COLUMN public.pull_requests.auto_mergeable IS 'Whether the forge would let komrad''s credential turn auto-merge on now, which it will not for a draft, a pull request with nothing left to wait for or a repository that does not allow it.';

COMMENT ON COLUMN public.pull_requests.latest_decision IS 'Where the latest read said the reviews stand, in komrad''s own words: pending, approved, changes_requested or rereview_requested, null when it said nothing.';

COMMENT ON COLUMN public.pull_requests.latest_checks IS 'Where the latest read said the checks on latest_head_sha stand, in komrad''s own words: pending, passing or failing, null when it reported none.';

COMMENT ON COLUMN public.pull_requests.latest_head_sha IS 'The head commit the latest standing read was for, null before the first read.';

COMMENT ON COLUMN public.pull_requests.latest_mergeability IS 'Where the latest read said the pull request stands against its base and its branch rules, in komrad''s own words: clean, unstable, blocked, behind or conflicting, null when the forge had not computed it.';

COMMENT ON COLUMN public.pull_requests.known_checks IS 'The last checks a read reported, which a blank read leaves alone.';

COMMENT ON COLUMN public.pull_requests.known_checks_sha IS 'The head commit known_checks was read for.';

COMMENT ON COLUMN public.pull_requests.known_mergeability IS 'The last mergeability a read reported, which a blank read leaves alone.';

COMMENT ON COLUMN public.pull_requests.known_mergeability_sha IS 'The head commit known_mergeability was read for.';

COMMENT ON COLUMN public.pull_requests.known_decision IS 'The last review decision a read reported, which a blank read leaves alone.';

COMMENT ON COLUMN public.pull_requests.author IS 'The login of whoever opened it, empty when the forge no longer has them.';

COMMENT ON COLUMN public.pull_requests.body IS 'The description as the forge has it, in markdown.';

CREATE TABLE public.review_messages (
    id uuid DEFAULT uuidv7() NOT NULL,
    thread_id uuid NOT NULL,
    author_kind public.review_author_kind NOT NULL,
    author_agent_id uuid,
    body text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    review_id uuid,
    suggestion text[] DEFAULT '{}'::text[] NOT NULL,
    engine_id uuid NOT NULL,
    round_id uuid,
    external_login text,
    forge_comment_id uuid,
    forge_edited_at timestamptz,
    held boolean DEFAULT false NOT NULL,
    accepted_from uuid
);

COMMENT ON TABLE public.review_messages IS 'One message in a review thread, written either by the reviewer or by the agent answering them.';

COMMENT ON COLUMN public.review_messages.author_kind IS 'Whether a person or an agent wrote the message.';

COMMENT ON COLUMN public.review_messages.author_agent_id IS 'The agent that wrote this message, null when a person wrote it or when that agent has since been deleted.';

COMMENT ON COLUMN public.review_messages.body IS 'What was written, as markdown.';

COMMENT ON COLUMN public.review_messages.review_id IS 'The review this message was released with, null for an answer from the agent. An answer belongs to no batch and reaches the reviewer as soon as it is written.';

COMMENT ON COLUMN public.review_messages.suggestion IS 'Replacement lines the reviewer is proposing for the range the thread is pinned to, empty on an ordinary comment. The agent applies them as written rather than treating them as a description of what to do.';

COMMENT ON COLUMN public.review_messages.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.review_messages.round_id IS 'The round an agent''s reply said answers the thread, taken of its worktree as it stood when it replied. Set only on a reply that resolves the thread; null on everything else.';

COMMENT ON COLUMN public.review_messages.external_login IS 'The forge login of whoever wrote this on the forge, for a comment that came from there, empty when the forge no longer knew them. Null for anything written in komrad.';

COMMENT ON COLUMN public.review_messages.forge_comment_id IS 'The forge comment this mirrors, null for anything written in komrad. It names no row once the forge deletes the comment, which is how the copy is known to be gone there.';

COMMENT ON COLUMN public.review_messages.forge_edited_at IS 'When the forge comment had last been edited as of the last sync, null when it never had been.';

COMMENT ON COLUMN public.review_messages.held IS 'Whether the forge author could not write to the repository when the text was written or last edited, which keeps it from the agent. A held first message holds its whole thread until the person accepts it.';

COMMENT ON COLUMN public.review_messages.accepted_from IS 'The held forge message the person accepted this copy of, which releases that message''s thread to the agent once the copy''s review is sent. Null on everything else.';

CREATE TABLE public.review_rounds (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    ordinal integer NOT NULL,
    base_sha text NOT NULL,
    snapshot_sha text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    landed_at timestamptz
);

COMMENT ON TABLE public.review_rounds IS 'One frozen snapshot of what an agent has written. A review is always given against a round rather than against the working files.';

COMMENT ON COLUMN public.review_rounds.ordinal IS 'Which round this is for its agent, counting from one. The panel names the round by this number.';

COMMENT ON COLUMN public.review_rounds.base_sha IS 'The commit the agent''s branch started from. It is resolved once, when the round is taken, so later movement on the base branch cannot change what the round says.';

COMMENT ON COLUMN public.review_rounds.snapshot_sha IS 'A commit covering the agent''s whole checkout, tracked and untracked files alike. A ref under refs/komrad/rounds keeps it alive so git will not collect it. Writing it leaves the agent''s own branch, index and files alone.';

COMMENT ON COLUMN public.review_rounds.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.review_rounds.landed_at IS 'When the base branch was first seen holding this round whole, or a later round whole, null until then. A landed round''s settled comments are history rather than work still under review.';

CREATE TABLE public.review_threads (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    file_path text NOT NULL,
    old_start integer DEFAULT 0 NOT NULL,
    old_end integer DEFAULT 0 NOT NULL,
    new_start integer DEFAULT 0 NOT NULL,
    new_end integer DEFAULT 0 NOT NULL,
    quoted_lines text[] DEFAULT '{}'::text[] NOT NULL,
    human_read_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    last_agent_activity_at timestamptz,
    resolved_at timestamptz,
    review_id uuid NOT NULL,
    round_id uuid NOT NULL,
    base_sha text DEFAULT ''::text NOT NULL,
    resolved_by_kind public.review_author_kind,
    resolved_by_agent_id uuid,
    engine_id uuid NOT NULL,
    forge_thread_id uuid,
    forge_resolved boolean DEFAULT false NOT NULL
);

COMMENT ON TABLE public.review_threads IS 'One conversation pinned to a file and a line range in the round its review was given against.';

COMMENT ON COLUMN public.review_threads.agent_id IS 'The agent being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by agent.';

COMMENT ON COLUMN public.review_threads.file_path IS 'The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it. It is empty for a comment on the change as a whole, which has no file and no lines.';

COMMENT ON COLUMN public.review_threads.old_start IS 'First line of the range on the old side of the diff. Zero means the thread is pinned to lines that exist only on the new side.';

COMMENT ON COLUMN public.review_threads.old_end IS 'Last line of the range on the old side. It equals old_start for a thread on a single line.';

COMMENT ON COLUMN public.review_threads.new_start IS 'First line of the range on the new side of the diff. Zero means the thread is pinned to lines the change deleted.';

COMMENT ON COLUMN public.review_threads.new_end IS 'Last line of the range on the new side. It equals new_start for a thread on a single line.';

COMMENT ON COLUMN public.review_threads.quoted_lines IS 'The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later round is searched for these lines to decide whether the thread has gone outdated.';

COMMENT ON COLUMN public.review_threads.human_read_at IS 'When the person reviewing last read this thread. It is copied from last_agent_activity_at rather than taken from the clock, so a reply written while they were reading is not marked read without having been shown.';

COMMENT ON COLUMN public.review_threads.last_agent_activity_at IS 'When the agent last wrote or rewrote a message here, null until it has written one. It is a column rather than something read off the messages. An agent that rewrites its own reply is activity the person reviewing has not seen, and comparing against the message''s created_at would miss that.';

COMMENT ON COLUMN public.review_threads.resolved_at IS 'When this thread was settled, null while it is still owed an answer.';

COMMENT ON COLUMN public.review_threads.round_id IS 'The round this thread''s line numbers were measured against. It is not read through the review, because a review stays open across several rounds and would name the wrong one.';

COMMENT ON COLUMN public.review_threads.base_sha IS 'The commit the old side of this thread''s line numbers was read against. For a thread written on the whole change that is the round''s own base. For a thread written while the diff was narrowed to one round it is the earlier round''s snapshot.';

COMMENT ON COLUMN public.review_threads.resolved_by_kind IS 'Whether a person or an agent settled this thread, null while nobody has. Two parties can settle one: the agent that wrote the comment, saying the answer was good enough, and the person, who may settle anything including a thread whose author is gone.';

COMMENT ON COLUMN public.review_threads.resolved_by_agent_id IS 'The agent that settled this thread, null when a person did or when nobody has. Do not read null as meaning a person, because resolved_by_kind is what says that. Deleting that agent clears this column rather than the settling.';

COMMENT ON COLUMN public.review_threads.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.review_threads.forge_thread_id IS 'The forge thread this thread mirrors, null for a thread written in komrad or once the forge has deleted it.';

COMMENT ON COLUMN public.review_threads.forge_resolved IS 'Whether the forge said the thread was resolved as of the last sync, so only a change there resolves or reopens it here.';

CREATE TABLE public.review_viewed_files (
    file_path text NOT NULL,
    viewed_at timestamptz DEFAULT now() NOT NULL,
    agent_id uuid NOT NULL,
    fingerprint text NOT NULL,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.review_viewed_files IS 'One file of an agent''s diff the person reviewing has read, and the shape that file had when they read it.';

COMMENT ON COLUMN public.review_viewed_files.file_path IS 'The file that was read, named the way the diff names it: relative to the repository root, where the change left it if it moved it.';

COMMENT ON COLUMN public.review_viewed_files.fingerprint IS 'A hash of the file''s own slice of the patch as it stood when it was read. The panel hashes the file again on every load and shows it as unread once the two disagree, which is what makes an agent writing to the file undo the mark. It is the diff that is hashed rather than the file, since the hunks change when the base moves under a file nothing has touched.';

COMMENT ON COLUMN public.review_viewed_files.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.reviews (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    round_id uuid NOT NULL,
    author_kind public.review_author_kind NOT NULL,
    author_agent_id uuid,
    verdict public.review_verdict,
    released_at timestamptz,
    path text DEFAULT ''::text NOT NULL,
    doorbell_mail_id uuid,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    body text DEFAULT ''::text NOT NULL,
    engine_id uuid NOT NULL,
    external_login text
);

COMMENT ON TABLE public.reviews IS 'One reviewer''s batch of comments on one round. Nobody else sees it while it is being written, and it is released all at once.';

COMMENT ON COLUMN public.reviews.agent_id IS 'The agent being reviewed. The round already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join.';

COMMENT ON COLUMN public.reviews.author_kind IS 'Whether a person or an agent is giving the review.';

COMMENT ON COLUMN public.reviews.author_agent_id IS 'The agent giving the review, null when a person is giving it. Do not read null as meaning a person, because author_kind is what says that. Deleting the reviewing agent clears this column rather than deleting the review. Reviewer agents are deleted as a matter of course, and the agent they reviewed is still working from what they said.';

COMMENT ON COLUMN public.reviews.verdict IS 'What the reviewer concluded, null while the review is still open. It is how the agent learns whether the work is done, and only an approval lets it open a pull request.';

COMMENT ON COLUMN public.reviews.released_at IS 'When the review was released, null while it is still being written. None of it reaches the agent until this is set.';

COMMENT ON COLUMN public.reviews.path IS 'The absolute path of the file the released review was written to, empty while the review is still open. The file lives in komrad''s own state directory rather than in the agent''s checkout.';

COMMENT ON COLUMN public.reviews.doorbell_mail_id IS 'The mail that told the agent a review was waiting. Deleting that mail clears this column rather than deleting the review, so a review whose mail was thrown away reads as undelivered.';

COMMENT ON COLUMN public.reviews.body IS 'What the reviewer said about the change as a whole rather than about any one line, empty when they said nothing. It goes at the top of the released review file, above the line comments, because it is what the agent should read first.';

COMMENT ON COLUMN public.reviews.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.reviews.external_login IS 'The forge login of whoever wrote a review on the forge, empty when the forge no longer knew them. Null for a review written in komrad.';

CREATE TABLE public.schedule_runs (
    id bigint NOT NULL,
    schedule_id uuid NOT NULL,
    scheduled_for timestamptz NOT NULL,
    state text NOT NULL,
    attempt integer NOT NULL,
    max_attempts integer NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    started_at timestamptz DEFAULT now() NOT NULL,
    finalized_at timestamptz,
    engine_id uuid NOT NULL,
    CONSTRAINT schedule_runs_state_known CHECK ((state = ANY (ARRAY['running'::text, 'retryable'::text, 'completed'::text, 'cancelled'::text, 'discarded'::text])))
);

ALTER TABLE public.schedule_runs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.schedule_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.schedules (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    cron text NOT NULL,
    timezone text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    hide_agents boolean DEFAULT false NOT NULL,
    prompt text DEFAULT ''::text NOT NULL,
    workflow_id uuid NOT NULL,
    archived_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    kind public.schedule_kind DEFAULT 'implementer'::public.schedule_kind NOT NULL,
    orchestrator_agent_id uuid,
    CONSTRAINT schedules_orchestrator_matches_kind CHECK (((kind = 'orchestrator'::public.schedule_kind) = (orchestrator_agent_id IS NOT NULL)))
);

COMMENT ON TABLE public.schedules IS 'A recurring firing, holding when to fire and the prompt it launches an implementer with or mails an orchestrator.';

COMMENT ON COLUMN public.schedules.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.schedules.kind IS 'Whether a firing launches an implementer into the workflow or mails the prompt to an orchestrator.';

COMMENT ON COLUMN public.schedules.orchestrator_agent_id IS 'The orchestrator an orchestrator schedule mails.';

CREATE TABLE public.workflow_configs (
    id uuid DEFAULT uuidv7() NOT NULL,
    harness text DEFAULT ''::text NOT NULL,
    cwd text DEFAULT ''::text NOT NULL,
    flags text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL,
    reviewer_harness text DEFAULT ''::text NOT NULL,
    reviewer_flags text DEFAULT ''::text NOT NULL
);

COMMENT ON TABLE public.workflow_configs IS 'The harness, working directory and flags a workflow gives each new agent it creates.';

COMMENT ON COLUMN public.workflow_configs.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.workflow_configs.reviewer_harness IS 'The harness the workflow''s reviewers run, blank meaning the one its other agents run.';

COMMENT ON COLUMN public.workflow_configs.reviewer_flags IS 'The flags the workflow''s reviewers are launched with, blank meaning the other agents'' flags when the harness is the same.';

CREATE TABLE public.workflow_event_prompts (
    workflow_id uuid NOT NULL,
    event text NOT NULL,
    prompt text NOT NULL,
    "position" integer NOT NULL,
    engine_id uuid NOT NULL,
    muted boolean DEFAULT false NOT NULL
);

COMMENT ON TABLE public.workflow_event_prompts IS 'What a workflow asks of its agents when komrad sends them an event, appended to that mail.';

COMMENT ON COLUMN public.workflow_event_prompts.event IS 'The event the prompt rides on, such as pull_request.checks_failing.';

COMMENT ON COLUMN public.workflow_event_prompts.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.workflow_event_prompts.muted IS 'Whether the event is kept from opening mail of its own.';

CREATE TABLE public.workflow_hooks (
    id uuid DEFAULT uuidv7() NOT NULL,
    workflow_id uuid NOT NULL,
    event text NOT NULL,
    script text NOT NULL,
    "position" integer NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.workflow_hooks IS 'Scripts a workflow runs at one point in an agent''s life, in position order.';

COMMENT ON COLUMN public.workflow_hooks.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.workflow_provisioner_configs (
    id uuid DEFAULT uuidv7() NOT NULL,
    worktree_enabled boolean DEFAULT false NOT NULL,
    worktree_base_branch text NOT NULL,
    worktree_copy_paths text[] DEFAULT '{}'::text[] NOT NULL,
    worktree_symlink_paths text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    worktree_diff_exclude text[] DEFAULT '{}'::text[] NOT NULL,
    worktree_dir text DEFAULT ''::text NOT NULL,
    engine_id uuid NOT NULL,
    CONSTRAINT worktree_base_branch_named CHECK ((worktree_base_branch <> ''::text))
);

COMMENT ON TABLE public.workflow_provisioner_configs IS 'Whether a workflow gives each of its agents a git worktree of its own, and how that worktree is set up.';

COMMENT ON COLUMN public.workflow_provisioner_configs.worktree_base_branch IS 'The branch worktrees are cut from and every agent''s work is measured against, never blank.';

COMMENT ON COLUMN public.workflow_provisioner_configs.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.workflows (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    context text DEFAULT ''::text NOT NULL,
    workflow_config_id uuid NOT NULL,
    provisioner_config_id uuid NOT NULL,
    archived_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    deleting_at timestamptz,
    engine_id uuid NOT NULL
);

COMMENT ON TABLE public.workflows IS 'A named group of related agents. It is grouping only. Nothing here runs.';

COMMENT ON COLUMN public.workflows.deleting_at IS 'When a delete claimed the workflow, which happens when a delete claims the agents inside it. A claimed workflow is left out of the archive until the last of those agents goes and takes the row with it, and an agent whose delete fails gives the workflow back as it comes back itself.';

COMMENT ON COLUMN public.workflows.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TABLE public.worktrees (
    dir text CONSTRAINT worktree_sizes_dir_not_null NOT NULL,
    size_bytes bigint DEFAULT 0 CONSTRAINT worktree_sizes_size_bytes_not_null NOT NULL,
    measured_at timestamptz,
    size_error text,
    engine_id uuid CONSTRAINT worktree_sizes_engine_id_not_null NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    repo_dir text DEFAULT ''::text NOT NULL,
    branch text DEFAULT ''::text NOT NULL,
    readable boolean DEFAULT false NOT NULL,
    mtime timestamptz,
    share_bytes bigint DEFAULT 0 NOT NULL,
    standing_read_at timestamptz,
    dirty boolean DEFAULT false NOT NULL,
    sequencing boolean DEFAULT false NOT NULL,
    unlanded integer DEFAULT 0 NOT NULL,
    standing_error text
);

COMMENT ON TABLE public.worktrees IS 'One directory under the worktree directories as the filesystem holds it, written by the size loop. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for. An unowned directory an unfinished delete names has no row, and the watch leaves out one whose agent is being deleted.';

COMMENT ON COLUMN public.worktrees.size_bytes IS 'Blocks taken rather than length, a hard linked file counted once. Zero until measured and while size_error is set.';

COMMENT ON COLUMN public.worktrees.measured_at IS 'When the walk last finished, whether or not it succeeded, null until the first walk.';

COMMENT ON COLUMN public.worktrees.size_error IS 'Why the last walk failed, null when it did not.';

COMMENT ON COLUMN public.worktrees.engine_id IS 'The engine that owns this row, stamped from the login that wrote it.';

COMMENT ON COLUMN public.worktrees.name IS 'The directory''s basename, which is the id of the agent it was cut for.';

COMMENT ON COLUMN public.worktrees.repo_dir IS 'The repository the directory was cut from, read off its .git file, empty when there is no such file.';

COMMENT ON COLUMN public.worktrees.branch IS 'What the gitdir''s HEAD names, empty when it is detached or cannot be read.';

COMMENT ON COLUMN public.worktrees.readable IS 'Whether the gitdir the .git file names is still there.';

COMMENT ON COLUMN public.worktrees.mtime IS 'When git last wrote the directory''s .git file, else the directory''s own modification time.';

COMMENT ON COLUMN public.worktrees.share_bytes IS 'The directory''s share of size_bytes, each hard linked file''s blocks split evenly across its links. Zero until measured and while size_error is set.';

COMMENT ON COLUMN public.worktrees.standing_read_at IS 'When git was last asked where the directory stands against its base, whether or not it answered, null until the first reading.';

COMMENT ON COLUMN public.worktrees.dirty IS 'Whether the directory held modified or untracked files at the last reading.';

COMMENT ON COLUMN public.worktrees.sequencing IS 'Whether a merge, a rebase or a cherry-pick was part-way through at the last reading.';

COMMENT ON COLUMN public.worktrees.unlanded IS 'How many of the branch''s own commits the base did not yet hold at the last reading.';

COMMENT ON COLUMN public.worktrees.standing_error IS 'Why the last reading failed, null when it did not.';

ALTER TABLE ONLY public.agent_pull_requests
    ADD CONSTRAINT agent_pull_requests_pkey PRIMARY KEY (agent_id, pull_request_id);

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.agent_screens
    ADD CONSTRAINT agent_screens_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.agent_transcript_chunks
    ADD CONSTRAINT agent_transcript_chunks_pkey PRIMARY KEY (agent_id, seq);

ALTER TABLE ONLY public.agent_transcript_sessions
    ADD CONSTRAINT agent_transcript_sessions_agent_id_session_id_key UNIQUE (agent_id, session_id);

ALTER TABLE ONLY public.agent_transcript_sessions
    ADD CONSTRAINT agent_transcript_sessions_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.agent_transcript_sessions
    ADD CONSTRAINT agent_transcript_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.document_type_versions
    ADD CONSTRAINT document_type_versions_pkey PRIMARY KEY (document_type_id, version);

ALTER TABLE ONLY public.document_types
    ADD CONSTRAINT document_types_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.document_types
    ADD CONSTRAINT document_types_engine_id_name_key UNIQUE (engine_id, name);

ALTER TABLE ONLY public.document_types
    ADD CONSTRAINT document_types_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.document_versions
    ADD CONSTRAINT document_versions_pkey PRIMARY KEY (document_id, version);

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_agent_id_name_key UNIQUE (engine_id, agent_id, name);

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.engine_logins
    ADD CONSTRAINT engine_logins_pkey PRIMARY KEY (role);

ALTER TABLE ONLY public.engines
    ADD CONSTRAINT engines_name_key UNIQUE (name);

ALTER TABLE ONLY public.engines
    ADD CONSTRAINT engines_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.forge_connections
    ADD CONSTRAINT forge_connections_host_key UNIQUE (engine_id, host);

ALTER TABLE ONLY public.forge_connections
    ADD CONSTRAINT forge_connections_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.forge_review_comments
    ADD CONSTRAINT forge_review_comments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.forge_review_comments
    ADD CONSTRAINT forge_review_comments_provider_id_key UNIQUE (engine_id, thread_id, provider_id);

ALTER TABLE ONLY public.forge_review_threads
    ADD CONSTRAINT forge_review_threads_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.forge_review_threads
    ADD CONSTRAINT forge_review_threads_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.forge_review_threads
    ADD CONSTRAINT forge_review_threads_provider_id_key UNIQUE (engine_id, pull_request_id, provider_id);

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.pull_request_syncs
    ADD CONSTRAINT pull_request_syncs_pkey PRIMARY KEY (engine_id, repository_url);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_host_provider_id_key UNIQUE (engine_id, host, provider_id);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_url_key UNIQUE (engine_id, url);

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.review_rounds
    ADD CONSTRAINT review_rounds_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.review_rounds
    ADD CONSTRAINT review_rounds_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.review_viewed_files
    ADD CONSTRAINT review_viewed_files_pkey PRIMARY KEY (agent_id, file_path);

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.schedule_runs
    ADD CONSTRAINT schedule_runs_occurrence_key UNIQUE (engine_id, schedule_id, scheduled_for);

ALTER TABLE ONLY public.schedule_runs
    ADD CONSTRAINT schedule_runs_pkey PRIMARY KEY (engine_id, id);

ALTER TABLE ONLY public.schedules
    ADD CONSTRAINT schedules_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.schedules
    ADD CONSTRAINT schedules_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workflow_configs
    ADD CONSTRAINT workflow_configs_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.workflow_configs
    ADD CONSTRAINT workflow_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workflow_event_prompts
    ADD CONSTRAINT workflow_event_prompts_pkey PRIMARY KEY (workflow_id, event);

ALTER TABLE ONLY public.workflow_hooks
    ADD CONSTRAINT workflow_hooks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workflow_provisioner_configs
    ADD CONSTRAINT workflow_provisioner_configs_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.workflow_provisioner_configs
    ADD CONSTRAINT workflow_provisioner_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_engine_id_id_key UNIQUE (engine_id, id);

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.worktrees
    ADD CONSTRAINT worktrees_pkey PRIMARY KEY (engine_id, dir);

CREATE INDEX agent_pull_requests_pull_request_id ON public.agent_pull_requests USING btree (pull_request_id);

CREATE INDEX agent_runs_agent_id_started_at ON public.agent_runs USING btree (agent_id, started_at DESC);

CREATE INDEX agent_runs_unread_failures ON public.agent_runs USING btree (agent_id) WHERE ((failure <> ''::text) AND (viewed_at IS NULL));

CREATE INDEX agent_transcript_chunks_content_trgm ON public.agent_transcript_chunks USING gin (content public.gin_trgm_ops);

CREATE INDEX agent_transcript_chunks_session_id ON public.agent_transcript_chunks USING btree (session_id);

CREATE INDEX agent_transcript_chunks_spoken_at ON public.agent_transcript_chunks USING btree (spoken_at);

CREATE INDEX agent_transcript_chunks_tsv ON public.agent_transcript_chunks USING gin (tsv);

CREATE UNIQUE INDEX agents_idempotency_key ON public.agents USING btree (engine_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

CREATE UNIQUE INDEX agents_name_active ON public.agents USING btree (workflow_id, name) WHERE (archived_at IS NULL);

CREATE INDEX agents_parent_agent_id ON public.agents USING btree (parent_agent_id);

CREATE INDEX agents_schedule_id ON public.agents USING btree (schedule_id);

CREATE INDEX agents_workflow_id ON public.agents USING btree (workflow_id);

CREATE UNIQUE INDEX agents_workflow_orchestrator ON public.agents USING btree (workflow_id) WHERE (role = 'orchestrator'::public.agent_role);

CREATE INDEX engine_logins_engine_id ON public.engine_logins USING btree (engine_id);

CREATE INDEX forge_review_comments_thread_id ON public.forge_review_comments USING btree (thread_id);

CREATE INDEX forge_review_threads_pull_request_id ON public.forge_review_threads USING btree (pull_request_id);

CREATE INDEX mail_from_agent_id ON public.mail USING btree (from_agent_id, to_agent_id);

COMMENT ON INDEX public.mail_from_agent_id IS 'Counts the mail two agents have exchanged. A send is refused once that count reaches the cap.';

CREATE UNIQUE INDEX mail_idempotency_key ON public.mail USING btree (engine_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

CREATE INDEX mail_pending ON public.mail USING btree (to_agent_id, id) WHERE (claimed_at IS NULL);

COMMENT ON INDEX public.mail_pending IS 'Reads the mail an agent has not claimed yet. A send is refused when that is already full, and further review mail joins the unread one found here.';

CREATE UNIQUE INDEX mail_pending_subject ON public.mail USING btree (to_agent_id, kind, subject_id) WHERE ((claimed_at IS NULL) AND (subject_id IS NOT NULL));

COMMENT ON INDEX public.mail_pending_subject IS 'Holds a recipient to one unread mail from komrad of a kind about one subject, which later mail rewrites in place.';

CREATE INDEX mail_reply_to ON public.mail USING btree (reply_to);

CREATE INDEX pull_requests_attribution ON public.pull_requests USING btree (repository_url, head_repository_url, head_branch);

CREATE INDEX pull_requests_unfinished_head ON public.pull_requests USING btree (repository_url, head_branch) WHERE ((merged_at IS NULL) AND (closed_at IS NULL));

CREATE INDEX review_messages_accepted_from ON public.review_messages USING btree (accepted_from);

CREATE INDEX review_messages_review_id ON public.review_messages USING btree (review_id);

CREATE UNIQUE INDEX review_messages_thread_forge_comment ON public.review_messages USING btree (thread_id, forge_comment_id) WHERE (forge_comment_id IS NOT NULL);

COMMENT ON INDEX public.review_messages_thread_forge_comment IS 'Holds each mirrored thread to one copy of a forge comment.';

CREATE INDEX review_messages_thread_id ON public.review_messages USING btree (thread_id, id);

CREATE UNIQUE INDEX review_rounds_agent_ordinal ON public.review_rounds USING btree (agent_id, ordinal);

CREATE UNIQUE INDEX review_threads_agent_forge_thread ON public.review_threads USING btree (agent_id, forge_thread_id) WHERE (forge_thread_id IS NOT NULL);

COMMENT ON INDEX public.review_threads_agent_forge_thread IS 'Holds each agent to one mirror of a forge thread.';

CREATE INDEX review_threads_agent_id ON public.review_threads USING btree (agent_id, file_path, new_start, old_start);

CREATE INDEX review_threads_forge_thread_id ON public.review_threads USING btree (forge_thread_id);

CREATE INDEX review_threads_review_id ON public.review_threads USING btree (review_id, id);

CREATE INDEX reviews_doorbell_mail_id ON public.reviews USING btree (doorbell_mail_id);

COMMENT ON INDEX public.reviews_doorbell_mail_id IS 'Finds the review a piece of mail told the agent about. Deleting that mail walks this index.';

CREATE UNIQUE INDEX reviews_open_agent ON public.reviews USING btree (agent_id, author_agent_id) WHERE ((released_at IS NULL) AND (author_kind = 'agent'::public.review_author_kind));

COMMENT ON INDEX public.reviews_open_agent IS 'Holds each reviewing agent to one unreleased review at a time. Postgres counts two nulls in this column as different values, and the index relies on that. If nulls collided, deleting a second reviewer agent would fail, because the foreign key clears this column and would land on the null the first delete left behind.';

CREATE UNIQUE INDEX reviews_open_human ON public.reviews USING btree (agent_id) WHERE ((released_at IS NULL) AND (author_kind = 'human'::public.review_author_kind));

COMMENT ON INDEX public.reviews_open_human IS 'Holds the person to one unreleased review at a time, the way GitHub holds one pending review per reviewer.';

CREATE INDEX reviews_round_id ON public.reviews USING btree (round_id, id);

CREATE UNIQUE INDEX schedules_name_active ON public.schedules USING btree (engine_id, name) WHERE (archived_at IS NULL);

CREATE INDEX schedules_workflow_id ON public.schedules USING btree (workflow_id);

CREATE INDEX workflow_hooks_workflow_id_position_idx ON public.workflow_hooks USING btree (workflow_id, "position");

CREATE INDEX workflows_provisioner_config_id ON public.workflows USING btree (provisioner_config_id);

CREATE INDEX workflows_workflow_config_id ON public.workflows USING btree (workflow_config_id);

CREATE INDEX worktrees_name ON public.worktrees USING btree (engine_id, name);

CREATE FUNCTION public.attach_updated_at_triggers() RETURNS void
    LANGUAGE plpgsql
    AS $$
DECLARE
    target text;
BEGIN
    FOR target IN
    SELECT
        c.relname
    FROM
        pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE
        n.nspname = 'public'
        AND c.relkind = 'r'
        AND a.attname = 'updated_at'
        AND NOT a.attisdropped LOOP
            EXECUTE FORMAT('DROP TRIGGER IF EXISTS set_updated_at ON %I', target);
            EXECUTE FORMAT('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', target);
        END LOOP;
END;
$$;

CREATE FUNCTION public.current_engine() RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    SELECT
        engine_id
    FROM
        public.engine_logins
    WHERE
        ROLE = SESSION_USER;
$$;

CREATE FUNCTION public.current_login_is_reader() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    SELECT
        COALESCE((
            SELECT
                reader
            FROM public.engine_logins
            WHERE
                ROLE = SESSION_USER), FALSE);
$$;

CREATE FUNCTION public.listen_to_watch_channel() RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF current_engine() IS NULL THEN
        RAISE EXCEPTION 'this login belongs to no engine';
    END IF;
    EXECUTE FORMAT('LISTEN %I', watch_channel());
END;
$$;

CREATE FUNCTION public.review_thread_root_readable(thread uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'public'
    AS $$
    SELECT
        NOT root.held
        OR EXISTS (
            SELECT
                1
            FROM
                public.review_messages copy
                JOIN public.reviews r ON r.id = copy.review_id
            WHERE
                copy.accepted_from = root.id
                AND r.released_at IS NOT NULL)
    FROM (
        SELECT
            m.id,
            m.held
        FROM
            public.review_messages m
        WHERE
            m.thread_id = thread
        ORDER BY
            m.id
        LIMIT 1) root;
$$;

CREATE FUNCTION public.seed_document_types() RETURNS trigger
    LANGUAGE plpgsql
    AS $_$
BEGIN
    WITH seeded AS (
        INSERT INTO document_types (engine_id, name, context)
            VALUES (NEW.id, 'RFC', $context$A proposal for a change, written before building it.

Number it after the highest-numbered RFC already written, and title it the way the others are, such as "RFC 0012: A short title".

Say why the change is needed and what is wrong today, then how the change works. Include a mockup of any screen it changes.

An RFC lives in the panel as a document. Never write one as a file in the repository.
$context$)
        RETURNING
            *
    )
    INSERT INTO document_type_versions (document_type_id, version, name, context, created_at, engine_id)
    SELECT
        id,
        version,
        name,
        context,
        updated_at,
        engine_id
    FROM
        seeded;
    RETURN NULL;
END;
$_$;

CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.stamp_engine() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.watch_channel_of(engine uuid) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
    SELECT
        'komrad_watch_' || REPLACE(engine::text, '-', '');
$$;

CREATE FUNCTION public.watch_channel() RETURNS text
    LANGUAGE sql STABLE
    AS $$
    SELECT
        watch_channel_of(current_engine());
$$;

CREATE FUNCTION public.watch_notify() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    ignored text[] := TG_ARGV[1:TG_NARGS - 1];
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify(watch_channel_of(OLD.engine_id), TG_TABLE_NAME || ' ' || (to_jsonb(OLD) ->> TG_ARGV[0]));
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND to_jsonb(OLD) - ignored = to_jsonb(NEW) - ignored THEN
        RETURN NULL;
    END IF;
    PERFORM pg_notify(watch_channel_of(NEW.engine_id), TG_TABLE_NAME || ' ' || (to_jsonb(NEW) ->> TG_ARGV[0]));
    RETURN NULL;
END;
$$;

CREATE FUNCTION public.watch_subresource() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(OLD.engine_id), TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(NEW.engine_id), TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION public.watch_subresource_lookup() RETURNS trigger
    LANGUAGE plpgsql
    AS $_$
DECLARE
    resource_key text;
    narrowing text := 'true';
    row_engine uuid := (to_jsonb(COALESCE(NEW, OLD)) ->> 'engine_id')::uuid;
    parent_type regtype;
BEGIN
    IF TG_OP = 'UPDATE' AND TG_ARGV[5] <> '' AND (to_jsonb(OLD) -> TG_ARGV[5]) IS NOT DISTINCT FROM (to_jsonb(NEW) -> TG_ARGV[5]) THEN
        RETURN NULL;
    END IF;
    IF TG_ARGV[4] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[4]);
    END IF;
    SELECT
        a.atttypid::regtype INTO parent_type
    FROM
        pg_attribute a
    WHERE
        a.attrelid = TG_ARGV[0]::regclass
        AND a.attname = TG_ARGV[2];
    FOR resource_key IN EXECUTE FORMAT('SELECT %I::text FROM %I WHERE engine_id = $2 AND %I = CAST($1 AS %s) AND (%s)', TG_ARGV[1], TG_ARGV[0], TG_ARGV[2], parent_type, narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[3]), row_engine LOOP
        PERFORM pg_notify(watch_channel_of(row_engine), TG_ARGV[0] || ' ' || resource_key);
    END LOOP;
    RETURN NULL;
END;
$_$;

INSERT INTO public.engines (id, name) VALUES ('00000000-0000-7000-8000-000000000001', 'default');

CREATE TRIGGER seed_document_types AFTER INSERT ON public.engines FOR EACH ROW EXECUTE FUNCTION public.seed_document_types();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.agents FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.document_types FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.documents FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.forge_review_comments FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.forge_review_threads FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.mail FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.review_messages FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.review_threads FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.reviews FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.schedules FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.workflow_configs FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.workflows FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agent_pull_requests FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agent_runs FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agent_screens FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agent_transcript_chunks FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agent_transcript_sessions FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.agents FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.document_type_versions FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.document_types FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.document_versions FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.documents FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.forge_connections FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.forge_review_comments FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.forge_review_threads FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.mail FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.pull_request_syncs FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.pull_requests FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.review_messages FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.review_rounds FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.review_threads FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.review_viewed_files FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.reviews FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.schedule_runs FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.schedules FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.workflow_configs FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.workflow_event_prompts FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.workflow_hooks FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.workflows FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER stamp_engine BEFORE INSERT ON public.worktrees FOR EACH ROW EXECUTE FUNCTION public.stamp_engine();

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.agents FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id', 'state_seen_at', 'updated_at');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.document_types FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.mail FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.pull_request_syncs FOR EACH ROW EXECUTE FUNCTION public.watch_notify('repository_url');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.pull_requests FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.review_threads FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.reviews FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.schedules FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.workflows FOR EACH ROW EXECUTE FUNCTION public.watch_notify('id');

CREATE TRIGGER watch_notify AFTER INSERT OR DELETE OR UPDATE ON public.worktrees FOR EACH ROW EXECUTE FUNCTION public.watch_notify('dir');

CREATE TRIGGER watch_subresource_agents AFTER INSERT OR DELETE OR UPDATE ON public.agent_runs FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('agents', 'agent_id');

CREATE TRIGGER watch_subresource_agents AFTER INSERT OR DELETE OR UPDATE ON public.documents FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('agents', 'agent_id');

CREATE TRIGGER watch_subresource_pull_requests AFTER INSERT OR DELETE OR UPDATE ON public.agent_pull_requests FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('pull_requests', 'pull_request_id');

CREATE TRIGGER watch_subresource_review_threads AFTER INSERT OR DELETE OR UPDATE ON public.agents FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('review_threads', 'id', 'agent_id', 'id', '', 'archived_at');

CREATE TRIGGER watch_subresource_review_threads AFTER INSERT OR DELETE OR UPDATE ON public.review_messages FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('review_threads', 'thread_id');

CREATE TRIGGER watch_subresource_reviews AFTER INSERT OR DELETE OR UPDATE ON public.mail FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('reviews', 'id', 'doorbell_mail_id', 'id', '', 'claimed_at');

CREATE TRIGGER watch_subresource_reviews AFTER INSERT OR DELETE OR UPDATE ON public.review_rounds FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('reviews', 'id', 'round_id', 'id', '', '');

CREATE TRIGGER watch_subresource_schedules AFTER INSERT OR DELETE OR UPDATE ON public.schedule_runs FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('schedules', 'schedule_id');

CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR DELETE OR UPDATE ON public.workflow_configs FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('workflows', 'id', 'workflow_config_id', 'id', '', '');

CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR DELETE OR UPDATE ON public.workflow_event_prompts FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('workflows', 'workflow_id');

CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR DELETE OR UPDATE ON public.workflow_hooks FOR EACH ROW EXECUTE FUNCTION public.watch_subresource('workflows', 'workflow_id');

CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR DELETE OR UPDATE ON public.workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('workflows', 'id', 'provisioner_config_id', 'id', '', '');

CREATE TRIGGER watch_subresource_worktrees AFTER INSERT OR DELETE OR UPDATE ON public.agents FOR EACH ROW EXECUTE FUNCTION public.watch_subresource_lookup('worktrees', 'dir', 'name', 'id', '', 'deleting_at');

ALTER TABLE ONLY public.agent_pull_requests
    ADD CONSTRAINT agent_pull_requests_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_pull_requests
    ADD CONSTRAINT agent_pull_requests_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agent_pull_requests
    ADD CONSTRAINT agent_pull_requests_pull_request_id_fkey FOREIGN KEY (engine_id, pull_request_id) REFERENCES public.pull_requests(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agent_screens
    ADD CONSTRAINT agent_screens_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_screens
    ADD CONSTRAINT agent_screens_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agent_transcript_chunks
    ADD CONSTRAINT agent_transcript_chunks_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_transcript_chunks
    ADD CONSTRAINT agent_transcript_chunks_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agent_transcript_chunks
    ADD CONSTRAINT agent_transcript_chunks_session_id_fkey FOREIGN KEY (engine_id, session_id) REFERENCES public.agent_transcript_sessions(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_transcript_sessions
    ADD CONSTRAINT agent_transcript_sessions_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_transcript_sessions
    ADD CONSTRAINT agent_transcript_sessions_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_parent_agent_id_fkey FOREIGN KEY (engine_id, parent_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_schedule_id_fkey FOREIGN KEY (engine_id, schedule_id) REFERENCES public.schedules(engine_id, id) ON DELETE SET NULL (schedule_id);

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_spawned_by_agent_id_fkey FOREIGN KEY (engine_id, spawned_by_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE SET NULL (spawned_by_agent_id);

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_workflow_id_fkey FOREIGN KEY (engine_id, workflow_id) REFERENCES public.workflows(engine_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.document_type_versions
    ADD CONSTRAINT document_type_versions_document_type_id_fkey FOREIGN KEY (engine_id, document_type_id) REFERENCES public.document_types(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.document_type_versions
    ADD CONSTRAINT document_type_versions_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.document_types
    ADD CONSTRAINT document_types_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.document_versions
    ADD CONSTRAINT document_versions_document_id_fkey FOREIGN KEY (engine_id, document_id) REFERENCES public.documents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.document_versions
    ADD CONSTRAINT document_versions_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.documents
    ADD CONSTRAINT documents_type_fkey FOREIGN KEY (engine_id, type) REFERENCES public.document_types(engine_id, name) ON UPDATE CASCADE ON DELETE RESTRICT;

ALTER TABLE ONLY public.engine_logins
    ADD CONSTRAINT engine_logins_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.forge_connections
    ADD CONSTRAINT forge_connections_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.forge_review_comments
    ADD CONSTRAINT forge_review_comments_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.forge_review_comments
    ADD CONSTRAINT forge_review_comments_thread_id_fkey FOREIGN KEY (engine_id, thread_id) REFERENCES public.forge_review_threads(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.forge_review_threads
    ADD CONSTRAINT forge_review_threads_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.forge_review_threads
    ADD CONSTRAINT forge_review_threads_pull_request_id_fkey FOREIGN KEY (engine_id, pull_request_id) REFERENCES public.pull_requests(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_from_agent_id_fkey FOREIGN KEY (engine_id, from_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE SET NULL (from_agent_id);

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_reply_to_fkey FOREIGN KEY (engine_id, reply_to) REFERENCES public.mail(engine_id, id) ON DELETE SET NULL (reply_to);

ALTER TABLE ONLY public.mail
    ADD CONSTRAINT mail_to_agent_id_fkey FOREIGN KEY (engine_id, to_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.pull_request_syncs
    ADD CONSTRAINT pull_request_syncs_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_accepted_from_fkey FOREIGN KEY (engine_id, accepted_from) REFERENCES public.review_messages(engine_id, id) ON DELETE SET NULL (accepted_from);

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_author_agent_id_fkey FOREIGN KEY (engine_id, author_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE SET NULL (author_agent_id);

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_review_id_fkey FOREIGN KEY (engine_id, review_id) REFERENCES public.reviews(engine_id, id) ON DELETE SET NULL (review_id);

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_round_id_fkey FOREIGN KEY (engine_id, round_id) REFERENCES public.review_rounds(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_messages
    ADD CONSTRAINT review_messages_thread_id_fkey FOREIGN KEY (engine_id, thread_id) REFERENCES public.review_threads(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_rounds
    ADD CONSTRAINT review_rounds_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_rounds
    ADD CONSTRAINT review_rounds_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_forge_thread_id_fkey FOREIGN KEY (engine_id, forge_thread_id) REFERENCES public.forge_review_threads(engine_id, id) ON DELETE SET NULL (forge_thread_id);

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_resolved_by_agent_id_fkey FOREIGN KEY (engine_id, resolved_by_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE SET NULL (resolved_by_agent_id);

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_review_id_fkey FOREIGN KEY (engine_id, review_id) REFERENCES public.reviews(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_threads
    ADD CONSTRAINT review_threads_round_id_fkey FOREIGN KEY (engine_id, round_id) REFERENCES public.review_rounds(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_viewed_files
    ADD CONSTRAINT review_viewed_files_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.review_viewed_files
    ADD CONSTRAINT review_viewed_files_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_agent_id_fkey FOREIGN KEY (engine_id, agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_author_agent_id_fkey FOREIGN KEY (engine_id, author_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE SET NULL (author_agent_id);

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_doorbell_mail_id_fkey FOREIGN KEY (engine_id, doorbell_mail_id) REFERENCES public.mail(engine_id, id) ON DELETE SET NULL (doorbell_mail_id);

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_round_id_fkey FOREIGN KEY (engine_id, round_id) REFERENCES public.review_rounds(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.schedule_runs
    ADD CONSTRAINT schedule_runs_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedule_runs
    ADD CONSTRAINT schedule_runs_schedule_id_fkey FOREIGN KEY (engine_id, schedule_id) REFERENCES public.schedules(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.schedules
    ADD CONSTRAINT schedules_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedules
    ADD CONSTRAINT schedules_orchestrator_agent_id_fkey FOREIGN KEY (engine_id, orchestrator_agent_id) REFERENCES public.agents(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.schedules
    ADD CONSTRAINT schedules_workflow_id_fkey FOREIGN KEY (engine_id, workflow_id) REFERENCES public.workflows(engine_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflow_configs
    ADD CONSTRAINT workflow_configs_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflow_event_prompts
    ADD CONSTRAINT workflow_event_prompts_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflow_event_prompts
    ADD CONSTRAINT workflow_event_prompts_workflow_id_fkey FOREIGN KEY (engine_id, workflow_id) REFERENCES public.workflows(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workflow_hooks
    ADD CONSTRAINT workflow_hooks_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflow_hooks
    ADD CONSTRAINT workflow_hooks_workflow_id_fkey FOREIGN KEY (engine_id, workflow_id) REFERENCES public.workflows(engine_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workflow_provisioner_configs
    ADD CONSTRAINT workflow_provisioner_configs_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_provisioner_config_id_fkey FOREIGN KEY (engine_id, provisioner_config_id) REFERENCES public.workflow_provisioner_configs(engine_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_workflow_config_id_fkey FOREIGN KEY (engine_id, workflow_config_id) REFERENCES public.workflow_configs(engine_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.worktrees
    ADD CONSTRAINT worktrees_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES public.engines(id) ON DELETE RESTRICT;

ALTER TABLE public.agent_pull_requests ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.agent_runs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.agent_screens ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.agent_transcript_chunks ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.agent_transcript_sessions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.agents ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.document_type_versions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.document_types ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.document_versions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON public.agent_pull_requests USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.agent_runs USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.agent_screens USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.agent_transcript_chunks USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.agent_transcript_sessions USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.agents USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.document_type_versions USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.document_types USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.document_versions USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.documents USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.engines USING ((id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.forge_connections USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.forge_review_comments USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.forge_review_threads USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.mail USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.pull_request_syncs USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.pull_requests USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.review_messages USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.review_rounds USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.review_threads USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.review_viewed_files USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.reviews USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.schedule_runs USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.schedules USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.workflow_configs USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.workflow_event_prompts USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.workflow_hooks USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.workflow_provisioner_configs USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.workflows USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

CREATE POLICY engine_isolation ON public.worktrees USING ((engine_id = ( SELECT public.current_engine() AS current_engine))) WITH CHECK ((engine_id = ( SELECT public.current_engine() AS current_engine)));

ALTER TABLE public.engines ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.forge_connections ENABLE ROW LEVEL SECURITY;

CREATE POLICY forge_held ON public.review_messages AS RESTRICTIVE FOR SELECT TO komrad_reader USING (((NOT held) AND ((review_id IS NULL) OR (author_kind <> 'human'::public.review_author_kind) OR (EXISTS ( SELECT 1
   FROM public.reviews r
  WHERE ((r.id = review_messages.review_id) AND (r.released_at IS NOT NULL))))) AND ((author_kind = 'agent'::public.review_author_kind) OR public.review_thread_root_readable(thread_id))));

CREATE POLICY forge_held ON public.review_threads AS RESTRICTIVE FOR SELECT TO komrad_reader USING ((EXISTS ( SELECT 1
   FROM public.review_messages m
  WHERE (m.thread_id = review_threads.id))));

ALTER TABLE public.forge_review_comments ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.forge_review_threads ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.mail ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.pull_request_syncs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.pull_requests ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.review_messages ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.review_rounds ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.review_threads ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.review_viewed_files ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.reviews ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.schedule_runs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.schedules ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.workflow_configs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.workflow_event_prompts ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.workflow_hooks ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.workflow_provisioner_configs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.workflows ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.worktrees ENABLE ROW LEVEL SECURITY;

GRANT USAGE ON SCHEMA public TO komrad_engine;
GRANT USAGE ON SCHEMA public TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agent_pull_requests TO komrad_engine;
GRANT SELECT ON TABLE public.agent_pull_requests TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agent_runs TO komrad_engine;
GRANT SELECT ON TABLE public.agent_runs TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agent_screens TO komrad_engine;
GRANT SELECT ON TABLE public.agent_screens TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agent_transcript_chunks TO komrad_engine;
GRANT SELECT ON TABLE public.agent_transcript_chunks TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agent_transcript_sessions TO komrad_engine;
GRANT SELECT ON TABLE public.agent_transcript_sessions TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.agents TO komrad_engine;
GRANT SELECT ON TABLE public.agents TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.document_type_versions TO komrad_engine;
GRANT SELECT ON TABLE public.document_type_versions TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.document_types TO komrad_engine;
GRANT SELECT ON TABLE public.document_types TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.document_versions TO komrad_engine;
GRANT SELECT ON TABLE public.document_versions TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.documents TO komrad_engine;
GRANT SELECT ON TABLE public.documents TO komrad_reader;

GRANT SELECT ON TABLE public.engines TO komrad_engine;
GRANT SELECT ON TABLE public.engines TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.forge_connections TO komrad_engine;
GRANT SELECT ON TABLE public.forge_connections TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.forge_review_comments TO komrad_engine;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.forge_review_threads TO komrad_engine;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.mail TO komrad_engine;
GRANT SELECT ON TABLE public.mail TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.pull_request_syncs TO komrad_engine;
GRANT SELECT ON TABLE public.pull_request_syncs TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.pull_requests TO komrad_engine;
GRANT SELECT ON TABLE public.pull_requests TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.review_messages TO komrad_engine;
GRANT SELECT ON TABLE public.review_messages TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.review_rounds TO komrad_engine;
GRANT SELECT ON TABLE public.review_rounds TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.review_threads TO komrad_engine;
GRANT SELECT ON TABLE public.review_threads TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.review_viewed_files TO komrad_engine;
GRANT SELECT ON TABLE public.review_viewed_files TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.reviews TO komrad_engine;
GRANT SELECT ON TABLE public.reviews TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.schedule_runs TO komrad_engine;
GRANT SELECT ON TABLE public.schedule_runs TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.schedules TO komrad_engine;
GRANT SELECT ON TABLE public.schedules TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.workflow_configs TO komrad_engine;
GRANT SELECT ON TABLE public.workflow_configs TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.workflow_event_prompts TO komrad_engine;
GRANT SELECT ON TABLE public.workflow_event_prompts TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.workflow_hooks TO komrad_engine;
GRANT SELECT ON TABLE public.workflow_hooks TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.workflow_provisioner_configs TO komrad_engine;
GRANT SELECT ON TABLE public.workflow_provisioner_configs TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.workflows TO komrad_engine;
GRANT SELECT ON TABLE public.workflows TO komrad_reader;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.worktrees TO komrad_engine;
GRANT SELECT ON TABLE public.worktrees TO komrad_reader;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO komrad_engine;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO komrad_reader;

DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        GRANT SELECT ON public.schema_migrations TO komrad_engine, komrad_reader;
    END IF;
END;
$$;
