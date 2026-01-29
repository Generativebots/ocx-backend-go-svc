package monitoring

import (
	"context"
	"sync"
	"time"
)

// ============================================================================
// REAL-TIME MONITORING & ANALYTICS
// ============================================================================

// MonitoringSystem tracks live execution metrics and analytics
type MonitoringSystem struct {
	mu sync.RWMutex

	// Live metrics
	metrics *LiveMetrics

	// Performance tracking
	latencyHistogram  map[string]*LatencyBucket
	throughputCounter map[string]*ThroughputCounter

	// Error tracking
	errors map[string]*ErrorRecord

	// Historical data
	historicalMetrics []*MetricsSnapshot

	// Entropy monitoring
	entropyScores map[string]*EntropyScore

	// Alerts
	alerts     []*Alert
	alertRules []*AlertRule
}

// LiveMetrics contains real-time metrics
type LiveMetrics struct {
	// Handshake metrics
	TotalHandshakes      int64
	SuccessfulHandshakes int64
	FailedHandshakes     int64
	AverageHandshakeTime float64 // milliseconds
	HandshakesPerSecond  float64

	// Contract execution metrics
	TotalExecutions      int64
	SuccessfulExecutions int64
	FailedExecutions     int64
	AverageExecutionTime float64
	ExecutionsPerSecond  float64

	// Trust metrics
	AverageTrustLevel      float64
	AverageTrustTax        float64
	TotalTrustTaxCollected float64

	// System metrics
	ActiveSessions     int64
	ActiveAgents       int64
	TotalAPICallsToday int64
	ErrorRate          float64

	// Entropy metrics
	AverageEntropyScore float64
	HighEntropyAlerts   int64

	LastUpdated time.Time
}

// LatencyBucket tracks latency distribution
type LatencyBucket struct {
	Operation string
	P50       float64 // 50th percentile
	P95       float64 // 95th percentile
	P99       float64 // 99th percentile
	Min       float64
	Max       float64
	Count     int64
	Sum       float64
}

// ThroughputCounter tracks throughput
type ThroughputCounter struct {
	Operation      string
	Count          int64
	LastMinute     int64
	LastHour       int64
	LastDay        int64
	RequestsPerSec float64
}

// ErrorRecord tracks an error occurrence
type ErrorRecord struct {
	ErrorID    string
	ErrorType  string
	Message    string
	Operation  string
	Timestamp  time.Time
	Count      int64
	LastSeen   time.Time
	Severity   string // "low", "medium", "high", "critical"
	Resolved   bool
	StackTrace string
}

// MetricsSnapshot captures metrics at a point in time
type MetricsSnapshot struct {
	Timestamp time.Time
	Metrics   *LiveMetrics
}

// EntropyScore tracks entropy for an agent/contract
type EntropyScore struct {
	AgentID    string
	ContractID string
	Score      float64 // 0.0 - 1.0
	Threshold  float64
	Exceeded   bool
	Timestamp  time.Time
	Details    map[string]float64
}

// Alert represents a triggered alert
type Alert struct {
	AlertID     string
	RuleID      string
	Severity    string
	Title       string
	Message     string
	TriggeredAt time.Time
	Resolved    bool
	ResolvedAt  *time.Time
	Metadata    map[string]interface{}
}

// AlertRule defines conditions for alerts
type AlertRule struct {
	RuleID        string
	Name          string
	Condition     string // "error_rate > 0.05", "latency_p99 > 1000", etc.
	Severity      string
	Enabled       bool
	Cooldown      time.Duration
	LastTriggered *time.Time
}

// NewMonitoringSystem creates a new monitoring system
func NewMonitoringSystem() *MonitoringSystem {
	return &MonitoringSystem{
		metrics: &LiveMetrics{
			LastUpdated: time.Now(),
		},
		latencyHistogram:  make(map[string]*LatencyBucket),
		throughputCounter: make(map[string]*ThroughputCounter),
		errors:            make(map[string]*ErrorRecord),
		historicalMetrics: make([]*MetricsSnapshot, 0),
		entropyScores:     make(map[string]*EntropyScore),
		alerts:            make([]*Alert, 0),
		alertRules:        make([]*AlertRule, 0),
	}
}

// ============================================================================
// METRICS RECORDING
// ============================================================================

// RecordHandshake records a handshake execution
func (ms *MonitoringSystem) RecordHandshake(ctx context.Context, success bool, duration time.Duration) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.metrics.TotalHandshakes++
	if success {
		ms.metrics.SuccessfulHandshakes++
	} else {
		ms.metrics.FailedHandshakes++
	}

	// Update average handshake time
	ms.updateAverageHandshakeTime(duration.Milliseconds())

	// Record latency
	ms.recordLatencyUnsafe("handshake", float64(duration.Milliseconds()))

	// Record throughput
	ms.recordThroughputUnsafe("handshake")

	ms.metrics.LastUpdated = time.Now()
}

