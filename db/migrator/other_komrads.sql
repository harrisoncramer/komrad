SELECT DISTINCT
    application_name
FROM
    pg_stat_activity
WHERE
    datname = CURRENT_DATABASE()
    AND pid <> PG_BACKEND_PID()
    AND application_name LIKE $1
    AND application_name <> $2
ORDER BY
    application_name;
