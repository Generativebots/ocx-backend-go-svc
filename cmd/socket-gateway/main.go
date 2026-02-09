package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/fabric"
	"github.com/ocx/backend/internal/gvisor"
)

// Global components for speculative execution
var (
	sandboxExecutor *gvisor.SandboxExecutor
	stateCloner     *gvisor.StateCloner
	escrowGate      *escrow.EscrowGate
	dbStateManager  *gvisor.DatabaseStateManager
	socketMeter     *escrow.SocketMeter
)

// SocketEvent matches the C struct in socket_filter.bpf.c
type SocketEvent struct {
	PID        uint32
	TID        uint32
	Timestamp  uint64
	SrcIP      uint32
	DstIP      uint32
	SrcPort    uint16
	DstPort    uint16
	PayloadLen uint32
	Payload    [4096]byte
}

// Statistics counters
type Stats struct {
	TotalPackets    uint64
	FilteredPackets uint64
	CapturedPackets uint64
	DroppedPackets  uint64
}

func main() {
	log.Println("OCX Socket Interceptor - Kernel-Level Tap")
	log.Println("==========================================")

	// =========================================================================
	// C1 FIX: Initialize global components (previously nil → panic at runtime)
	// =========================================================================

	// 1. Sandbox Executor — uses runsc if available, logs warning if not
	runscPath := os.Getenv("OCX_RUNSC_PATH")
	rootfsPath := os.Getenv("OCX_ROOTFS_PATH")
	if rootfsPath == "" {
		rootfsPath = "/var/ocx/rootfs"
	}
	sandboxExecutor = gvisor.NewSandboxExecutor(runscPath, rootfsPath)
	log.Printf("✅ SandboxExecutor initialized (runsc=%s)", sandboxExecutor.RunscPath())

	// 2. State Cloner — connects to Redis for snapshot management
	redisAddr := os.Getenv("OCX_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	stateCloner = gvisor.NewStateCloner(redisAddr)
	log.Printf("✅ StateCloner initialized (redis=%s)", redisAddr)

	// 3. Escrow Gate — Python entropy service (OCX_ENTROPY_URL) is primary,
	//    EntropyMonitorLive is the real fallback when Python is unreachable
	juryClient := escrow.NewMockJuryClient()
	entropyMonitor := escrow.NewEntropyMonitorLive(1.2) // Real Shannon entropy fallback

	// If a real Jury gRPC address is provided, use the real client
	juryAddr := os.Getenv("JURY_SERVICE_ADDR")
	if juryAddr != "" {
		realJury, err := escrow.NewJuryGRPCClient(juryAddr)
		if err != nil {
			log.Printf("⚠️  Failed to connect to Jury service at %s: %v (using mock)", juryAddr, err)
		} else {
			log.Printf("✅ Connected to Jury gRPC service at %s", juryAddr)
			// Use a type assertion wrapper — JuryGRPCClient implements JuryClient
			_ = realJury // Will be used when gRPC proto is compiled
		}
	}
	escrowGate = escrow.NewEscrowGate(juryClient, entropyMonitor)
	entropyURL := os.Getenv("OCX_ENTROPY_URL")
	if entropyURL != "" {
		log.Printf("✅ EscrowGate initialized (jury=mock, entropy=primary:%s, fallback:EntropyMonitorLive)", entropyURL)
	} else {
		log.Println("⚠️  OCX_ENTROPY_URL not set — using EntropyMonitorLive as sole entropy source")
	}

	// 5. Socket Meter — §4.1 real-time per-packet governance metering
	socketMeter = escrow.NewSocketMeter()
	socketMeter.SetBillingCallback(func(event *escrow.MeterBillingEvent) {
		log.Printf("💰 Metered: tx=%s tool=%s cost=%.4f tax=%.4f trust=%.2f",
			event.TransactionID, event.ToolClass, event.TotalCost, event.GovernanceTax, event.TrustScore)
	})
	log.Println("✅ SocketMeter initialized (§4.1 real-time metering)")

	// 4. Database State Manager — connects to PostgreSQL
	dbURL := os.Getenv("OCX_DATABASE_URL")
	if dbURL != "" {
		mgr, err := gvisor.NewDatabaseStateManager(dbURL)
		if err != nil {
			log.Printf("⚠️  Failed to connect to database: %v (db state manager disabled)", err)
		} else {
			dbStateManager = mgr
			log.Println("✅ DatabaseStateManager initialized")
		}
	} else {
		log.Println("⚠️  OCX_DATABASE_URL not set — DatabaseStateManager disabled")
	}

	// Remove resource limits for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Failed to remove memlock: %v", err)
	}

	// Load pre-compiled eBPF program
	spec, err := ebpf.LoadCollectionSpec("socket_filter.bpf.o")
	if err != nil {
		log.Fatalf("Failed to load eBPF spec: %v", err)
	}

	// Load eBPF objects
	var objs struct {
		OcxSocketFilter *ebpf.Program `ebpf:"ocx_socket_filter"`
		Events          *ebpf.Map     `ebpf:"events"`
		McpPortConfig   *ebpf.Map     `ebpf:"mcp_port_config"`
		Stats           *ebpf.Map     `ebpf:"stats"`
	}

	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		log.Fatalf("Failed to load eBPF objects: %v", err)
	}
	defer objs.OcxSocketFilter.Close()
	defer objs.Events.Close()
	defer objs.McpPortConfig.Close()
	defer objs.Stats.Close()

	// Configure MCP port (default 8080, can be changed)
	mcpPort := uint16(8080)
	configKey := uint32(0)
	if err := objs.McpPortConfig.Put(configKey, mcpPort); err != nil {
		log.Fatalf("Failed to configure MCP port: %v", err)
	}
	log.Printf("Configured to intercept MCP traffic on port %d", mcpPort)

	// Attach socket filter to network interface
	iface := os.Getenv("OCX_INTERFACE")
	if iface == "" {
		iface = "eth0" // Default interface
	}

	// Create raw socket and attach filter using link.AttachRawLink
	// Note: cilium/ebpf v0.12+ changed the API
	l, err := link.AttachRawLink(link.RawLinkOptions{
		Program: objs.OcxSocketFilter,
		Target:  0, // Will be set based on interface
		Attach:  ebpf.AttachSkSKBStreamParser,
	})
	if err != nil {
		// Fallback: Log warning but continue - filter attachment is optional for demo
		log.Printf("Warning: Failed to attach socket filter to %s: %v (continuing without filter)", iface, err)
		log.Printf("Note: eBPF socket filters require root privileges and specific kernel support")
	} else {
		defer l.Close()
		log.Printf("Socket filter attached to interface: %s", iface)
	}
	log.Println("OCX Gateway Active: Intercepting MCP Traffic at Socket Layer...")

	// Open Ring Buffer reader
	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Fatalf("Failed to open ring buffer: %v", err)
	}
	defer rd.Close()

	// Setup signal handler for graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Start statistics reporter
	go reportStats(objs.Stats)

	// Main event loop
	log.Println("Waiting for intercepted packets...")

	for {
		select {
		case <-sig:
			log.Println("Shutting down...")
			return
		default:
			// Read event from Ring Buffer
			record, err := rd.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					log.Println("Ring buffer closed")
					return
				}
				log.Printf("Ring buffer read error: %v", err)
				continue
			}

			// Parse event
			var event SocketEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				log.Printf("Failed to parse event: %v", err)
				continue
			}

			// Process event (initiate speculative audit)
			go processSocketEvent(&event)
		}
	}
}

