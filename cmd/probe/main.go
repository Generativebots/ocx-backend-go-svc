package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"google.golang.org/grpc"

	socketio "github.com/googollee/go-socket.io"

	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/ledger"
	"github.com/ocx/backend/internal/plan"
	"github.com/ocx/backend/internal/probe"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/revert"
	"github.com/ocx/backend/internal/snapshot" // Phase 4

	// Phase 8: Economic Barrier
	// Phase 8: Economic Barrier
	"github.com/ocx/backend/pb"
)

// Event matches the memory layout of the C struct exactly.
// Total size: 4+4+16+4+1024 = 1052 bytes (plus padding if necessary)
type Event struct {
	PID     uint32
	FD      uint32
	Comm    [16]byte
	Size    uint32
	Payload [1024]byte
}

var (
	// eventPool reuses memory to avoid allocation-heavy GC cycles.
	eventPool = sync.Pool{
		New: func() interface{} {
			return new(Event)
		},
	}
)

func main() {
	// 1. Allow the current process to lock memory for eBPF resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatal("Removing memlock:", err)
	}

	// 2. Load pre-compiled eBPF objects.
	objs := interceptorObjects{}
	if err := loadInterceptorObjects(&objs, nil); err != nil {
		log.Fatalf("Loading objects: %v", err)
	}
	defer objs.Close()

	// 3. Attach kretprobe to sys_read to capture data AFTER it's written to the buffer.
	// Attach Kretprobe (Exit)
	krp, err := link.Kretprobe("sys_read", objs.KretprobeSysRead, nil)
	if err != nil {
		log.Fatalf("opening kretprobe: %s", err)
	}
	defer krp.Close()

	// Attach LSM (Gatekeeper)
	// Note: This requires a kernel compiled with CONFIG_BPF_LSM=y
	lsmLink, err := link.AttachLSM(link.LSMOptions{
		Program: objs.EnforcePolicy,
	})
	if err != nil {
		log.Fatalf("opening lsm: %s", err)
	}
	defer lsmLink.Close()

	// Populate PID Filter & Verdict Cache
	targetPID := uint32(os.Getpid())

	// Observability Filter
	if err := objs.PidsToTrace.Put(&targetPID, uint8(1)); err != nil {
		slog.Warn("Failed to add PID to filter", "target_p_i_d", targetPID, "error", err)
	}

	// Verdict Cache: Pre-Approve this loader process (Self-Preservation)
	// Verdict 1 = ALLOW.
	allowVerdict := uint32(1)
	if err := objs.VerdictCache.Put(&targetPID, &allowVerdict); err != nil {
		slog.Warn("Failed to whitelist PID", "target_p_i_d", targetPID, "error", err)
	}

	slog.Info("Monitoring & Enforcing on PID: via RingBuffer + LSM", "target_p_i_d", targetPID)
	// Also attach the Kprobe for state tracking (Buffer Address capture)
	// The "Sound-Proof" architecture requires both: Entry (to get address) + Exit (to read data)
	kprobe, err := link.Kprobe("sys_read", objs.KprobeSysRead, nil)
	if err != nil {
		log.Fatalf("Opening kprobe: %s", err)
	}
	defer kprobe.Close()

	// Attach Tracepoint: Process Exit (Lifecycle Management)
	tpExit, err := link.Tracepoint("sched", "sched_process_exit", objs.HandleExit, nil)
	if err != nil {
		log.Fatalf("opening tracepoint: %s", err)
	}
	defer tpExit.Close()

	// 4. Initialize the Ring Buffer reader for Traffic Events.
	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Fatalf("Opening ringbuf reader: %s", err)
	}
	defer rd.Close()

	// 4b. Initialize Ring Buffer reader for Exit Events
	rdExit, err := ringbuf.NewReader(objs.ExitEvents)
	if err != nil {
		log.Fatalf("Opening exit ringbuf reader: %s", err)
	}
	defer rdExit.Close()

	slog.Info("High-Performance Interceptor Active (RingBuffer + sync.Pool)...")
	// --- Synapse Bridge: Socket.IO Server ---
	// This allows the Frontend (TrustDashboard) to see the live pulse.
	ioServer, err := setupSocketServer()
	if err != nil {
		log.Fatalf("Failed to start Synapse Bridge: %v", err)
	}
	// Start HTTP Server for Socket.IO
	go func() {
		slog.Info("Synapse Bridge (Socket.IO) listening on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal("Synapse Bridge failed: ", err)
		}
	}()
	// ----------------------------------------

	// 5. Processing loop
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize Worker Group with Identity Cache AND Socket.IO
	vu := probe.NewVerdictUpdater(objs.VerdictCache)

	// --- Phase 8: Initialize Economic Barrier ---
	// Create Reputation Wallet (auto-detects SQLite vs Spanner from env)
	reputationWallet, err := reputation.NewReputationStoreFromEnv()
	if err != nil {
		log.Fatalf("Failed to initialize Reputation Wallet: %v", err)
	}
	defer reputationWallet.Close()
	slog.Info("Reputation Wallet initialized")
	// Create Escrow Gate with mock clients
	escrowGate := escrow.NewEscrowGate(
		escrow.NewMockJuryClient(),
		escrow.NewMockEntropyMonitor(),
	)
	slog.Info("Economic Barrier (Escrow Gate) initialized")
	// --- Plan Service gRPC Server with Economic Barrier ---
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("failed to listen on :50051: %v", err)
		}

		grpcServer := grpc.NewServer(
			grpc.UnaryInterceptor(escrow.EscrowInterceptor(escrowGate)),
			grpc.StreamInterceptor(escrow.StreamEscrowInterceptor(escrowGate)),
		)
		slog.Info("Plan Service (gRPC) listening on :50051 with Economic Barrier")
		if err := grpcServer.Serve(lis); err != nil {
			slog.Warn("gRPC server error", "error", err)
		}
	}()
	// --------------------------------------------

	wg := NewWorkerGroup(nil, ioServer, vu, escrowGate)
	wg.Start(ctx)

	// Start Lifecycle Manager (Anti-Recycling)
	go wg.cache.WatchExits(rdExit)

	go func() {
		// ... (rest of read loop)

		for {
			// Read a raw sample from the Ring Buffer.
			record, err := rd.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				slog.Warn("Read error", "error", err)
				continue
			}

			// Pull an Event struct from the pool.
			event := eventPool.Get().(*Event)

			// Fast-parse the binary data directly into our struct.
			// FIX: binary.LittleEndian.Reader is invalid. generic binary.Read with bytes.NewReader is used.
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, event); err != nil {
				slog.Warn("Parse error", "error", err)
				eventPool.Put(event)
				continue
			}

			// Submit to Worker Group
			wg.Submit(event)
		}
	}()

	<-ctx.Done()
	ioServer.Close() // Cleanup
	slog.Info("Shutting down gracefully...")
}

