package models

import "context"

type DBTX interface {
	Exec(ctx context.Context, sql string) error
}
