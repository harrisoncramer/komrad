SELECT
    set_config('komrade.engine_name', $1, TRUE),
    set_config('komrade.engine_password', $2, TRUE);