// --- Identity Enrichment (Patent-Ready) ---

// Identity represents the immutable footprint of a process.
type Identity struct {
	BinaryPath string
	SHA256     string
}

type IdentityCache struct {
	mu    sync.RWMutex
	cache map[uint32]Identity
}

func NewIdentityCache() *IdentityCache {
	return &IdentityCache{cache: make(map[uint32]Identity)}
}

// Resolve retrieves the binary identity for a given PID.
func (c *IdentityCache) Resolve(pid uint32) (Identity, error) {
	c.mu.RLock()
	// Go specific: Check if entry exists
	if id, exists := c.cache[pid]; exists {
		c.mu.RUnlock()
		return id, nil
	}
	c.mu.RUnlock()

	// 1. Get the binary path from procfs
	// We use strconv to safely convert uint32 to string
	exePath, err := os.Readlink("/proc/" + strconv.Itoa(int(pid)) + "/exe")
	if err != nil {
		return Identity{}, err
	}

	// 2. Calculate SHA-256
	hash, err := calculateSHA256(exePath)
	if err != nil {
		// Fallback or handle error
		return Identity{BinaryPath: exePath, SHA256: "unknown"}, err
	}

	id := Identity{BinaryPath: exePath, SHA256: hash}

	c.mu.Lock()
	c.cache[pid] = id
	c.mu.Unlock()

	return id, nil
}

