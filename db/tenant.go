package db

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultTenantID is the tenant every row belongs to until a tenant comes from authentication.
const DefaultTenantID = "00000000-0000-7000-8000-000000000001"

// TenantSetting is the connection setting every tenant_id column takes its default from.
const TenantSetting = "weave.tenant"

// ScopeToTenant makes every connection the pool opens act for tenantID.
func ScopeToTenant(cfg *pgxpool.Config, tenantID string) {
	cfg.ConnConfig.RuntimeParams[TenantSetting] = tenantID
}
