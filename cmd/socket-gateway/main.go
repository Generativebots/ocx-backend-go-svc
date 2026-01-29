package main

import (
	"bytes"
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
	"github.com/ocx/backend/internal/gvisor"
)

// Global components for speculative execution
var (
	sandboxExecutor *gvisor.SandboxExecutor
	stateCloner     *gvisor.StateCloner
	escrowGate      *escrow.EscrowGate
	dbStateManager  *gvisor.DatabaseStateManager
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

	l, err := link.AttachSocketFilter(iface, objs.OcxSocketFilter)
	if err != nil {
		log.Fatalf("Failed to attach socket filter to %s: %v", iface, err)
	}
	defer l.Close()

	log.Printf("Socket filter attached to interface: %s", iface)
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

	// CRITICAL: This is where the "Ghost-Turn" starts
	// Integrated tri-factor audit pipeline
	log.Println("🔮 Initiating Speculative Audit...")
	log.Printf("   Transaction: %s, Agent: %s", txID, agentID)
	log.Printf("   Payload preview: %s", string(payload[:min(100, len(payload))]))

	// For now, just log the event
	// Full integration requires:
	// - gVisor runtime setup
	// - Jury gRPC server running
	// - Redis for state cloning
	// See integrated_audit.go for complete implementation
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

func ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