// WatchExits monitors the exit_events ring buffer and evicts PIDs from the cache.
func (c *IdentityCache) WatchExits(rd *ringbuf.Reader) {
	slog.Info("Lifecycle Manager Active: Watching for Process Exits...")
	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			continue
		}

		// Simple struct for the exit event
		var exit struct{ PID uint32 }
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &exit); err != nil {
			continue
		}

		// Evict from cache to prevent PID collision and memory leaks
		c.mu.Lock()
		delete(c.cache, exit.PID)
		// log.Printf("PID %d Terminated. Evicted Identity.", exit.PID)
		c.mu.Unlock()
	}
}

func calculateSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Synapse Bridge Implementation ---

func setupSocketServer() (*socketio.Server, error) {
	server := socketio.NewServer(nil)

	server.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		// log.Println("Frontend Connected:", s.ID())
		return nil
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		// log.Println("Frontend Disconnected:", s.ID())
	})

	// Setup HTTP handlers
	http.Handle("/socket.io/", server)

	go server.Serve()

	return server, nil
}

// --- Worker Group Implementation (Sound-Proof) ---

// --- Worker Group Implementation (Sound-Proof) ---

const (
	MaxWorkers     = 10   // Fixed concurrent workers for gRPC
	BufferCapacity = 1000 // Backpressure: Max pending events before dropping
)

type WorkerGroup struct {
	eventChan chan *Event
	wg        sync.WaitGroup
	cache     *IdentityCache   // Identity
	socket    *socketio.Server // Synapse Bridge

	// New: Phase 3 Components
	planStore      *plan.PlanStore
	ghostPool      *ghostpool.PoolManager
	verdictUpdater *probe.VerdictUpdater

	// Phase 4
	revertGen   *revert.Generator
	auditLogger *ledger.AuditLogger

	// Phase 8: Economic Barrier
	escrowGate *escrow.EscrowGate
}

func NewWorkerGroup(conn interface{}, s *socketio.Server, vu *probe.VerdictUpdater, eg *escrow.EscrowGate) *WorkerGroup {
	return &WorkerGroup{
		eventChan:      make(chan *Event, BufferCapacity),
		cache:          NewIdentityCache(),
		socket:         s,
		planStore:      plan.NewPlanStore(),
		ghostPool:      ghostpool.NewPoolManager(2, 5, "ocx-ghost-node:latest"),
		verdictUpdater: vu,

		revertGen:   &revert.Generator{},
		auditLogger: ledger.NewAuditLogger(&pb.MockLedgerClient{}), // Use Mock

		escrowGate: eg, // Phase 8
	}
}

func (wg *WorkerGroup) Start(ctx context.Context) {
	// 1. Initialize the bidirectional stream
	// stream, err := wg.client.InspectTraffic(ctx)
	// if err != nil {
	// 	log.Fatalf("Failed to open stream: %v", err)
	// }
	// wg.stream = stream

	// 2. Start Response Listener (Handles Jury decisions)
	// go wg.listenForVerdicts()

	// 3. Start fixed Worker Pool
	for i := 0; i < MaxWorkers; i++ {
		wg.wg.Add(1)
		go wg.worker(ctx)
	}
}

