package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	uniqueViolation = "23505"
	undefinedTable  = "42P01"
)

// IsNoRows reports whether an error is Postgres finding nothing.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation reports whether an error is a unique index refusing a write.
func IsUniqueViolation(err error) bool {
	return hasCode(err, uniqueViolation)
}

// isUndefinedTable reports whether an error is a query against a table that does not exist.
func isUndefinedTable(err error) bool {
	return hasCode(err, undefinedTable)
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == code
}