// RecordContractExecution records a contract execution
func (ms *MonitoringSystem) RecordContractExecution(ctx context.Context, success bool, duration time.Duration) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.metrics.TotalExecutions++
	if success {
		ms.metrics.SuccessfulExecutions++
	} else {
		ms.metrics.FailedExecutions++
	}

	// Update average execution time
	ms.updateAverageExecutionTime(duration.Milliseconds())

	// Record latency
	ms.recordLatencyUnsafe("contract_execution", float64(duration.Milliseconds()))

	// Record throughput
	ms.recordThroughputUnsafe("contract_execution")

	ms.metrics.LastUpdated = time.Now()
}

// RecordTrustMetrics records trust-related metrics
func (ms *MonitoringSystem) RecordTrustMetrics(trustLevel, trustTax float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Update average trust level (exponential moving average)
	alpha := 0.1
	ms.metrics.AverageTrustLevel = alpha*trustLevel + (1-alpha)*ms.metrics.AverageTrustLevel

	// Update average trust tax
	ms.metrics.AverageTrustTax = alpha*trustTax + (1-alpha)*ms.metrics.AverageTrustTax

	// Update total trust tax collected
	ms.metrics.TotalTrustTaxCollected += trustTax

	ms.metrics.LastUpdated = time.Now()
}

// RecordError records an error occurrence
func (ms *MonitoringSystem) RecordError(ctx context.Context, errorType, message, operation, stackTrace string, severity string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	errorKey := errorType + ":" + message

	if existing, ok := ms.errors[errorKey]; ok {
		existing.Count++
		existing.LastSeen = time.Now()
	} else {
		ms.errors[errorKey] = &ErrorRecord{
			ErrorID:    generateErrorID(),
			ErrorType:  errorType,
			Message:    message,
			Operation:  operation,
			Timestamp:  time.Now(),
			Count:      1,
			LastSeen:   time.Now(),
			Severity:   severity,
			StackTrace: stackTrace,
		}
	}

	// Update error rate
	ms.updateErrorRate()

	// Check alert rules
	ms.checkAlertRulesUnsafe()

	ms.metrics.LastUpdated = time.Now()
}

// RecordEntropyScore records an entropy score
func (ms *MonitoringSystem) RecordEntropyScore(ctx context.Context, agentID, contractID string, score float64, threshold float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := agentID + ":" + contractID

	exceeded := score > threshold

	ms.entropyScores[key] = &EntropyScore{
		AgentID:    agentID,
		ContractID: contractID,
		Score:      score,
		Threshold:  threshold,
		Exceeded:   exceeded,
		Timestamp:  time.Now(),
	}

	// Update average entropy score
	totalScore := 0.0
	count := 0
	for _, es := range ms.entropyScores {
		totalScore += es.Score
		count++
	}
	if count > 0 {
		ms.metrics.AverageEntropyScore = totalScore / float64(count)
	}

	// Count high entropy alerts
	if exceeded {
		ms.metrics.HighEntropyAlerts++
	}

	ms.metrics.LastUpdated = time.Now()
}

// ============================================================================
// METRICS RETRIEVAL
// ============================================================================

// GetLiveMetrics returns current live metrics
func (ms *MonitoringSystem) GetLiveMetrics() *LiveMetrics {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	// Return a copy
	metrics := *ms.metrics
	return &metrics
}

// GetLatencyMetrics returns latency metrics for an operation
func (ms *MonitoringSystem) GetLatencyMetrics(operation string) *LatencyBucket {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	bucket, ok := ms.latencyHistogram[operation]
	if !ok {
		return nil
	}

	// Return a copy
	bucketCopy := *bucket
	return &bucketCopy
}

// GetThroughputMetrics returns throughput metrics for an operation
func (ms *MonitoringSystem) GetThroughputMetrics(operation string) *ThroughputCounter {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	counter, ok := ms.throughputCounter[operation]
	if !ok {
		return nil
	}

	// Return a copy
	counterCopy := *counter
	return &counterCopy
}

// GetRecentErrors returns recent errors
func (ms *MonitoringSystem) GetRecentErrors(limit int) []*ErrorRecord {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	errors := make([]*ErrorRecord, 0, len(ms.errors))
	for _, err := range ms.errors {
		if !err.Resolved {
			errors = append(errors, err)
		}
	}

	// Sort by last seen (most recent first)
	// Simple bubble sort for small datasets
	for i := 0; i < len(errors)-1; i++ {
		for j := 0; j < len(errors)-i-1; j++ {
			if errors[j].LastSeen.Before(errors[j+1].LastSeen) {
				errors[j], errors[j+1] = errors[j+1], errors[j]
			}
		}
	}

	if limit > 0 && limit < len(errors) {
		errors = errors[:limit]
	}

	return errors
}

// GetActiveAlerts returns active alerts
func (ms *MonitoringSystem) GetActiveAlerts() []*Alert {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	active := make([]*Alert, 0)
	for _, alert := range ms.alerts {
		if !alert.Resolved {
			active = append(active, alert)
		}
	}

	return active
}

