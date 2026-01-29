package reputation

import (
	"fmt"
	"os"
)

// WalletConfig holds configuration for reputation storage
type WalletConfig struct {
	Backend         string // "sqlite" or "spanner"
	SQLitePath      string
	SpannerProject  string
	SpannerInstance string
	SpannerDatabase string
}

// NewReputationStore creates the appropriate wallet based on configuration
func NewReputationStore(config WalletConfig) (ReputationStore, error) {
	switch config.Backend {
	case "spanner":
		if config.SpannerProject == "" || config.SpannerInstance == "" || config.SpannerDatabase == "" {
			return nil, fmt.Errorf("spanner configuration incomplete")
		}
		return NewSpannerWallet(config.SpannerProject, config.SpannerInstance, config.SpannerDatabase)

	case "sqlite", "":
		// Default to SQLite for local development
		dbPath := config.SQLitePath
		if dbPath == "" {
			dbPath = "reputation.db"
		}
		return NewWallet(dbPath)

	default:
		return nil, fmt.Errorf("unknown backend: %s", config.Backend)
	}
}

// NewReputationStoreFromEnv creates a wallet from environment variables
func NewReputationStoreFromEnv() (ReputationStore, error) {
	backend := os.Getenv("REPUTATION_BACKEND")
	if backend == "" {
		backend = "sqlite" // Default
	}

	config := WalletConfig{
		Backend:         backend,
		SQLitePath:      os.Getenv("REPUTATION_SQLITE_PATH"),
		SpannerProject:  os.Getenv("SPANNER_PROJECT_ID"),
		SpannerInstance: os.Getenv("SPANNER_INSTANCE_ID"),
		SpannerDatabase: os.Getenv("SPANNER_DATABASE_ID"),
	}

	return NewReputationStore(config)
}
