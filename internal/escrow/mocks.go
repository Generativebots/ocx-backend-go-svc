package escrow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// MockJuryClient simulates the Python Jury service
type MockJuryClient struct{}

func NewMockJuryClient() *MockJuryClient {
	return &MockJuryClient{}
}

func (m *MockJuryClient) EvaluateAction(ctx context.Context, agentID, action string, context map[string]interface{}) (bool, error) {
	// Simulate Jury evaluation logic
	// Block on obvious violations
	actionLower := strings.ToLower(action)

	// Critical failures
	if strings.Contains(actionLower, "ignore all previous instructions") {
		return false, fmt.Errorf("security threat detected")
	}

	if strings.Contains(actionLower, "@") && strings.Contains(actionLower, "public") {
		return false, fmt.Errorf("PII leak detected")
	}

	// Simulate 95% approval rate for normal actions
	// In production, this would call the actual Jury gRPC service
	return true, nil
}

// MockEntropyMonitor simulates Shannon Entropy calculation
type MockEntropyMonitor struct {
	BaseThreshold float64
}

func NewMockEntropyMonitor() *MockEntropyMonitor {
	return &MockEntropyMonitor{
		BaseThreshold: 4.8, // Maximum allowable entropy
	}
}

func (m *MockEntropyMonitor) CheckEntropy(ctx context.Context, data []byte, agentID string) (bool, error) {
	entropy := m.calculateShannonEntropy(data)

	// Check against threshold
	if entropy > m.BaseThreshold {
		return false, fmt.Errorf("high entropy detected: %.2f > %.2f", entropy, m.BaseThreshold)
	}

	return true, nil
}

func (m *MockEntropyMonitor) calculateShannonEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}

	// Count character frequencies
	charCounts := make(map[byte]int)
	for _, b := range data {
		charCounts[b]++
	}

	// Calculate Shannon Entropy
	var entropy float64
	totalLen := float64(len(data))

	for _, count := range charCounts {
		p := float64(count) / totalLen
		entropy -= p * math.Log2(p)
	}

	return entropy
}

// TransactionIDGenerator creates unique transaction IDs
type TransactionIDGenerator struct{}

func (g *TransactionIDGenerator) Generate(agentID string, payload []byte) string {
	// Create deterministic ID from agent + payload hash
	hasher := sha256.New()
	hasher.Write([]byte(agentID))
	hasher.Write(payload)
	hash := hex.EncodeToString(hasher.Sum(nil))
	return fmt.Sprintf("tx-%s", hash[:16])
}

func (m *MockJuryClient) Close() error {
	return nil
}

func (m *MockJuryClient) EvaluateTrace(ctx context.Context, traceID string, payload []byte) (bool, error) {
	return true, nil
}

// Assess performs a full trust assessment for Tri-Factor Gate (mock implementation)
func (m *MockJuryClient) Assess(ctx context.Context, transactionID, tenantID string) JuryResult {
	return JuryResult{
		Verdict:    "ALLOW",
		TrustLevel: 0.85,
		Reasoning:  "Mock assessment - trust score meets threshold",
	}
}

func (m *MockEntropyMonitor) Close() error {
	return nil
}

func (m *MockEntropyMonitor) MeasureEntropy(ctx context.Context, payload []byte) (float64, error) {
	return m.calculateShannonEntropy(payload), nil
}

// Analyze performs full signal validation for Tri-Factor Gate (mock implementation)
func (m *MockEntropyMonitor) Analyze(payload []byte, tenantID string) EntropyResult {
	entropy := m.calculateShannonEntropy(payload)

	verdict := "CLEAN"
	confidence := 0.9

	if entropy > 7.5 {
		verdict = "ENCRYPTED"
	} else if entropy > 6.0 {
		verdict = "SUSPICIOUS"
		confidence = 0.7
	}

	return EntropyResult{
		EntropyScore: entropy,
		Verdict:      verdict,
		Confidence:   confidence,
	}
}
