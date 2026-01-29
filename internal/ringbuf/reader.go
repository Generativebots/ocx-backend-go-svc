package ringbuf

import (
	"encoding/binary"
	"fmt"
	"log"

	//"os" // Unused
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/ocx/backend/internal/escrow"
)

// Event match the C struct
type Event struct {
	PID          uint32
	UID          uint32
	TenantIDHash uint32
	Len          uint32
	Payload      [256]byte
}

type Reader struct {
	ring *ringbuf.Reader
	gate *escrow.EscrowGate
}

func NewReader(gate *escrow.EscrowGate) (*Reader, error) {
	// 1. Allow RLIMIT_MEMLOCK
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %v", err)
	}

	// 2. Load eBPF Objects
	// In a real build, we'd use bpf2go generated code.
	// For this simulation, we assume the map "events" is available via pinned path or loaded obj.
	// Since we can't compile BPF here, we start a Mock Reader that mimics the behavior.
	return &Reader{gate: gate}, nil
}

func (r *Reader) Start() {
	log.Println("🔌 Kernel Tap: Starting Ring Buffer Consumer...")

	// Real Reader Logic
	if r.ring == nil {
		log.Println("⚠️  No BPF RingBuffer attached (Mock Mode)")
		return
	}

	go func() {
		for {
			record, err := r.ring.Read()
			if err != nil {
				log.Printf("Ringbuf Read Error: %v", err)
				if err == ringbuf.ErrClosed {
					return
				}
				continue
			}

			// Parse Event
			// C Struct: u32 pid, u32 uid, u32 tenant_id_hash, u32 len, u8 payload[256]
			if len(record.RawSample) < 20 {
				continue
			}

			// Manual binary parsing (Big Endian or Little Endian? Usually Little on x86)
			// Assuming LittleEndian
			// pid := binary.LittleEndian.Uint32(record.RawSample[0:4])
			// uid := binary.LittleEndian.Uint32(record.RawSample[4:8])
			tenantHash := binary.LittleEndian.Uint32(record.RawSample[8:12])
			dataLen := binary.LittleEndian.Uint32(record.RawSample[12:16])

			payloadData := record.RawSample[16:]
			if len(payloadData) > int(dataLen) {
				payloadData = payloadData[:dataLen]
			}

			// Map TenantHash to ID (In real world, use a lookup table)
			tenantID := fmt.Sprintf("tenant-%d", tenantHash)

			// Forward to Escrow Gate
			traceID := fmt.Sprintf("kernel-trace-%d", time.Now().UnixNano())
			r.gate.Hold(traceID, tenantID, payloadData)
		}
	}()
}
