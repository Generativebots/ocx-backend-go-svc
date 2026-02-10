// Package escrow — Socket Pipeline Meter (Gap 5 Fix: §4.1 Real-Time Metering)
//
// Per-packet governance cost metering in the WebSocket/eBPF pipeline.
// Applies dynamic cost multiplier based on tool risk level, computes
// real-time governance tax, and emits per-frame billing events.
package escrow

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// SocketMeter provides real-time per-packet governance cost metering.
// It sits in the socket pipeline and meters every frame passing through
// the OCX gateway, applying the §4.1 dynamic cost multiplier.
type SocketMeter struct {
	mu sync.RWMutex

	// Per-tenant metering state
	tenantMeters map[string]*TenantMeter

	// Global counters (atomic for lock-free hot path)
	totalFrames  atomic.Uint64
	totalCredits atomic.Uint64
	totalTaxed   atomic.Uint64

	// Cost tables
	riskMultipliers  map[string]float64 // tool_class → multiplier
	baseCostPerFrame float64

	// Callbacks
	onBillingEvent func(event *MeterBillingEvent)

	// L4 FIX: Stop channel for graceful shutdown of background goroutine
	stopEvict chan struct{}
}

// TenantMeter tracks per-tenant metering state.
type TenantMeter struct {
	TenantID       string    `json:"tenant_id"`
	FrameCount     uint64    `json:"frame_count"`
	TotalCost      float64   `json:"total_cost"`
	TotalTax       float64   `json:"total_tax"`
	BurnRatePerSec float64   `json:"burn_rate_per_sec"`
	LastFrameAt    time.Time `json:"last_frame_at"`
	WindowStart    time.Time `json:"window_start"`
	WindowFrames   uint64    `json:"window_frames"`
}

// MeterBillingEvent is emitted for every metered frame.
type MeterBillingEvent struct {
	TransactionID  string    `json:"transaction_id"`
	TenantID       string    `json:"tenant_id"`
	AgentID        string    `json:"agent_id"`
	ToolClass      string    `json:"tool_class"`
	BaseCost       float64   `json:"base_cost"`
	RiskMultiplier float64   `json:"risk_multiplier"`
	GovernanceTax  float64   `json:"governance_tax"`
	TotalCost      float64   `json:"total_cost"`
	TrustScore     float64   `json:"trust_score"`
	Timestamp      time.Time `json:"timestamp"`
}

// FrameContext contains metadata about a frame being metered.
type FrameContext struct {
	TransactionID string
	TenantID      string
	AgentID       string
	ToolClass     string  // e.g. "file_write", "network_call", "data_query"
	TrustScore    float64 // Agent's current trust score [0, 1]
	PayloadBytes  int
}

// MeterSnapshot is a point-in-time view of meter state.
type MeterSnapshot struct {
	TotalFrames     uint64        `json:"total_frames"`
	TotalCredits    uint64        `json:"total_credits"`
	TotalTaxed      uint64        `json:"total_taxed"`
	ActiveTenants   int           `json:"active_tenants"`
	TenantSnapshots []TenantMeter `json:"tenant_snapshots"`
}

// NewSocketMeter creates a new socket pipeline meter.
func NewSocketMeter() *SocketMeter {
	sm := &SocketMeter{
		tenantMeters:     make(map[string]*TenantMeter),
		baseCostPerFrame: 0.001, // 0.001 credits per frame baseline
		stopEvict:        make(chan struct{}),
		riskMultipliers: map[string]float64{
			// §4.1: Dynamic cost multiplier based on tool risk
			"data_query":    1.0, // Low risk — baseline
			"read_only":     0.5, // Very low risk
			"file_read":     1.0, // Low risk
			"file_write":    3.0, // Medium risk — 3x
			"network_call":  2.0, // Medium risk — 2x
			"api_call":      2.5, // Medium-high risk
			"data_mutation": 4.0, // High risk — 4x
			"admin_action":  5.0, // Very high — 5x governance tax
			"exec_command":  5.0, // Very high — 5x
			"payment":       4.0, // High risk — 4x
			"pii_access":    3.5, // Elevated — 3.5x
			"unknown":       2.0, // Default to medium
		},
	}

	// P1 FIX #8: Start background reaper to evict stale tenant meters.
	// Without this, tenantMeters map grows unbounded and leaks memory.
	go sm.evictStaleTenants()

	return sm
}

