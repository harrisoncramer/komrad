-- name: AttachWatchTrigger :exec
SELECT
    attach_watch_trigger(@target::text, @key_column::text);

-- name: AttachProjectionTrigger :exec
SELECT
    attach_projection_trigger(@target::text, @parent::text, @parent_column::text);

-- name: AttachProjectionLookupTrigger :exec
SELECT
    attach_projection_lookup_trigger(@target::text, @parent::text, @parent_key::text, @parent_match::text, @match::text, sqlc.narg(parent_must_be_null)::text);

-- name: ListenToWatchChannel :exec
LISTEN weave_watch;