func (wg *WorkerGroup) worker(ctx context.Context) {
	defer wg.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-wg.eventChan:
			// 1. Resolve Identity (Existing)
			id, _ := wg.cache.Resolve(ev.PID)

			// 2. Plan Validation
			allowed, sessionPlan := wg.planStore.Validate(ev.PID, "sys_read")

			action := "ALLOW"
			if !allowed {
				action = "BLOCK_VIOLATION"
			}

			// 3. Ghost Turn & Verification Logic
			manualReq := false
			if sessionPlan != nil {
				// Logic: MATCH -> COMMIT (Update Verdict Cache)

				// For this demo, we assume the "shadowOutcome" is the payload itself (or a simulation of it)
				shadowOutcome := ev.Payload[:]

				// --- PHASE 4: REVERT ENGINE ---
				// Initialize Compensation Stack for this Turn
				stack := revert.NewStack(fmt.Sprintf("%d-%d", ev.PID, ev.Size)) // Use PID+Size as pseudo-ID
				// Example: If it's a file write, push undo
				// stack.Push(wg.revertGen.UndoFileCreation("/tmp/speculative_data.log"))

				// --- PHASE 4: SNAPSHOT VERIFICATION ---
				isAligned, matchErr := snapshot.CompareAndVerify(sessionPlan.ExpectedOutcomeHash, shadowOutcome)

				if sessionPlan.ManualReviewRequired {
					manualReq = true
					action = "PENDING_REVIEW"
				} else {
					// AUTO-COMMIT
					if isAligned || sessionPlan.ExpectedOutcomeHash == "" { // Empty hash = allow all for demo
						// Update Kernel Map to ALLOW
						if err := wg.verdictUpdater.ReleaseProcess(ev.PID); err != nil {
							slog.Warn("Failed to release PID", "p_i_d", ev.PID, "error", err)
							action = "ERROR_COMMIT"
						} else {
							action = "AUTO_COMMITTED"

							// --- PHASE 4: AUDIT LEDGER (ASYNC) ---
							auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
							wg.auditLogger.LogTurn(auditCtx, &ledger.TurnData{
								TurnID:     stack.TurnID,
								AgentID:    sessionPlan.AgentId,
								Status:     pb.LedgerEntry_COMMITTED,
								IntentHash: sessionPlan.ExpectedOutcomeHash,
								ActualHash: "calculated-hash",
							})
							auditCancel()
						}
					} else {
						slog.Info("HASH MISMATCH", "match_err", matchErr)
						action = "BLOCK_MISMATCH"
						wg.verdictUpdater.RevokeProcess(ev.PID)

						// --- PHASE 4: COMPENSATE ---
						slog.Info("Rolling back environmental changes...")
						compCtx, compCancel := context.WithTimeout(context.Background(), 5*time.Second)
						stack.Compensate(compCtx)
						compCancel()

						// Log Rejection
						rejectCtx, rejectCancel := context.WithTimeout(context.Background(), 5*time.Second)
						wg.auditLogger.LogTurn(rejectCtx, &ledger.TurnData{
							TurnID:     stack.TurnID,
							AgentID:    sessionPlan.AgentId,
							Status:     pb.LedgerEntry_COMPENSATED,
							IntentHash: sessionPlan.ExpectedOutcomeHash,
							ActualHash: "calculated-hash-mismatch",
						})
						rejectCancel()
					}
				}
			}
			// 5. Broadcast to Frontend (Synapse Bridge)
			uiEvent := map[string]interface{}{
				"pid":           ev.PID,
				"comm":          string(bytes.TrimRight(ev.Comm[:], "\x00")),
				"size":          ev.Size,
				"binary_hash":   id.SHA256,
				"action":        action,
				"manual_review": manualReq, // Trigger for UI Modal
			}

			wg.socket.BroadcastToNamespace("/", "traffic_event", uiEvent)

			// Logging
			slog.Info("TRAFFIC: PID | Action: | Hash", "p_i_d", ev.PID, "action", action, "s_h_a2568", id.SHA256[:8])
			// Return the struct to our sync.Pool
			eventPool.Put(ev)
		}
	}
}

func (wg *WorkerGroup) listenForVerdicts() {
	// for {
	// 	res, err := wg.stream.Recv()
	// 	if err != nil {
	// 		log.Printf("Stream recv error (Jury disconnected?): %v", err)
	// 		return
	// 	}
	// 	log.Printf("VERDICT: ID %s | Action: %v | Reasoning: %s",
	// 		res.RequestId, res.Verdict.Action, res.Reasoning)
	// }
}

// Submit is called from the eBPF read loop
func (wg *WorkerGroup) Submit(ev *Event) {
	select {
	case wg.eventChan <- ev:
		// Event accepted into the queue
	default:
		// BACKPRESSURE: Queue is full, drop packet to protect system
		slog.Info("WARNING: Drop packet (Jury too slow)")
		eventPool.Put(ev)
	}
}