// MeterFrame applies real-time governance metering to a single frame.
// This is the hot path — called for every packet in the socket pipeline.
func (sm *SocketMeter) MeterFrame(ctx *FrameContext) *MeterBillingEvent {
	// 1. Look up risk multiplier for this tool class
	multiplier := sm.getRiskMultiplier(ctx.ToolClass)

	// 2. Apply trust-score discount (higher trust → lower cost)
	// §4.1: "Dynamic cost multiplier based on tool risk"
	trustDiscount := 1.0
	if ctx.TrustScore > 0.8 {
		trustDiscount = 0.7 // 30% discount for highly trusted agents
	} else if ctx.TrustScore > 0.6 {
		trustDiscount = 0.85 // 15% discount for trusted
	} else if ctx.TrustScore < 0.3 {
		trustDiscount = 1.5 // 50% surcharge for untrusted
	}

	// 3. Compute costs
	baseCost := sm.baseCostPerFrame * float64(max(ctx.PayloadBytes, 1)) / 1024.0
	governanceTax := baseCost * multiplier * trustDiscount
	totalCost := baseCost + governanceTax

	// 4. Update atomic counters (lock-free)
	sm.totalFrames.Add(1)
	sm.totalCredits.Add(uint64(totalCost * 1000))
	sm.totalTaxed.Add(uint64(governanceTax * 1000))

	// 5. Update per-tenant meter
	sm.updateTenantMeter(ctx.TenantID, totalCost, governanceTax)

	// 6. Create billing event
	event := &MeterBillingEvent{
		TransactionID:  ctx.TransactionID,
		TenantID:       ctx.TenantID,
		AgentID:        ctx.AgentID,
		ToolClass:      ctx.ToolClass,
		BaseCost:       baseCost,
		RiskMultiplier: multiplier,
		GovernanceTax:  governanceTax,
		TotalCost:      totalCost,
		TrustScore:     ctx.TrustScore,
		Timestamp:      time.Now(),
	}

	// 7. Fire billing callback if registered
	if sm.onBillingEvent != nil {
		sm.onBillingEvent(event)
	}

	return event
}

// SetBillingCallback registers a callback for every metered frame.
func (sm *SocketMeter) SetBillingCallback(cb func(*MeterBillingEvent)) {
	sm.onBillingEvent = cb
}

// GetSnapshot returns a point-in-time view of metering state.
func (sm *SocketMeter) GetSnapshot() *MeterSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snap := &MeterSnapshot{
		TotalFrames:     sm.totalFrames.Load(),
		TotalCredits:    sm.totalCredits.Load(),
		TotalTaxed:      sm.totalTaxed.Load(),
		ActiveTenants:   len(sm.tenantMeters),
		TenantSnapshots: make([]TenantMeter, 0, len(sm.tenantMeters)),
	}

	for _, tm := range sm.tenantMeters {
		snap.TenantSnapshots = append(snap.TenantSnapshots, *tm)
	}

	return snap
}

// GetTenantBurnRate returns the current credit burn rate for a tenant.
func (sm *SocketMeter) GetTenantBurnRate(tenantID string) float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if tm, ok := sm.tenantMeters[tenantID]; ok {
		return tm.BurnRatePerSec
	}
	return 0
}

// --- Internal helpers ---

func (sm *SocketMeter) getRiskMultiplier(toolClass string) float64 {
	if m, ok := sm.riskMultipliers[toolClass]; ok {
		return m
	}
	return sm.riskMultipliers["unknown"]
}

func (sm *SocketMeter) updateTenantMeter(tenantID string, cost, tax float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	tm, exists := sm.tenantMeters[tenantID]
	if !exists {
		tm = &TenantMeter{
			TenantID:    tenantID,
			WindowStart: time.Now(),
		}
		sm.tenantMeters[tenantID] = tm
	}

	tm.FrameCount++
	tm.TotalCost += cost
	tm.TotalTax += tax
	tm.LastFrameAt = time.Now()
	tm.WindowFrames++

	// Recalculate burn rate every 100 frames
	if tm.WindowFrames%100 == 0 {
		elapsed := time.Since(tm.WindowStart).Seconds()
		if elapsed > 0 {
			tm.BurnRatePerSec = tm.TotalCost / elapsed
		}
	}
}

// LogMeterStats logs current meter statistics (called from stats reporter).
func (sm *SocketMeter) LogMeterStats() {
	snap := sm.GetSnapshot()
	slog.Info("Socket Meter: frames= credits= tax= tenants", "total_frames", snap.TotalFrames, "total_credits", snap.TotalCredits, "total_taxed", snap.TotalTaxed, "active_tenants", snap.ActiveTenants)
}

// evictStaleTenants periodically removes tenant meters that have been idle
// for more than 1 hour, preventing unbounded memory growth.
// P1 FIX #8: Without this, the tenantMeters map grows forever.
func (sm *SocketMeter) evictStaleTenants() {
	const evictAfter = 1 * time.Hour
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.mu.Lock()
			now := time.Now()
			var evicted int
			for id, tm := range sm.tenantMeters {
				if now.Sub(tm.LastFrameAt) > evictAfter {
					delete(sm.tenantMeters, id)
					evicted++
				}
			}
			sm.mu.Unlock()

			if evicted > 0 {
				slog.Info("Socket Meter: evicted stale tenant meters", "evicted", evicted)
			}
		case <-sm.stopEvict:
			slog.Info("🛑 Socket Meter: eviction goroutine stopped")
			return
		}
	}
}

// Stop signals the background eviction goroutine to exit.
func (sm *SocketMeter) Stop() {
	close(sm.stopEvict)
}
