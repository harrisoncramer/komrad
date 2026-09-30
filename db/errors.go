package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
	restrictViolation   = "23001"
	undefinedTable      = "42P01"
)

// IsNoRows reports whether an error is Postgres finding nothing.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation reports whether an error is a unique index refusing a write.
func IsUniqueViolation(err error) bool {
	return hasCode(err, uniqueViolation)
}

// IsForeignKeyViolation reports whether an error is the named foreign key refusing a write.
func IsForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && (pgErr.Code == foreignKeyViolation || pgErr.Code == restrictViolation) && pgErr.ConstraintName == constraint
}

// isUndefinedTable reports whether an error is a query against a table that does not exist.
func isUndefinedTable(err error) bool {
	return hasCode(err, undefinedTable)
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == code
}
