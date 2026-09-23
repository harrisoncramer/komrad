package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultTenantID is the tenant every row belongs to until a tenant comes from authentication.
const DefaultTenantID = "00000000-0000-7000-8000-000000000001"

// TenantSetting is the connection setting every tenant_id column takes its default from.
const TenantSetting = "weave.tenant"

// ScopeToTenant makes every connection the pool opens act for tenantID.
func ScopeToTenant(cfg *pgxpool.Config, tenantID string) {
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, TenantSetting, tenantID); err != nil {
			return fmt.Errorf("failed to set the connection's tenant: %w", err)
		}

		return nil
	}
}
