package store

import (
	"context"

	"komrad/db/models"
)

type DBTX = models.DBTX

type Pool struct{}

func (p *Pool) InTx(ctx context.Context, fn func(DBTX) error) error {
	return fn(nil)
}
