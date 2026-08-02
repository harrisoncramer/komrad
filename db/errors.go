package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The SQLSTATE codes read here rather than the driver's message text, so a check does not
// depend on wording that can change under it.
const (
	uniqueViolation = "23505"
	undefinedTable  = "42P01"
)

// NoRows reports whether an error is Postgres finding nothing, which every domain store
// turns into its own empty answer rather than an error.
func NoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// UniqueViolation reports whether an error is a unique index refusing a write.
func UniqueViolation(err error) bool {
	return hasCode(err, uniqueViolation)
}

// UndefinedTable reports whether an error is a query against a table that does not exist,
// which on the first run is schema_migrations itself.
func UndefinedTable(err error) bool {
	return hasCode(err, undefinedTable)
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == code
}
