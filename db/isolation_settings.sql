SELECT
    SET_CONFIG('komrad.unisolated_tables', ARRAY_TO_STRING($1::text[], ','), TRUE),
    SET_CONFIG('komrad.readerless_tables', ARRAY_TO_STRING($2::text[], ','), TRUE);
