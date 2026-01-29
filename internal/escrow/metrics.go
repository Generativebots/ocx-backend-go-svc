package escrow

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the Economic Barrier
type Metrics struct {
	// Entropy metrics
	EntropyScore  *prometheus.HistogramVec
	EntropyChecks *prometheus.CounterVec

	// Transaction metrics
	TransactionTotal    *prometheus.CounterVec
	TransactionDuration *prometheus.HistogramVec

	// Tax metrics
	GovTaxLevied      *prometheus.CounterVec
	GovTaxDistributed *prometheus.GaugeVec

	// Reputation metrics
	AgentBalance    *prometheus.GaugeVec
	AgentTrustScore *prometheus.GaugeVec
	AgentFrozen     *prometheus.GaugeVec

	// Tri-Factor metrics
	TriFactorDuration *prometheus.HistogramVec
	TriFactorFailures *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// Entropy Score Histogram
		EntropyScore: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "escrow_entropy_score",
				Help:    "Shannon entropy score of agent handshake intervals",
				Buckets: []float64{0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0},
			},
			[]string{"agent_id"},
		),

		// Entropy Check Counter
		EntropyChecks: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "escrow_entropy_checks_total",
				Help: "Total number of entropy checks performed",
			},
			[]string{"agent_id", "result"}, // result: pass, fail_low, fail_high
		),

		// Transaction Total Counter
		TransactionTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "escrow_transaction_total",
				Help: "Total number of transactions processed by Economic Barrier",
			},
			[]string{"agent_id", "status"}, // status: released, shredded
		),

		// Transaction Duration Histogram
		TransactionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "escrow_transaction_duration_seconds",
				Help:    "Duration of Tri-Factor validation",
				Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
			},
			[]string{"agent_id"},
		),

		// Governance Tax Levied Counter
		GovTaxLevied: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "escrow_gov_tax_levied_total",
				Help: "Total governance tax levied on failed transactions",
			},
			[]string{"agent_id"},
		),

		// Governance Tax Distributed Gauge
		GovTaxDistributed: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "escrow_gov_tax_distributed",
				Help: "Amount of governance tax distributed in last cycle",
			},
			[]string{"agent_id"},
		),

		// Agent Balance Gauge
		AgentBalance: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "escrow_agent_balance",
				Help: "Current GovTaxBalance for each agent",
			},
			[]string{"agent_id"},
		),

		// Agent Trust Score Gauge
		AgentTrustScore: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "escrow_agent_trust_score",
				Help: "Current trust score for each agent",
			},
			[]string{"agent_id"},
		),

		// Agent Frozen Status Gauge
		AgentFrozen: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "escrow_agent_frozen",
				Help: "Whether agent is frozen (1) or active (0)",
			},
			[]string{"agent_id"},
		),

		// Tri-Factor Duration Histogram
		TriFactorDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "escrow_trifactor_duration_seconds",
				Help:    "Duration of individual Tri-Factor checks",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"factor"}, // factor: jury, entropy, reputation
		),

		// Tri-Factor Failures Counter
		TriFactorFailures: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "escrow_trifactor_failures_total",
				Help: "Total number of Tri-Factor validation failures",
			},
			[]string{"agent_id", "factor"}, // factor: jury, entropy, reputation, timeout
		),
	}
}

// RecordEntropyScore records an entropy measurement
func (m *Metrics) RecordEntropyScore(agentID string, score float64) {
	m.EntropyScore.WithLabelValues(agentID).Observe(score)
}

// RecordEntropyCheck records an entropy check result
func (m *Metrics) RecordEntropyCheck(agentID string, passed bool, score float64) {
	result := "pass"
	if !passed {
		if score < 1.2 {
			result = "fail_low"
		} else {
			result = "fail_high"
		}
	}
	m.EntropyChecks.WithLabelValues(agentID, result).Inc()
}

// RecordTransaction records a transaction outcome
func (m *Metrics) RecordTransaction(agentID string, released bool, duration float64) {
	status := "shredded"
	if released {
		status = "released"
	}
	m.TransactionTotal.WithLabelValues(agentID, status).Inc()
	m.TransactionDuration.WithLabelValues(agentID).Observe(duration)
}

// RecordTaxLevied records a penalty
func (m *Metrics) RecordTaxLevied(agentID string, amount float64) {
	m.GovTaxLevied.WithLabelValues(agentID).Add(amount)
}

// RecordTaxDistributed records a reward
func (m *Metrics) RecordTaxDistributed(agentID string, amount float64) {
	m.GovTaxDistributed.WithLabelValues(agentID).Set(amount)
}

// UpdateAgentMetrics updates agent-specific gauges
func (m *Metrics) UpdateAgentMetrics(agentID string, balance float64, trustScore float64, isFrozen bool) {
	m.AgentBalance.WithLabelValues(agentID).Set(balance)
	m.AgentTrustScore.WithLabelValues(agentID).Set(trustScore)

	frozenValue := 0.0
	if isFrozen {
		frozenValue = 1.0
	}
	m.AgentFrozen.WithLabelValues(agentID).Set(frozenValue)
}

// RecordTriFactorCheck records a Tri-Factor validation result
func (m *Metrics) RecordTriFactorCheck(factor string, duration float64, passed bool, agentID string) {
	m.TriFactorDuration.WithLabelValues(factor).Observe(duration)

	if !passed {
		m.TriFactorFailures.WithLabelValues(agentID, factor).Inc()
	}
}
