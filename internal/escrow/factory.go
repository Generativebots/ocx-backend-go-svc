package escrow

import (
	"fmt"
	"os"
)

// ClientConfig holds configuration for Jury and Entropy clients
type ClientConfig struct {
	Mode              string  // "mock" or "production"
	JuryServiceAddr   string  // e.g., "localhost:50052"
	EntropyThreshold  float64 // e.g., 1.2
	EnableLiveCapture bool    // Enable real-time socket capture
}

// NewJuryClientFromConfig creates appropriate Jury client based on config
func NewJuryClientFromConfig(config ClientConfig) (JuryClient, error) {
	switch config.Mode {
	case "production":
		if config.JuryServiceAddr == "" {
			return nil, fmt.Errorf("jury service address required for production mode")
		}
		return NewJuryGRPCClient(config.JuryServiceAddr)

	case "mock", "":
		return NewMockJuryClient(), nil

	default:
		return nil, fmt.Errorf("unknown mode: %s", config.Mode)
	}
}

// NewEntropyMonitorFromConfig creates appropriate Entropy monitor based on config
func NewEntropyMonitorFromConfig(config ClientConfig) (EntropyMonitor, error) {
	threshold := config.EntropyThreshold
	if threshold == 0 {
		threshold = 4.8 // Default max threshold
	}

	switch config.Mode {
	case "production":
		if config.EnableLiveCapture {
			return NewEntropyMonitorLive(threshold), nil
		}
		// Production mode but without live capture - use mock with production threshold
		return NewMockEntropyMonitor(), nil

	case "mock", "":
		return NewMockEntropyMonitor(), nil

	default:
		return nil, fmt.Errorf("unknown mode: %s", config.Mode)
	}
}

// NewClientConfigFromEnv creates config from environment variables
func NewClientConfigFromEnv() ClientConfig {
	mode := os.Getenv("ESCROW_MODE")
	if mode == "" {
		mode = "mock" // Default to mock for development
	}

	threshold := 4.8
	if thresholdStr := os.Getenv("ENTROPY_THRESHOLD"); thresholdStr != "" {
		fmt.Sscanf(thresholdStr, "%f", &threshold)
	}

	enableLive := os.Getenv("ENABLE_LIVE_CAPTURE") == "true"

	return ClientConfig{
		Mode:              mode,
		JuryServiceAddr:   os.Getenv("JURY_SERVICE_ADDR"),
		EntropyThreshold:  threshold,
		EnableLiveCapture: enableLive,
	}
}
