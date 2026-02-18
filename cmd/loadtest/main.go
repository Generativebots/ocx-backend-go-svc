package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/reputation"
)

// LoadTestConfig holds load test parameters
type LoadTestConfig struct {
	NumTransactions int
	Concurrency     int
	Duration        time.Duration
	ReportInterval  time.Duration
}

// LoadTestStats tracks test metrics
type LoadTestStats struct {
	TotalTransactions   uint64
	SuccessfulReleases  uint64
	FailedValidations   uint64
	TotalDuration       time.Duration
	AvgLatency          time.Duration
	MaxLatency          time.Duration
	MinLatency          time.Duration
	P95Latency          time.Duration
	P99Latency          time.Duration
	ThroughputPerSecond float64
}

func main() {
	// Parse flags
	numTxns := flag.Int("txns", 1000, "Number of transactions to simulate")
	concurrency := flag.Int("concurrency", 100, "Number of concurrent workers")
	duration := flag.Duration("duration", 0, "Test duration (0 = run until txns complete)")
	reportInterval := flag.Duration("report", 5*time.Second, "Stats reporting interval")
	flag.Parse()

	config := LoadTestConfig{
		NumTransactions: *numTxns,
		Concurrency:     *concurrency,
		Duration:        *duration,
		ReportInterval:  *reportInterval,
	}

	slog.Info("🚀 Starting Economic Barrier Load Test")
	slog.Info("Transactions", "num_transactions", config.NumTransactions)
	slog.Info("Concurrency", "concurrency", config.Concurrency)
	slog.Info("Duration", "duration", config.Duration)
	stats := runLoadTest(config)

	// Print final results
	printResults(stats)
}

func runLoadTest(config LoadTestConfig) *LoadTestStats {
	// Initialize components
	wallet := reputation.NewReputationWallet(nil) // Loadtest uses in-memory mode
	defer wallet.Close()

	gate := escrow.NewEscrowGate(
		escrow.NewMockJuryClient(),
		escrow.NewMockEntropyMonitor(),
	)

	// Stats tracking
	stats := &LoadTestStats{
		MinLatency: time.Hour, // Initialize to large value
	}
	var latencies []time.Duration
	var latenciesMu sync.Mutex

	// Worker pool
	txnChan := make(chan int, config.NumTransactions)
	var wg sync.WaitGroup

	// Start stats reporter
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reportStats(ctx, stats, config.ReportInterval)

	// Start workers
	startTime := time.Now()
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for txnID := range txnChan {
				processTransaction(ctx, gate, workerID, txnID, stats, &latencies, &latenciesMu)
			}
		}(i)
	}

	// Feed transactions
	for i := 0; i < config.NumTransactions; i++ {
		txnChan <- i
	}
	close(txnChan)

	// Wait for completion
	wg.Wait()
	totalDuration := time.Since(startTime)

	// Calculate final stats
	stats.TotalDuration = totalDuration
	stats.ThroughputPerSecond = float64(stats.TotalTransactions) / totalDuration.Seconds()

	// Calculate latency percentiles
	latenciesMu.Lock()
	if len(latencies) > 0 {
		stats.AvgLatency = calculateAverage(latencies)
		stats.P95Latency = calculatePercentile(latencies, 95)
		stats.P99Latency = calculatePercentile(latencies, 99)
	}
	latenciesMu.Unlock()

	return stats
}

