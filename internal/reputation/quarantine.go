package reputation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// QuarantineManager handles automated quarantine and recovery
type QuarantineManager struct {
	wallet ReputationStore
	logger *log.Logger
	config QuarantineConfig
}

// QuarantineConfig holds quarantine parameters
type QuarantineConfig struct {
	MinRecoveryStake    int64         // Minimum stake to unfreeze (e.g., 5000)
	CooldownPeriod      time.Duration // Probationary period (e.g., 24 hours)
	ProbationThreshold  float64       // Stricter entropy threshold during probation (e.g., 1.5)
	MaxRecoveryAttempts int           // Max recovery attempts before permanent ban
}

// GrafanaAlert represents the webhook payload from Grafana
type GrafanaAlert struct {
	Receiver          string             `json:"receiver"`
	Status            string             `json:"status"`
	Alerts            []GrafanaAlertItem `json:"alerts"`
	GroupLabels       map[string]string  `json:"groupLabels"`
	CommonLabels      map[string]string  `json:"commonLabels"`
	CommonAnnotations map[string]string  `json:"commonAnnotations"`
	ExternalURL       string             `json:"externalURL"`
}

// GrafanaAlertItem represents a single alert
type GrafanaAlertItem struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// NewQuarantineManager creates a new quarantine manager
func NewQuarantineManager(wallet ReputationStore, config QuarantineConfig) *QuarantineManager {
	if config.MinRecoveryStake == 0 {
		config.MinRecoveryStake = 5000
	}
	if config.CooldownPeriod == 0 {
		config.CooldownPeriod = 24 * time.Hour
	}
	if config.ProbationThreshold == 0 {
		config.ProbationThreshold = 1.5
	}
	if config.MaxRecoveryAttempts == 0 {
		config.MaxRecoveryAttempts = 3
	}

	return &QuarantineManager{
		wallet: wallet,
		logger: log.New(log.Writer(), "[QuarantineManager] ", log.LstdFlags),
		config: config,
	}
}

// QuarantineWebhookHandler handles Grafana alert webhooks
func (qm *QuarantineManager) QuarantineWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var alert GrafanaAlert
	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		qm.logger.Printf("Failed to decode webhook: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	qm.logger.Printf("Received Grafana alert: %s (status: %s)", alert.Receiver, alert.Status)

	// Process each alert
	for _, item := range alert.Alerts {
		if item.Status == "firing" {
			agentID := item.Labels["agent_id"]
			if agentID == "" {
				qm.logger.Printf("Alert missing agent_id label")
				continue
			}

			// Quarantine the agent
			if err := qm.QuarantineAgent(context.Background(), agentID, item.Annotations["description"]); err != nil {
				qm.logger.Printf("Failed to quarantine agent %s: %v", agentID, err)
				continue
			}

			qm.logger.Printf("🔒 Auto-quarantined agent %s due to alert: %s", agentID, item.Annotations["summary"])
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

// QuarantineAgent freezes an agent
func (qm *QuarantineManager) QuarantineAgent(ctx context.Context, agentID, reason string) error {
	// Use the wallet's QuarantineAgent method
	if err := qm.wallet.QuarantineAgent(ctx, agentID); err != nil {
		return fmt.Errorf("failed to quarantine: %w", err)
	}

	qm.logger.Printf("🔒 Quarantined agent %s: %s", agentID, reason)
	return nil
}

// ProcessRecovery handles agent recovery with staking
func (qm *QuarantineManager) ProcessRecovery(ctx context.Context, agentID string, stakeAmount int64) error {
	if stakeAmount < qm.config.MinRecoveryStake {
		return fmt.Errorf("insufficient stake: %d < %d", stakeAmount, qm.config.MinRecoveryStake)
	}

	// Check recovery attempts
	attempts := qm.getRecoveryAttempts(agentID)
	if attempts >= qm.config.MaxRecoveryAttempts {
		return fmt.Errorf("max recovery attempts exceeded (%d/%d)", attempts, qm.config.MaxRecoveryAttempts)
	}

	// Use the wallet's ProcessRecovery method
	if err := qm.wallet.ProcessRecovery(ctx, agentID, stakeAmount); err != nil {
		return fmt.Errorf("recovery failed: %w", err)
	}

	// Enter probationary period
	qm.enterProbation(agentID)

	qm.logger.Printf("🔓 Agent %s recovered with stake %d (attempt %d/%d, probation: %v)",
		agentID, stakeAmount, attempts+1, qm.config.MaxRecoveryAttempts, qm.config.CooldownPeriod)

	return nil
}

// getRecoveryAttempts returns the number of recovery attempts
func (qm *QuarantineManager) getRecoveryAttempts(_ string) int {
	// In production, query ReputationAudit:
	// SELECT COUNT(*) FROM reputation_audit WHERE agent_id = ? AND verdict = 'RECOVERED'

	// Mock data
	return 0
}

// enterProbation sets probationary status
func (qm *QuarantineManager) enterProbation(agentID string) {
	// In production, store probation end time in database
	// For now, log it
	endTime := time.Now().Add(qm.config.CooldownPeriod)
	qm.logger.Printf("⏳ Agent %s entering probation until %s (stricter threshold: %.2f)",
		agentID, endTime.Format(time.RFC3339), qm.config.ProbationThreshold)
}

// IsProbationary checks if agent is in probationary period
func (qm *QuarantineManager) IsProbationary(ctx context.Context, agentID string) (bool, time.Time) {
	// In production, query database for probation end time
	// For now, return false
	return false, time.Time{}
}

// GetProbationThreshold returns the entropy threshold for an agent
func (qm *QuarantineManager) GetProbationThreshold(ctx context.Context, agentID string) float64 {
	isProbation, _ := qm.IsProbationary(ctx, agentID)
	if isProbation {
		return qm.config.ProbationThreshold
	}
	return 1.2 // Normal threshold
}

// RecoveryRequest represents a recovery API request
type RecoveryRequest struct {
	AgentID     string `json:"agent_id"`
	StakeAmount int64  `json:"stake_amount"`
}

// RecoveryResponse represents a recovery API response
type RecoveryResponse struct {
	Success         bool      `json:"success"`
	Message         string    `json:"message"`
	ProbationEnd    time.Time `json:"probation_end,omitempty"`
	RecoveryAttempt int       `json:"recovery_attempt"`
	MaxAttempts     int       `json:"max_attempts"`
}

// RecoveryAPIHandler handles recovery API requests
func (qm *QuarantineManager) RecoveryAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RecoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	attempts := qm.getRecoveryAttempts(req.AgentID)

	err := qm.ProcessRecovery(ctx, req.AgentID, req.StakeAmount)
	if err != nil {
		resp := RecoveryResponse{
			Success:         false,
			Message:         err.Error(),
			RecoveryAttempt: attempts,
			MaxAttempts:     qm.config.MaxRecoveryAttempts,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
		return
	}

	probationEnd := time.Now().Add(qm.config.CooldownPeriod)
	resp := RecoveryResponse{
		Success:         true,
		Message:         "Recovery successful",
		ProbationEnd:    probationEnd,
		RecoveryAttempt: attempts + 1,
		MaxAttempts:     qm.config.MaxRecoveryAttempts,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
