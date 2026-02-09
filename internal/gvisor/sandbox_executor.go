package gvisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// SandboxExecutor manages gVisor sandboxes for speculative execution
type SandboxExecutor struct {
	runscPath     string
	rootfsPath    string
	networkPolicy string // "none" for isolated execution
	available     bool   // false if runsc binary not found
}

// ToolCallPayload represents an intercepted MCP tool call
type ToolCallPayload struct {
	TransactionID string                 `json:"transaction_id"`
	AgentID       string                 `json:"agent_id"`
	ToolName      string                 `json:"tool_name"`
	Parameters    map[string]interface{} `json:"parameters"`
	Context       map[string]interface{} `json:"context"`
}

// ExecutionResult represents the output from sandbox execution
type ExecutionResult struct {
	TransactionID string                 `json:"transaction_id"`
	Success       bool                   `json:"success"`
	Output        map[string]interface{} `json:"output"`
	Error         string                 `json:"error,omitempty"`
	ExecutionTime time.Duration          `json:"execution_time"`
	RevertToken   string                 `json:"revert_token"`
	DemoMode      bool                   `json:"demo_mode,omitempty"`
}

// NewSandboxExecutor creates a new gVisor sandbox executor.
// C2 FIX: Checks if runsc binary exists and sets available flag.
func NewSandboxExecutor(runscPath, rootfsPath string) *SandboxExecutor {
	if runscPath == "" {
		runscPath = "/usr/local/bin/runsc" // Default gVisor runtime path
	}

	// C2 FIX: Check if runsc binary actually exists
	available := true
	if _, err := exec.LookPath(runscPath); err != nil {
		log.Printf("⚠️  gVisor runsc not found at %s: %v (sandbox will run in demo mode)", runscPath, err)
		available = false
	}

	return &SandboxExecutor{
		runscPath:     runscPath,
		rootfsPath:    rootfsPath,
		networkPolicy: "none", // No outbound network access
		available:     available,
	}
}

// RunscPath returns the configured runsc binary path
func (se *SandboxExecutor) RunscPath() string {
	return se.runscPath
}

// IsAvailable returns whether the gVisor runtime is installed and usable
func (se *SandboxExecutor) IsAvailable() bool {
	return se.available
}

// ExecuteSpeculative runs a tool call in an isolated gVisor sandbox
// C2 FIX: Returns demo-mode result if runsc is not available (instead of crashing)
func (se *SandboxExecutor) ExecuteSpeculative(ctx context.Context, payload *ToolCallPayload) (*ExecutionResult, error) {
	startTime := time.Now()

	log.Printf("🔮 Starting speculative execution for transaction: %s", payload.TransactionID)
	log.Printf("   Tool: %s, Agent: %s", payload.ToolName, payload.AgentID)

	// C2 FIX: If runsc is not available, return a demo-mode result instead of crashing
	if !se.available {
		log.Printf("⚠️  gVisor not available — returning demo-mode speculative result")
		return &ExecutionResult{
			TransactionID: payload.TransactionID,
			Success:       true,
			Output: map[string]interface{}{
				"mode":    "demo",
				"message": "gVisor sandbox unavailable — speculative execution simulated",
				"tool":    payload.ToolName,
			},
			ExecutionTime: time.Since(startTime),
			RevertToken:   se.generateRevertToken(payload),
			DemoMode:      true,
		}, nil
	}

	// 1. Generate revert token (for state cleanup)
	revertToken := se.generateRevertToken(payload)

	// 2. Create isolated sandbox
	sandboxID := fmt.Sprintf("ocx-sandbox-%s", uuid.New().String()[:8])

	// L1 FIX: defer cleanup BEFORE the work it guards, so it always runs on
	// function exit (previously placed after runInGVisor, same semantics but
	// confusing placement that looked like cleanup-after-return)
	defer se.cleanupSandbox(sandboxID)

	// 3. Prepare sandbox configuration
	config := se.prepareSandboxConfig(sandboxID, payload)

	// 4. Execute in gVisor
	output, err := se.runInGVisor(ctx, sandboxID, config, payload)

	result := &ExecutionResult{
		TransactionID: payload.TransactionID,
		Success:       err == nil,
		Output:        output,
		ExecutionTime: time.Since(startTime),
		RevertToken:   revertToken,
	}

	if err != nil {
		result.Error = err.Error()
		log.Printf("❌ Speculative execution failed: %v", err)
	} else {
		log.Printf("✅ Speculative execution complete: %s (took %v)", payload.TransactionID, result.ExecutionTime)
	}

	return result, err
}