// GetHistoricalMetrics returns historical metrics for a time range
func (ms *MonitoringSystem) GetHistoricalMetrics(start, end time.Time) []*MetricsSnapshot {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	snapshots := make([]*MetricsSnapshot, 0)
	for _, snapshot := range ms.historicalMetrics {
		if snapshot.Timestamp.After(start) && snapshot.Timestamp.Before(end) {
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots
}

// ============================================================================
// ALERT MANAGEMENT
// ============================================================================

// AddAlertRule adds a new alert rule
func (ms *MonitoringSystem) AddAlertRule(rule *AlertRule) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.alertRules = append(ms.alertRules, rule)
}

// checkAlertRulesUnsafe checks all alert rules (must be called with lock)
func (ms *MonitoringSystem) checkAlertRulesUnsafe() {
	for _, rule := range ms.alertRules {
		if !rule.Enabled {
			continue
		}

		// Check cooldown
		if rule.LastTriggered != nil && time.Since(*rule.LastTriggered) < rule.Cooldown {
			continue
		}

		// Evaluate condition
		if ms.evaluateCondition(rule.Condition) {
			ms.triggerAlertUnsafe(rule)
		}
	}
}

// evaluateCondition evaluates an alert condition
func (ms *MonitoringSystem) evaluateCondition(condition string) bool {
	// Simple condition evaluation
	// In production, use a proper expression evaluator

	// Example conditions:
	// "error_rate > 0.05"
	// "latency_p99 > 1000"
	// "entropy_score > 0.8"

	// For now, check error rate
	if condition == "error_rate > 0.05" {
		return ms.metrics.ErrorRate > 0.05
	}

	return false
}

// triggerAlertUnsafe triggers an alert (must be called with lock)
func (ms *MonitoringSystem) triggerAlertUnsafe(rule *AlertRule) {
	alert := &Alert{
		AlertID:     generateAlertID(),
		RuleID:      rule.RuleID,
		Severity:    rule.Severity,
		Title:       rule.Name,
		Message:     "Alert condition met: " + rule.Condition,
		TriggeredAt: time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	ms.alerts = append(ms.alerts, alert)

	now := time.Now()
	rule.LastTriggered = &now
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func (ms *MonitoringSystem) updateAverageHandshakeTime(durationMs int64) {
	// Exponential moving average
	alpha := 0.1
	ms.metrics.AverageHandshakeTime = alpha*float64(durationMs) + (1-alpha)*ms.metrics.AverageHandshakeTime
}

func (ms *MonitoringSystem) updateAverageExecutionTime(durationMs int64) {
	alpha := 0.1
	ms.metrics.AverageExecutionTime = alpha*float64(durationMs) + (1-alpha)*ms.metrics.AverageExecutionTime
}

func (ms *MonitoringSystem) updateErrorRate() {
	totalOps := ms.metrics.TotalHandshakes + ms.metrics.TotalExecutions
	totalErrors := ms.metrics.FailedHandshakes + ms.metrics.FailedExecutions

	if totalOps > 0 {
		ms.metrics.ErrorRate = float64(totalErrors) / float64(totalOps)
	}
}

func (ms *MonitoringSystem) recordLatencyUnsafe(operation string, latencyMs float64) {
	bucket, ok := ms.latencyHistogram[operation]
	if !ok {
		bucket = &LatencyBucket{
			Operation: operation,
			Min:       latencyMs,
			Max:       latencyMs,
		}
		ms.latencyHistogram[operation] = bucket
	}

	bucket.Count++
	bucket.Sum += latencyMs

	if latencyMs < bucket.Min {
		bucket.Min = latencyMs
	}
	if latencyMs > bucket.Max {
		bucket.Max = latencyMs
	}

	// Simple percentile calculation (would use proper histogram in production)
	bucket.P50 = bucket.Sum / float64(bucket.Count)
	bucket.P95 = bucket.Max * 0.95
	bucket.P99 = bucket.Max * 0.99
}

func (ms *MonitoringSystem) recordThroughputUnsafe(operation string) {
	counter, ok := ms.throughputCounter[operation]
	if !ok {
		counter = &ThroughputCounter{
			Operation: operation,
		}
		ms.throughputCounter[operation] = counter
	}

	counter.Count++
	counter.LastMinute++
	counter.LastHour++
	counter.LastDay++

	// Calculate requests per second (simplified)
	counter.RequestsPerSec = float64(counter.LastMinute) / 60.0
}

func generateErrorID() string {
	return "err_" + time.Now().Format("20060102150405")
}

func generateAlertID() string {
	return "alert_" + time.Now().Format("20060102150405")
}

// SnapshotMetrics creates a snapshot of current metrics for historical tracking
func (ms *MonitoringSystem) SnapshotMetrics() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		Metrics:   ms.metrics,
	}

	ms.historicalMetrics = append(ms.historicalMetrics, snapshot)

	// Keep only last 24 hours of snapshots
	cutoff := time.Now().Add(-24 * time.Hour)
	filtered := make([]*MetricsSnapshot, 0)
	for _, s := range ms.historicalMetrics {
		if s.Timestamp.After(cutoff) {
			filtered = append(filtered, s)
		}
	}
	ms.historicalMetrics = filtered
}
