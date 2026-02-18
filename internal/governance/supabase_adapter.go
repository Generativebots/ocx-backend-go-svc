package governance

import (
	"encoding/json"

	"github.com/ocx/backend/internal/database"
)

// SupabaseConfigAdapter adapts the database.SupabaseClient to satisfy
// the governance.ConfigLoader interface. It bridges the type gap between
// TenantGovernanceConfig (governance domain) and TenantGovernanceConfigRow
// (database layer) via JSON marshaling since both types share identical tags.
type SupabaseConfigAdapter struct {
	Client *database.SupabaseClient
}

func (a *SupabaseConfigAdapter) GetTenantGovernanceConfig(tenantID string) (*TenantGovernanceConfig, error) {
	row, err := a.Client.GetTenantGovernanceConfig(tenantID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	// Convert row → domain via JSON (both share identical json tags)
	data, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var cfg TenantGovernanceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (a *SupabaseConfigAdapter) UpsertTenantGovernanceConfig(tenantID string, cfg *TenantGovernanceConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var row database.TenantGovernanceConfigRow
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	return a.Client.UpsertTenantGovernanceConfig(tenantID, &row)
}