// prepareSandboxConfig creates the gVisor runtime configuration
func (se *SandboxExecutor) prepareSandboxConfig(sandboxID string, payload *ToolCallPayload) map[string]interface{} {
	return map[string]interface{}{
		"sandbox_id":           sandboxID,
		"network":              se.networkPolicy,
		"platform":             "ptrace", // Use ptrace for compatibility (can use kvm for performance)
		"file_access":          "exclusive",
		"overlay":              true,
		"debug":                false,
		"strace":               false,
		"num_network_channels": 0, // No network
		"rootfs":               se.rootfsPath,
		"tool_name":            payload.ToolName,
		"parameters":           payload.Parameters,
	}
}

// runInGVisor executes the tool call in gVisor sandbox
func (se *SandboxExecutor) runInGVisor(ctx context.Context, sandboxID string, config map[string]interface{}, payload *ToolCallPayload) (map[string]interface{}, error) {
	// Create temporary directory for sandbox
	sandboxDir := filepath.Join("/tmp", "ocx-sandboxes", sandboxID)
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox dir: %w", err)
	}

	// Write payload to sandbox input file
	payloadPath := filepath.Join(sandboxDir, "input.json")
	payloadData, _ := json.Marshal(payload)
	if err := os.WriteFile(payloadPath, payloadData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write payload: %w", err)
	}

	// Prepare gVisor command
	// runsc run --network=none --platform=ptrace --rootfs=/path/to/rootfs <sandbox-id>
	cmd := exec.CommandContext(ctx,
		se.runscPath,
		"run",
		"--network=none",
		"--platform=ptrace",
		fmt.Sprintf("--rootfs=%s", se.rootfsPath),
		fmt.Sprintf("--bundle=%s", sandboxDir),
		sandboxID,
	)

	// Capture stdout/stderr
	var stdout, stderr []byte
	var err error

	stdout, err = cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = exitErr.Stderr
		}
		return nil, fmt.Errorf("gVisor execution failed: %w (stderr: %s)", err, string(stderr))
	}

	// Parse output
	var output map[string]interface{}
	if err := json.Unmarshal(stdout, &output); err != nil {
		// If output is not JSON, wrap it
		output = map[string]interface{}{
			"raw_output": string(stdout),
		}
	}

	return output, nil
}

// generateRevertToken creates a unique token for state cleanup
func (se *SandboxExecutor) generateRevertToken(payload *ToolCallPayload) string {
	// Token format: <transaction-id>:<timestamp>:<hash>
	timestamp := time.Now().Unix()
	hash := fmt.Sprintf("%x", payload.TransactionID)
	// L2 FIX: Safe truncation — previous [:8] could panic if hash < 8 chars
	if len(hash) > 8 {
		hash = hash[:8]
	}
	return fmt.Sprintf("%s:%d:%s", payload.TransactionID, timestamp, hash)
}

// cleanupSandbox removes the gVisor sandbox and temporary files
func (se *SandboxExecutor) cleanupSandbox(sandboxID string) {
	// Kill sandbox if still running
	cmd := exec.Command(se.runscPath, "kill", sandboxID)
	cmd.Run() // Ignore errors

	// Delete sandbox
	cmd = exec.Command(se.runscPath, "delete", sandboxID)
	cmd.Run() // Ignore errors

	// Remove temporary directory
	sandboxDir := filepath.Join("/tmp", "ocx-sandboxes", sandboxID)
	os.RemoveAll(sandboxDir)

	log.Printf("🧹 Cleaned up sandbox: %s", sandboxID)
}

// VerifySandboxIsolation checks that sandbox has no network access
func (se *SandboxExecutor) VerifySandboxIsolation(sandboxID string) error {
	// Check sandbox state
	cmd := exec.Command(se.runscPath, "state", sandboxID)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get sandbox state: %w", err)
	}

	var state map[string]interface{}
	if err := json.Unmarshal(output, &state); err != nil {
		return fmt.Errorf("failed to parse sandbox state: %w", err)
	}

	// Verify network is disabled
	if network, ok := state["network"].(string); ok && network != "none" {
		return fmt.Errorf("sandbox has network access: %s", network)
	}

	log.Printf("✓ Sandbox isolation verified: %s", sandboxID)
	return nil
}

// Example usage
func ExampleSandboxExecutor() {
	executor := NewSandboxExecutor("/usr/local/bin/runsc", "/var/ocx/rootfs")

	payload := &ToolCallPayload{
		TransactionID: "tx-12345",
		AgentID:       "PROCUREMENT_BOT",
		ToolName:      "execute_payment",
		Parameters: map[string]interface{}{
			"vendor": "ACME",
			"amount": 1500,
		},
		Context: map[string]interface{}{
			"user_id": "alice",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteSpeculative(ctx, payload)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Result: %+v\n", result)
	fmt.Printf("Revert Token: %s\n", result.RevertToken)
}