func processTransaction(
	ctx context.Context,
	gate *escrow.EscrowGate,
	workerID, txnID int,
	stats *LoadTestStats,
	latencies *[]time.Duration,
	latenciesMu *sync.Mutex,
) {
	agentID := fmt.Sprintf("agent-%d", workerID%10) // 10 agents
	txID := fmt.Sprintf("tx-%d-%d", workerID, txnID)
	payload := []byte(fmt.Sprintf("Test transaction %d from worker %d", txnID, workerID))

	// Sequester
	gate.Sequester(txID, agentID, payload)

	// Measure validation latency
	start := time.Now()
	_, err := gate.AwaitRelease(ctx, txID)
	latency := time.Since(start)

	// Update stats
	atomic.AddUint64(&stats.TotalTransactions, 1)

	if err != nil {
		atomic.AddUint64(&stats.FailedValidations, 1)
	} else {
		atomic.AddUint64(&stats.SuccessfulReleases, 1)
	}

	// Track latency
	latenciesMu.Lock()
	*latencies = append(*latencies, latency)
	if latency > stats.MaxLatency {
		stats.MaxLatency = latency
	}
	if latency < stats.MinLatency {
		stats.MinLatency = latency
	}
	latenciesMu.Unlock()
}

func reportStats(ctx context.Context, stats *LoadTestStats, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			total := atomic.LoadUint64(&stats.TotalTransactions)
			success := atomic.LoadUint64(&stats.SuccessfulReleases)
			failed := atomic.LoadUint64(&stats.FailedValidations)

			slog.Warn("Progress: txns | success | failed | Latency: min= max", "total", total, "success", success, "failed", failed, "min_latency", stats.MinLatency, "max_latency", stats.MaxLatency)
		case <-ctx.Done():
			return
		}
	}
}

func printResults(stats *LoadTestStats) {
	separator := "================================================================================"
	divider := "--------------------------------------------------------------------------------"

	fmt.Println("\n" + separator)
	fmt.Println("📊 LOAD TEST RESULTS")
	fmt.Println(separator)
	fmt.Printf("Total Transactions:     %d\n", stats.TotalTransactions)
	fmt.Printf("Successful Releases:    %d (%.2f%%)\n",
		stats.SuccessfulReleases,
		float64(stats.SuccessfulReleases)/float64(stats.TotalTransactions)*100)
	fmt.Printf("Failed Validations:     %d (%.2f%%)\n",
		stats.FailedValidations,
		float64(stats.FailedValidations)/float64(stats.TotalTransactions)*100)
	fmt.Println(divider)
	fmt.Printf("Total Duration:         %v\n", stats.TotalDuration)
	fmt.Printf("Throughput:             %.2f txns/sec\n", stats.ThroughputPerSecond)
	fmt.Println(divider)
	fmt.Printf("Latency (min):          %v\n", stats.MinLatency)
	fmt.Printf("Latency (avg):          %v\n", stats.AvgLatency)
	fmt.Printf("Latency (p95):          %v\n", stats.P95Latency)
	fmt.Printf("Latency (p99):          %v\n", stats.P99Latency)
	fmt.Printf("Latency (max):          %v\n", stats.MaxLatency)
	fmt.Println(separator)

	// Performance assessment
	if stats.ThroughputPerSecond >= 100 {
		fmt.Println("✅ PASS: Throughput meets target (>100 txns/sec)")
	} else {
		fmt.Println("❌ FAIL: Throughput below target (<100 txns/sec)")
	}

	if stats.P95Latency < 100*time.Millisecond {
		fmt.Println("✅ PASS: P95 latency meets target (<100ms)")
	} else {
		fmt.Println("⚠️  WARN: P95 latency above target (>100ms)")
	}

	successRate := float64(stats.SuccessfulReleases) / float64(stats.TotalTransactions) * 100
	if successRate >= 95 {
		fmt.Println("✅ PASS: Success rate meets target (>95%)")
	} else {
		fmt.Println("❌ FAIL: Success rate below target (<95%)")
	}
	fmt.Println(separator + "\n")
}

func calculateAverage(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	var total time.Duration
	for _, l := range latencies {
		total += l
	}

	return total / time.Duration(len(latencies))
}

func calculatePercentile(latencies []time.Duration, percentile int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Sort latencies
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Simple bubble sort (good enough for testing)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate percentile index
	idx := int(float64(len(sorted)) * float64(percentile) / 100.0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}
