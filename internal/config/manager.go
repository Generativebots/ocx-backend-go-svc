package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v2"
)

// TenantsConfig holds map of tenant overrides
type TenantsConfig struct {
	Tenants map[string]Config `yaml:"tenants"`
}

// Manager handles dynamic configuration resolution
type Manager struct {
	globalConfig  *Config
	tenantConfigs map[string]Config
	mu            sync.RWMutex
}

// NewManager loads both master and tenant configs
func NewManager(masterPath, tenantsPath string) (*Manager, error) {
	// Load Master
	master, err := LoadConfig(masterPath)
	if err != nil {
		return nil, err
	}

	// Load Tenants
	f, err := os.Open(tenantsPath)
	if err != nil {
		// If tenants file missing, just use empty map
		if os.IsNotExist(err) {
			return &Manager{globalConfig: master, tenantConfigs: make(map[string]Config)}, nil
		}
		return nil, err
	}
	defer f.Close()

	var tc TenantsConfig
	if err := yaml.NewDecoder(f).Decode(&tc); err != nil {
		return nil, err
	}

	return &Manager{
		globalConfig:  master,
		tenantConfigs: tc.Tenants,
	}, nil
}

// Get returns the effective config for a tenant
// It merges tenant overrides on top of the global config
func (m *Manager) Get(tenantID string) *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Start with a copy of the global config
	effective := *m.globalConfig

	// Apply overrides if they exist
	if override, ok := m.tenantConfigs[tenantID]; ok {
		// Trust Weights
		if override.Trust.Weights.Audit != 0 || override.Trust.Weights.Reputation != 0 {
			effective.Trust.Weights = override.Trust.Weights
		}
		// Trust Secrets
		if override.Trust.Secrets.VisualKey != "" {
			effective.Trust.Secrets = override.Trust.Secrets
		}

		// Handshake
		if override.Handshake.SessionExpiryMinutes != 0 {
			effective.Handshake = override.Handshake
		}

		// Network
		if override.Network.InitialNodeCount != 0 {
			effective.Network = override.Network
		}

		// Governance (Committee size usually global, but let's allow override)
		if override.Governance.CommitteeSize != 0 {
			effective.Governance = override.Governance
		}

		// Contracts
		if override.Contracts.RuntimeTimeoutMs != 0 {
			effective.Contracts = override.Contracts
		}

		// Monitoring
		if override.Monitoring.EntropyThreshold != 0 {
			effective.Monitoring = override.Monitoring
		}

		// Simulation
		if override.Simulation.MaxBatchSize != 0 {
			effective.Simulation = override.Simulation
		}

		// Impact
		if override.Impact.MonteCarloIterations != 0 {
			effective.Impact = override.Impact
		}
	}

	return &effective
}