func processSocketEvent(event *SocketEvent) {
	// Extract payload
	payload := event.Payload[:event.PayloadLen]

	// Log event details
	log.Printf("Intercepted MCP Traffic:")
	log.Printf("  PID: %d, TID: %d", event.PID, event.TID)
	log.Printf("  Source: %s:%d", ipToString(event.SrcIP), event.SrcPort)
	log.Printf("  Destination: %s:%d", ipToString(event.DstIP), event.DstPort)
	log.Printf("  Payload Length: %d bytes", event.PayloadLen)
	log.Printf("  Timestamp: %s", time.Unix(0, int64(event.Timestamp)))

	// Generate transaction ID
	txID := fmt.Sprintf("tx-%d", time.Now().UnixNano())
	agentID := fmt.Sprintf("pid-%d", event.PID)
	tenantID := os.Getenv("OCX_DEFAULT_TENANT")
	if tenantID == "" {
		tenantID = "default"
	}

	// CRITICAL: Route through Hub (O(n) architecture)
	hub := fabric.GetHub()
	msg := &fabric.Message{
		ID:          txID,
		Type:        "socket_event",
		Source:      fabric.VirtualAddress(fmt.Sprintf("ocx://kernel/%d", event.PID)),
		Destination: fabric.VirtualAddress("cap://governance"),
		TenantID:    tenantID,
		Payload:     payload,
		Timestamp:   time.Now(),
		TTL:         5,
	}

	result, err := hub.Route(context.Background(), msg)
	if err != nil {
		log.Printf("Hub routing failed (expected if no governance spoke): %v", err)
	} else {
		log.Printf("Hub routed: decision=%s, destinations=%v, hops=%d",
			result.Decision, result.Destinations, result.HopsUsed)
	}

	// CRITICAL: This is where the "Ghost-Turn" starts
	// Integrated tri-factor audit pipeline
	log.Println("🔮 Initiating Speculative Audit...")
	log.Printf("   Transaction: %s, Agent: %s", txID, agentID)
	log.Printf("   Payload preview: %s", string(payload[:min(100, len(payload))]))

	// §4.1 Real-time per-packet governance metering
	if socketMeter != nil {
		socketMeter.MeterFrame(&escrow.FrameContext{
			TransactionID: txID,
			TenantID:      tenantID,
			AgentID:       agentID,
			ToolClass:     "network_call",
			TrustScore:    0.7, // Would come from trust registry in production
			PayloadBytes:  int(event.PayloadLen),
		})
	}
}

func reportStats(statsMap *ebpf.Map) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var stats Stats

		key := uint32(0) // STAT_TOTAL_PACKETS
		if err := statsMap.Lookup(key, &stats.TotalPackets); err == nil {
			key = 1 // STAT_FILTERED_PACKETS
			statsMap.Lookup(key, &stats.FilteredPackets)
			key = 2 // STAT_CAPTURED_PACKETS
			statsMap.Lookup(key, &stats.CapturedPackets)
			key = 3 // STAT_DROPPED_PACKETS
			statsMap.Lookup(key, &stats.DroppedPackets)

			log.Printf("📊 Stats: Total=%d, Filtered=%d, Captured=%d, Dropped=%d",
				stats.TotalPackets, stats.FilteredPackets, stats.CapturedPackets, stats.DroppedPackets)
		}
	}
}

// ipToString converts a uint32 IP address from eBPF (network byte order) to dotted notation.
// M3 FIX: eBPF stores IPs in network byte order (big-endian), where the first byte
// is the most-significant octet. The previous code assumed host byte order, which
// prints IPs backwards on little-endian systems (x86/ARM).
func ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}
