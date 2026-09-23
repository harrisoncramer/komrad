SELECT
    set_config('weave.engine_name', $1, TRUE),
    set_config('weave.engine_password', $2, TRUE);
