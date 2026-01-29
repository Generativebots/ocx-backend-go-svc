package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Trust      TrustConfig      `yaml:"trust"`
	Handshake  HandshakeConfig  `yaml:"handshake"`
	Governance GovernanceConfig `yaml:"governance"`
	Network    NetworkConfig    `yaml:"network"`
	Contracts  ContractsConfig  `yaml:"contracts"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Simulation SimulationConfig `yaml:"simulation"`
	Impact     ImpactConfig     `yaml:"impact"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
	Env  string `yaml:"env"`
}

type TrustConfig struct {
	Weights TrustWeights `yaml:"weights"`
	Secrets TrustSecrets `yaml:"secrets"`
}

type TrustWeights struct {
	Audit       float64 `yaml:"audit"`
	Reputation  float64 `yaml:"reputation"`
	Attestation float64 `yaml:"attestation"`
	History     float64 `yaml:"history"`
}

type TrustSecrets struct {
	VisualKey string `yaml:"visual_key"`
	TestKey   string `yaml:"test_key"`
}

type HandshakeConfig struct {
	SessionExpiryMinutes int      `yaml:"session_expiry_minutes"`
	SupportedVersions    []string `yaml:"supported_versions"`
}

type GovernanceConfig struct {
	CommitteeSize int `yaml:"committee_size"`
}

type NetworkConfig struct {
	InitialNodeCount float64 `yaml:"initial_node_count"`
}

type ContractsConfig struct {
	RuntimeTimeoutMs   int  `yaml:"runtime_timeout_ms"`
	EnforceUseCaseLink bool `yaml:"enforce_use_case_link"`
	MaxActiveContracts int  `yaml:"max_active_contracts"`
}

type MonitoringConfig struct {
	EntropyThreshold float64 `yaml:"entropy_threshold"`
	LatencyAlertMs   int     `yaml:"latency_alert_ms"`
	EnableLiveStream bool    `yaml:"enable_live_stream"`
}

type SimulationConfig struct {
	MaxBatchSize  int `yaml:"max_batch_size"`
	RetentionDays int `yaml:"retention_days"`
}

type ImpactConfig struct {
	MonteCarloIterations int `yaml:"monte_carlo_iterations"`
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
