package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
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
	Federation FederationConfig `yaml:"federation"`
	Webhook    WebhookConfig    `yaml:"webhook"`
	Evidence   EvidenceConfig   `yaml:"evidence"`
	PubSub     PubSubConfig     `yaml:"pubsub"`
	CloudTasks CloudTasksConfig `yaml:"cloud_tasks"`
	Security   SecurityConfig   `yaml:"security"`
	Sovereign  SovereignConfig  `yaml:"sovereign"`
}

type ServerConfig struct {
	Port             string   `yaml:"port"`
	Env              string   `yaml:"env"`
	Interface        string   `yaml:"interface"`
	ReadTimeoutSec   int      `yaml:"read_timeout_sec"`
	WriteTimeoutSec  int      `yaml:"write_timeout_sec"`
	IdleTimeoutSec   int      `yaml:"idle_timeout_sec"`
	ShutdownTimeout  int      `yaml:"shutdown_timeout_sec"`
	CORSAllowOrigins []string `yaml:"cors_allow_origins"`
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
	DefaultTrustScore float64 `yaml:"default_trust_score"`
	FailureTaxRate    float64 `yaml:"failure_tax_rate"`
	JITEntitlementTTL int     `yaml:"jit_entitlement_ttl_sec"`
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

// FederationConfig for inter-OCX federation identity
type FederationConfig struct {
	InstanceID   string `yaml:"instance_id"`
	TrustDomain  string `yaml:"trust_domain"`
	Region       string `yaml:"region"`
	Organization string `yaml:"organization"`
}

// WebhookConfig for webhook dispatcher
type WebhookConfig struct {
	WorkerCount int `yaml:"worker_count"`
}

// EvidenceConfig for evidence vault
type EvidenceConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

// PubSubConfig for Google Cloud Pub/Sub event bus
type PubSubConfig struct {
	ProjectID string `yaml:"project_id"`
	TopicID   string `yaml:"topic_id"`
	Enabled   bool   `yaml:"enabled"`
}

// CloudTasksConfig for webhook delivery via Google Cloud Tasks
type CloudTasksConfig struct {
	ProjectID  string `yaml:"project_id"`
	LocationID string `yaml:"location_id"`
	QueueID    string `yaml:"queue_id"`
	Enabled    bool   `yaml:"enabled"`
}

// SecurityConfig for Token Broker and Continuous Access Evaluation (Claims 7+8)
type SecurityConfig struct {
	HMACSecret          string  `yaml:"hmac_secret"`
	TokenTTLSec         int     `yaml:"token_ttl_sec"`
	MinTrustForToken    float64 `yaml:"min_trust_for_token"`
	MaxTokensPerAgent   int     `yaml:"max_tokens_per_agent"`
	CAESweepIntervalSec int     `yaml:"cae_sweep_interval_sec"`
	DriftThreshold      float64 `yaml:"drift_threshold"`
	TrustDropLimit      float64 `yaml:"trust_drop_limit"`
	AnomalyThreshold    int     `yaml:"anomaly_threshold"`
}

// SovereignConfig for Sovereign Mode (Claim 12)
type SovereignConfig struct {
	Enabled                bool   `yaml:"enabled"`
	LocalInferenceEndpoint string `yaml:"local_inference_endpoint"`
	BoundaryEnforced       bool   `yaml:"boundary_enforced"`
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
		cfg, err := LoadConfig(getEnv("CONFIG_PATH", "config.yaml"))
		if err != nil {
			slog.Warn("Config: failed to load config file: (using defaults)", "error", err)
		}
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

	// Server timeouts
	if v := getEnvInt("SERVER_READ_TIMEOUT_SEC", 0); v > 0 {
		c.Server.ReadTimeoutSec = v
	}
	if v := getEnvInt("SERVER_WRITE_TIMEOUT_SEC", 0); v > 0 {
		c.Server.WriteTimeoutSec = v
	}
	if v := getEnvInt("SERVER_IDLE_TIMEOUT_SEC", 0); v > 0 {
		c.Server.IdleTimeoutSec = v
	}
	if v := getEnvInt("SERVER_SHUTDOWN_TIMEOUT_SEC", 0); v > 0 {
		c.Server.ShutdownTimeout = v
	}
	if origins := getEnv("CORS_ALLOW_ORIGINS", ""); origins != "" {
		c.Server.CORSAllowOrigins = splitCSV(origins)
	}

	// Escrow extended
	if v := getEnvFloat("DEFAULT_TRUST_SCORE", 0); v > 0 {
		c.Escrow.DefaultTrustScore = v
	}
	if v := getEnvFloat("FAILURE_TAX_RATE", 0); v > 0 {
		c.Escrow.FailureTaxRate = v
	}
	if v := getEnvInt("JIT_ENTITLEMENT_TTL_SEC", 0); v > 0 {
		c.Escrow.JITEntitlementTTL = v
	}

	// Federation
	c.Federation.InstanceID = getEnv("OCX_INSTANCE_ID", c.Federation.InstanceID)
	c.Federation.TrustDomain = getEnv("OCX_TRUST_DOMAIN", c.Federation.TrustDomain)
	c.Federation.Region = getEnv("OCX_REGION", c.Federation.Region)
	c.Federation.Organization = getEnv("OCX_ORG", c.Federation.Organization)

	// Webhooks
	if v := getEnvInt("WEBHOOK_WORKERS", 0); v > 0 {
		c.Webhook.WorkerCount = v
	}

	// Evidence
	if v := getEnvInt("EVIDENCE_RETENTION_DAYS", 0); v > 0 {
		c.Evidence.RetentionDays = v
	}

	// Pub/Sub
	if projectID := getEnv("GCP_PROJECT_ID", ""); projectID != "" {
		c.PubSub.ProjectID = projectID
		c.CloudTasks.ProjectID = projectID // share project
	}
	c.PubSub.TopicID = getEnv("PUBSUB_TOPIC_ID", c.PubSub.TopicID)
	c.PubSub.Enabled = getEnvBool("PUBSUB_ENABLED", c.PubSub.Enabled)

	// Cloud Tasks
	c.CloudTasks.LocationID = getEnv("CLOUD_TASKS_LOCATION", c.CloudTasks.LocationID)
	c.CloudTasks.QueueID = getEnv("CLOUD_TASKS_QUEUE", c.CloudTasks.QueueID)
	c.CloudTasks.Enabled = getEnvBool("CLOUD_TASKS_ENABLED", c.CloudTasks.Enabled)

	// Security (Claims 7+8)
	c.Security.HMACSecret = getEnv("OCX_HMAC_SECRET", c.Security.HMACSecret)
	if v := getEnvInt("OCX_TOKEN_TTL_SEC", 0); v > 0 {
		c.Security.TokenTTLSec = v
	}
	if v := getEnvFloat("OCX_MIN_TRUST_FOR_TOKEN", 0); v > 0 {
		c.Security.MinTrustForToken = v
	}

	// Sovereign Mode (Claim 12)
	c.Sovereign.Enabled = getEnvBool("OCX_SOVEREIGN_MODE", c.Sovereign.Enabled)
	c.Sovereign.LocalInferenceEndpoint = getEnv("OCX_LOCAL_INFERENCE_ENDPOINT", c.Sovereign.LocalInferenceEndpoint)
	c.Sovereign.BoundaryEnforced = getEnvBool("OCX_BOUNDARY_ENFORCED", c.Sovereign.BoundaryEnforced)

	// Apply defaults for zero values
	c.applyDefaults()
}

// applyDefaults sets sensible defaults for zero-valued config fields
func (c *Config) applyDefaults() {
	if c.Server.Port == "" {
		c.Server.Port = "8080"
	}
	if c.Server.ReadTimeoutSec == 0 {
		c.Server.ReadTimeoutSec = 15
	}
	if c.Server.WriteTimeoutSec == 0 {
		c.Server.WriteTimeoutSec = 15
	}
	if c.Server.IdleTimeoutSec == 0 {
		c.Server.IdleTimeoutSec = 60
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = 30
	}
	if len(c.Server.CORSAllowOrigins) == 0 {
		c.Server.CORSAllowOrigins = []string{"*"}
	}
	if c.Escrow.EntropyThreshold == 0 {
		c.Escrow.EntropyThreshold = 1.2
	}
	if c.Escrow.DefaultTrustScore == 0 {
		c.Escrow.DefaultTrustScore = 0.5
	}
	if c.Escrow.FailureTaxRate == 0 {
		c.Escrow.FailureTaxRate = 1.5
	}
	if c.Escrow.JITEntitlementTTL == 0 {
		c.Escrow.JITEntitlementTTL = 300 // 5 minutes
	}
	if c.Federation.InstanceID == "" {
		c.Federation.InstanceID = "ocx-local"
	}
	if c.Federation.TrustDomain == "" {
		c.Federation.TrustDomain = "spiffe://ocx-local"
	}
	if c.Webhook.WorkerCount == 0 {
		c.Webhook.WorkerCount = 4
	}
	if c.Evidence.RetentionDays == 0 {
		c.Evidence.RetentionDays = 365
	}
	if c.PubSub.TopicID == "" {
		c.PubSub.TopicID = "ocx-events"
	}
	if c.CloudTasks.LocationID == "" {
		c.CloudTasks.LocationID = "us-central1"
	}
	if c.CloudTasks.QueueID == "" {
		c.CloudTasks.QueueID = "ocx-webhooks"
	}
	// Security defaults
	if c.Security.TokenTTLSec == 0 {
		c.Security.TokenTTLSec = 300 // 5 minutes
	}
	if c.Security.MinTrustForToken == 0 {
		c.Security.MinTrustForToken = 0.65
	}
	if c.Security.MaxTokensPerAgent == 0 {
		c.Security.MaxTokensPerAgent = 50
	}
	if c.Security.CAESweepIntervalSec == 0 {
		c.Security.CAESweepIntervalSec = 10
	}
	if c.Security.DriftThreshold == 0 {
		c.Security.DriftThreshold = 0.20
	}
	if c.Security.TrustDropLimit == 0 {
		c.Security.TrustDropLimit = 0.15
	}
	if c.Security.AnomalyThreshold == 0 {
		c.Security.AnomalyThreshold = 5
	}
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

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func splitCSV(s string) []string {
	parts := make([]string, 0)
	for _, p := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
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
