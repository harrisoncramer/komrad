SELECT
    SET_CONFIG('komrad.engine_name', $1, TRUE),
    SET_CONFIG('komrad.engine_password', $2, TRUE);
