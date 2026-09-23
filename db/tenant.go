package db

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantSetting is the connection setting every tenant_id column takes its default from.
const TenantSetting = "weave.tenant"

// ScopeToTenant makes every connection the pool opens act for tenantID.
func ScopeToTenant(cfg *pgxpool.Config, tenantID string) {
	cfg.ConnConfig.RuntimeParams[TenantSetting] = tenantID
}
