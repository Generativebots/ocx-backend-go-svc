package config

import (
	"os"
	"strconv"
	"sync"

	"gopkg.in/yaml.v2"
)

// =============================================================================
// OCX Go Backend - Enhanced Configuration with Environment Overrides
// =============================================================================

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	Reputation ReputationConfig `yaml:"reputation"`
	Escrow     EscrowConfig     `yaml:"escrow"`
	Trust      TrustConfig      `yaml:"trust"`
	Handshake  HandshakeConfig  `yaml:"handshake"`
	Governance GovernanceConfig `yaml:"governance"`
	Network    NetworkConfig    `yaml:"network"`
	Contracts  ContractsConfig  `yaml:"contracts"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Simulation SimulationConfig `yaml:"simulation"`
	Impact     ImpactConfig     `yaml:"impact"`
	Services   ServicesConfig   `yaml:"services"`
}

type ServerConfig struct {
	Port      string `yaml:"port"`
	Env       string `yaml:"env"`
	Interface string `yaml:"interface"`
}

// DatabaseConfig for Supabase
type DatabaseConfig struct {
	Supabase SupabaseConfig `yaml:"supabase"`
}

type SupabaseConfig struct {
	URL        string `yaml:"url"`
	ServiceKey string `yaml:"service_key"`
}

// ReputationConfig for reputation backend
type ReputationConfig struct {
	Backend    string        `yaml:"backend"`
	SQLitePath string        `yaml:"sqlite_path"`
	Spanner    SpannerConfig `yaml:"spanner"`
}

type SpannerConfig struct {
	ProjectID  string `yaml:"project_id"`
	InstanceID string `yaml:"instance_id"`
	DatabaseID string `yaml:"database_id"`
}

// EscrowConfig for escrow service
type EscrowConfig struct {
	Mode              string  `yaml:"mode"`
	EntropyThreshold  float64 `yaml:"entropy_threshold"`
	EnableLiveCapture bool    `yaml:"enable_live_capture"`
	JuryServiceAddr   string  `yaml:"jury_service_addr"`
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
	CommitteeSize     int     `yaml:"committee_size"`
	QuorumPercentage  float64 `yaml:"quorum_percentage"`
	VotingPeriodHours int     `yaml:"voting_period_hours"`
}

type NetworkConfig struct {
	InitialNodeCount    int `yaml:"initial_node_count"`
	MaxConnections      int `yaml:"max_connections"`
	HeartbeatIntervalMs int `yaml:"heartbeat_interval_ms"`
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

// ServicesConfig contains URLs for Python services
type ServicesConfig struct {
	TrustRegistryURL    string `yaml:"trust_registry_url"`
	JuryGRPCAddr        string `yaml:"jury_grpc_addr"`
	ActivityRegistryURL string `yaml:"activity_registry_url"`
	EvidenceVaultURL    string `yaml:"evidence_vault_url"`
	AuthorityURL        string `yaml:"authority_url"`
	MonitorURL          string `yaml:"monitor_url"`
}

// =============================================================================
// Singleton Pattern with Environment Overrides
// =============================================================================

var (
	instance *Config
	once     sync.Once
)

// Get returns the singleton config instance
func Get() *Config {
	once.Do(func() {
		cfg, _ := LoadConfig(getEnv("CONFIG_PATH", "config.yaml"))
		if cfg == nil {
			cfg = &Config{}
		}
		cfg.applyEnvOverrides()
		instance = cfg
	})
	return instance
}

// LoadConfig loads config from YAML file
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

// applyEnvOverrides applies environment variable overrides
func (c *Config) applyEnvOverrides() {
	// Server
	c.Server.Port = getEnv("PORT", c.Server.Port)
	c.Server.Env = getEnv("OCX_ENV", c.Server.Env)
	c.Server.Interface = getEnv("OCX_INTERFACE", c.Server.Interface)

	// Database - Supabase
	c.Database.Supabase.URL = getEnv("SUPABASE_URL", c.Database.Supabase.URL)
	c.Database.Supabase.ServiceKey = getEnv("SUPABASE_SERVICE_KEY", c.Database.Supabase.ServiceKey)

	// Reputation
	c.Reputation.Backend = getEnv("REPUTATION_BACKEND", c.Reputation.Backend)
	c.Reputation.SQLitePath = getEnv("REPUTATION_SQLITE_PATH", c.Reputation.SQLitePath)
	c.Reputation.Spanner.ProjectID = getEnv("SPANNER_PROJECT_ID", c.Reputation.Spanner.ProjectID)
	c.Reputation.Spanner.InstanceID = getEnv("SPANNER_INSTANCE_ID", c.Reputation.Spanner.InstanceID)
	c.Reputation.Spanner.DatabaseID = getEnv("SPANNER_DATABASE_ID", c.Reputation.Spanner.DatabaseID)

	// Escrow
	c.Escrow.Mode = getEnv("ESCROW_MODE", c.Escrow.Mode)
	if v := getEnvFloat("ENTROPY_THRESHOLD", 0); v > 0 {
		c.Escrow.EntropyThreshold = v
	}
	c.Escrow.EnableLiveCapture = getEnvBool("ENABLE_LIVE_CAPTURE", c.Escrow.EnableLiveCapture)
	c.Escrow.JuryServiceAddr = getEnv("JURY_SERVICE_ADDR", c.Escrow.JuryServiceAddr)

	// Trust secrets
	c.Trust.Secrets.VisualKey = getEnv("VISUAL_SECRET_KEY", c.Trust.Secrets.VisualKey)
	c.Trust.Secrets.TestKey = getEnv("TEST_SECRET_KEY", c.Trust.Secrets.TestKey)

	// Services (Python service URLs)
	c.Services.TrustRegistryURL = getEnv("TRUST_REGISTRY_URL", c.Services.TrustRegistryURL)
	c.Services.JuryGRPCAddr = getEnv("JURY_SERVICE_ADDR", c.Services.JuryGRPCAddr)
	c.Services.ActivityRegistryURL = getEnv("ACTIVITY_REGISTRY_URL", c.Services.ActivityRegistryURL)
	c.Services.EvidenceVaultURL = getEnv("EVIDENCE_VAULT_URL", c.Services.EvidenceVaultURL)
	c.Services.AuthorityURL = getEnv("AUTHORITY_URL", c.Services.AuthorityURL)
	c.Services.MonitorURL = getEnv("MONITOR_URL", c.Services.MonitorURL)
}

// =============================================================================
// Helper Functions
// =============================================================================

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// =============================================================================
// Convenience Methods
// =============================================================================

func (c *Config) IsProduction() bool {
	return c.Server.Env == "production"
}

func (c *Config) IsDevelopment() bool {
	return c.Server.Env == "development"
}

func (c *Config) GetPort() string {
	if c.Server.Port == "" {
		return "8080"
	}
	return c.Server.Port
}

// GetSupabaseURL returns the Supabase URL
func (c *Config) GetSupabaseURL() string {
	return c.Database.Supabase.URL
}

// GetSupabaseKey returns the Supabase service key
func (c *Config) GetSupabaseKey() string {
	return c.Database.Supabase.ServiceKey
}
