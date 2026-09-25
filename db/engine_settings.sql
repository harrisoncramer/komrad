SELECT
    set_config('komrad.engine_name', $1, TRUE),
    set_config('komrad.engine_password', $2, TRUE);
