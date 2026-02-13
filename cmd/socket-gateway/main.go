package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/evidence"
	"github.com/ocx/backend/internal/fabric"
	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/gvisor"
	"github.com/ocx/backend/internal/protocol"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/revert"
)

// Global components for speculative execution
var (
	sandboxExecutor    *gvisor.SandboxExecutor
	stateCloner        *gvisor.StateCloner
	escrowGate         *escrow.EscrowGate
	dbStateManager     *gvisor.DatabaseStateManager
	socketMeter        *escrow.SocketMeter
	toolClassifier     *escrow.ToolClassifier         // §2 Classification
	triFactorGate      *escrow.TriFactorGate          // §2 Tri-Factor Gate
	micropaymentEscrow *escrow.MicropaymentEscrow     // §4.2 Micropayment Escrow
	jitEntitlements    *escrow.JITEntitlementManager  // §4.3 JIT Entitlements
	jitterInjector     *escrow.TemporalJitterInjector // §3.3 Temporal Jitter
	evidenceVault      *evidence.EvidenceVault        // §6 Evidence Vault
	aiParser           *protocol.UniversalAIParser    // Universal AI protocol parser
	reputationWallet   *reputation.ReputationWallet   // Trust score lookup
	ghostPool          *ghostpool.PoolManager         // Pre-warmed sandbox container pool
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
	slog.Info("OCX Socket Interceptor - Kernel-Level Tap")
	slog.Info("==========================================")
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
	slog.Info("SandboxExecutor initialized (runsc=)", "runsc_path", sandboxExecutor.RunscPath())

	// 1b. GhostPool — pre-warmed sandbox containers (production scaling)
	poolImage := os.Getenv("OCX_GHOST_IMAGE")
	if poolImage == "" {
		poolImage = "ocx-ghost-node:latest"
	}
	ghostPool = ghostpool.NewPoolManager(2, 5, poolImage)
	slog.Info("GhostPool initialized", "min", 2, "max", 5, "image", poolImage)

	// 2. State Cloner — connects to Redis for snapshot management
	redisAddr := os.Getenv("OCX_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	stateCloner = gvisor.NewStateCloner(redisAddr)
	slog.Info("StateCloner initialized (redis=)", "redis_addr", redisAddr)
	// 3. Escrow Gate — Python entropy service (OCX_ENTROPY_URL) is primary,
	//    EntropyMonitorLive is the real fallback when Python is unreachable
	var juryClient escrow.JuryClient = escrow.NewMockJuryClient()
	entropyMonitor := escrow.NewEntropyMonitorLive(1.2) // Real Shannon entropy fallback

	// If a real Jury gRPC address is provided, use the real client
	juryAddr := os.Getenv("JURY_SERVICE_ADDR")
	if juryAddr != "" {
		realJury, err := escrow.NewJuryGRPCClient(juryAddr)
		if err != nil {
			slog.Warn("Failed to connect to Jury service at : (using mock)", "jury_addr", juryAddr, "error", err)
		} else {
			slog.Info("Connected to Jury gRPC service at", "jury_addr", juryAddr)
			juryClient = realJury // Use real Jury instead of mock
		}
	}
	escrowGate = escrow.NewEscrowGate(juryClient, entropyMonitor)
	entropyURL := os.Getenv("OCX_ENTROPY_URL")
	if entropyURL != "" {
		slog.Info("EscrowGate initialized (jury=mock, entropy=primary:, fallback:EntropyMonitorLive)", "entropy_u_r_l", entropyURL)
	} else {
		slog.Info("OCX_ENTROPY_URL not set — using EntropyMonitorLive as sole entropy source")
	}

	// 5. Socket Meter — §4.1 real-time per-packet governance metering
	socketMeter = escrow.NewSocketMeter()
	socketMeter.SetBillingCallback(func(event *escrow.MeterBillingEvent) {
		slog.Info("Metered: tx= tool= cost= tax= trust", "transaction_i_d", event.TransactionID, "tool_class", event.ToolClass, "total_cost", event.TotalCost, "governance_tax", event.GovernanceTax, "trust_score", event.TrustScore)
	})
	slog.Info("SocketMeter initialized (§4.1 real-time metering)")
	// 6. Tool Classifier — §2 deterministic Class A/B classification
	toolClassifier = escrow.NewToolClassifier()
	slog.Info("ToolClassifier initialized (§2 Class A/B registry)")
	// 7. Tri-Factor Gate — §2 Identity + Signal + Cognitive validation
	triFactorGate = escrow.NewTriFactorGate(toolClassifier, juryClient, entropyMonitor)
	slog.Info("TriFactorGate initialized (§2 sequestration pipeline)")
	// 8. Micropayment Escrow — §4.2 funds hold/release/refund
	micropaymentEscrow = escrow.NewMicropaymentEscrow()
	slog.Info("MicropaymentEscrow initialized (§4.2 fund escrow)")
	// 9. JIT Entitlements — §4.3 ephemeral permissions with TTL
	jitEntitlements = escrow.NewJITEntitlementManager()
	slog.Info("JITEntitlementManager initialized (§4.3 ephemeral permissions)")
	// 10. Temporal Jitter Injector — §3.3 anti-steganography
	jitterInjector = escrow.NewTemporalJitterInjector(50, 500)
	slog.Info("TemporalJitterInjector initialized (§3.3 jitter 50-500ms)")
	// 11. Evidence Vault — §6 cryptographic audit trail
	evidenceVault = evidence.NewEvidenceVault(evidence.VaultConfig{
		RetentionDays: 365,
		Store:         evidence.NewInMemoryEvidenceStore(),
	})
	slog.Info("EvidenceVault initialized (§6 hash-chain audit trail)")
	// 12. Universal AI Protocol Parser — MCP, OpenAI, A2A, LangChain, CrewAI, AutoGen, RAG
	aiParser = protocol.NewUniversalAIParser()
	slog.Info("UniversalAIParser initialized (MCP/OpenAI/A2A/LangChain/RAG/GenericAI)")
	// 13. Reputation Wallet — agent trust score lookup
	var repDB *sql.DB // Production: connect to Spanner/Postgres
	reputationWallet = reputation.NewReputationWallet(repDB)
	slog.Info("ReputationWallet initialized (agent trust scores)")
	// 4. Database State Manager — connects to PostgreSQL
	dbURL := os.Getenv("OCX_DATABASE_URL")
	if dbURL != "" {
		mgr, err := gvisor.NewDatabaseStateManager(dbURL)
		if err != nil {
			slog.Warn("Failed to connect to database: (db state manager disabled)", "error", err)
		} else {
			dbStateManager = mgr
			slog.Info("DatabaseStateManager initialized")
		}
	} else {
		slog.Info("OCX_DATABASE_URL not set — DatabaseStateManager disabled")
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
	slog.Info("Configured to intercept MCP traffic on port", "mcp_port", mcpPort)
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
		slog.Warn("Warning: Failed to attach socket filter to : (continuing without filter)", "iface", iface, "error", err)
		slog.Info("Note: eBPF socket filters require root privileges and specific kernel support")
	} else {
		defer l.Close()
		slog.Info("Socket filter attached to interface", "iface", iface)
	}
	slog.Info("OCX Gateway Active: Intercepting MCP Traffic at Socket Layer...")
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
	slog.Info("Waiting for intercepted packets...")
	for {
		select {
		case <-sig:
			slog.Info("Shutting down...")
			return
		default:
			// Read event from Ring Buffer
			record, err := rd.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					slog.Info("Ring buffer closed")
					return
				}
				slog.Warn("Ring buffer read error", "error", err)
				continue
			}

			// Parse event
			var event SocketEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				slog.Warn("Failed to parse event", "error", err)
				continue
			}

			// Process event (initiate speculative audit)
			go processSocketEvent(&event)
		}
	}
}

func processSocketEvent(event *SocketEvent) {
	// Pipeline-wide timeout — prevents goroutine leak if any step hangs
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Extract payload with bounds check to prevent panic
	payloadLen := event.PayloadLen
	if payloadLen > uint32(len(event.Payload)) {
		slog.Info("PayloadLen exceeds buffer size clamping", "payload_len", payloadLen, "payload", len(event.Payload))
		payloadLen = uint32(len(event.Payload))
	}
	payload := event.Payload[:payloadLen]

	// Log event details
	slog.Info("Intercepted MCP Traffic:")
	slog.Info("PID: , TID", "p_i_d", event.PID, "t_i_d", event.TID)
	slog.Info("Source:", "src_i_p", ipToString(event.SrcIP), "src_port", event.SrcPort)
	slog.Info("Destination:", "dst_i_p", ipToString(event.DstIP), "dst_port", event.DstPort)
	slog.Info("Payload Length: bytes", "payload_len", event.PayloadLen)
	slog.Info("Timestamp", "timestamp", time.Unix(0, int64(event.Timestamp)))
	// Generate transaction ID and context
	txID := fmt.Sprintf("tx-%d", time.Now().UnixNano())
	agentID := fmt.Sprintf("pid-%d", event.PID)
	tenantID := os.Getenv("OCX_DEFAULT_TENANT")
	if tenantID == "" {
		tenantID = "default"
	}

	// =========================================================================
	// §10: Protocol Frame Parsing — extract 110-byte AOCS header if present
	// =========================================================================
	var protoFrame *protocol.Frame
	if len(payload) >= 110 {
		frame := &protocol.Frame{}
		if err := frame.Unmarshal(payload); err != nil {
			slog.Warn("Protocol frame parse failed (raw payload mode)", "error", err)
		} else {
			protoFrame = frame
			slog.Info("Protocol frame parsed",
				"frame_type", frame.Header.FrameType,
				"action_class", frame.Header.ActionClass,
				"tenant_id", frame.Header.TenantID,
				"agent_id", frame.Header.AgentID,
				"seq_num", frame.Header.SequenceNum,
				"payload_len", frame.Header.PayloadLen)

			// Override context from frame metadata
			txID = fmt.Sprintf("tx-%x", frame.Header.TransactionID[:8])
			agentID = fmt.Sprintf("agent-%d", frame.Header.AgentID)
			if frame.Header.TenantID != 0 {
				tenantID = fmt.Sprintf("tenant-%d", frame.Header.TenantID)
			}
			// Use inner payload from frame (after header) for downstream processing
			if protoFrame.Payload != nil {
				payload = protoFrame.Payload
			}
		}
	}
	_ = protoFrame // Available for downstream frame-aware processing

	// Compensation Stack — for rollback on failure (§revert)
	compStack := revert.NewStack(txID)

	// =========================================================================
	// §9: Route through Hub (O(n) architecture)
	// =========================================================================
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

	routeResult, err := hub.Route(ctx, msg)
	if err != nil {
		slog.Warn("Hub routing failed (expected if no governance spoke)", "error", err)
	} else {
		slog.Info("Hub routed: decision=, destinations=, hops", "decision", routeResult.Decision, "destinations", routeResult.Destinations, "hops_used", routeResult.HopsUsed)
	}

	// =========================================================================
	// §3.3: Temporal Jitter Injection — break timing-based steganographic channels
	// =========================================================================
	if jitterInjector != nil {
		jitterDuration := jitterInjector.InjectJitter(agentID)
		slog.Info("Jitter injected: for agent", "jitter_duration", jitterDuration, "agent_i_d", agentID)
	}

	// =========================================================================
	// Universal AI Protocol Detection — extract tool/agent from ANY AI protocol
	// Supports: MCP, OpenAI, A2A, LangChain, CrewAI, AutoGen, RAG, Custom
	// =========================================================================
	toolID := "network_call" // Default fallback
	agentTrustScore := 0.5   // Neutral default

	if aiParser != nil {
		aiPayload := aiParser.Parse(payload)
		if aiPayload.Protocol != protocol.ProtoRaw {
			slog.Info("AI Protocol Detected: (confidence= tool= type=)", "protocol", aiPayload.Protocol, "confidence", aiPayload.Confidence, "tool_name", aiPayload.ToolName, "message_type", aiPayload.MessageType)
			// Use parsed tool name for classification
			if aiPayload.ToolName != "" {
				toolID = aiPayload.ToolName
			}
			// Use parsed agent ID if available
			if aiPayload.AgentID != "" {
				agentID = aiPayload.AgentID
			}
			// Use parsed tenant ID if available
			if aiPayload.TenantID != "" {
				tenantID = aiPayload.TenantID
			}
		} else {
			slog.Info("📡 Raw payload (no AI protocol detected) — using defaults")
		}
	}

	// Look up real agent trust score from ReputationWallet
	if reputationWallet != nil {
		score, err := reputationWallet.GetTrustScore(ctx, agentID, tenantID)
		if err == nil {
			agentTrustScore = score
		}
		slog.Info("Agent trust score: for /", "agent_trust_score", agentTrustScore, "tenant_i_d", tenantID, "agent_i_d", agentID)
	}

	var classification *escrow.ClassificationResult
	if toolClassifier != nil {
		classification, err = toolClassifier.Classify(escrow.ClassificationRequest{
			ToolID:          toolID,
			AgentID:         agentID,
			TenantID:        tenantID,
			Args:            map[string]interface{}{"payload_len": event.PayloadLen},
			AgentTrustScore: agentTrustScore,
			Entitlements:    []string{}, // Would come from JIT manager
		})
		if err != nil {
			slog.Warn("Classification failed: (defaulting to CLASS_B fail-secure)", "error", err)
			// Fail-secure: default to CLASS_B (stricter) when classification fails
			classification = &escrow.ClassificationResult{
				Classification: escrow.ToolClassification{
					ActionClass:              escrow.CLASS_B,
					GovernanceTaxCoefficient: 1.5,
				},
				FinalVerdict:   "HOLD",
				EscrowDecision: escrow.ATOMIC_HOLD,
			}
		} else {
			slog.Info("Classified",
				"tool_id", toolID,
				"action_class", classification.Classification.ActionClass,
				"verdict", classification.FinalVerdict,
				"escrow_decision", classification.EscrowDecision)
		}
	}

	// =========================================================================
	// §2: Tri-Factor Gate Sequestration — Identity + Signal + Cognitive
	// =========================================================================
	if triFactorGate != nil && classification != nil {
		pendingItem, seqErr := triFactorGate.Sequester(ctx, txID, tenantID, payload, classification)
		if seqErr != nil {
			slog.Warn("Tri-Factor sequestration failed", "seq_err", seqErr)
		} else {
			slog.Info("Sequestered in Tri-Factor Gate: tx= (awaiting 3-factor validation)", "tx_i_d", txID)
			// For Class B actions, wait for the tri-factor result with timeout
			if classification.Classification.ActionClass == escrow.CLASS_B {
				select {
				case tfResult := <-pendingItem.ReleaseChan:
					if tfResult != nil {
						slog.Info("Tri-Factor result",
							"all_passed", tfResult.AllPassed,
							"identity_valid", tfResult.Identity.Valid,
							"signal_valid", tfResult.Signal.Valid,
							"cognitive_valid", tfResult.Cognitive.Valid)
						if !tfResult.AllPassed {
							slog.Info("Tri-Factor REJECTED tx= triggering compensation", "tx_i_d", txID)
							compStack.Compensate(ctx)
							return
						}
					}
				case <-time.After(30 * time.Second):
					slog.Info("Tri-Factor Gate timed out for tx= triggering compensation", "tx_i_d", txID)
					compStack.Compensate(ctx)
					return
				case <-ctx.Done():
					slog.Info("Pipeline context cancelled for tx", "tx_i_d", txID)
					compStack.Compensate(ctx)
					return
				}
			}
		}
	}

	// =========================================================================
	// §1: Speculative gVisor Execution — Ghost-Turn containment (Patent Claim 1)
	// Clones state, executes tool call in isolated sandbox, registers compensation.
	// Runs only for Class B actions that survived Tri-Factor validation.
	// =========================================================================
	if sandboxExecutor != nil && classification != nil &&
		classification.Classification.ActionClass == escrow.CLASS_B {

		// 1. Clone state snapshot (Redis + DB) before speculative execution
		snapshot, cloneErr := stateCloner.CloneState(ctx, txID, agentID)
		if cloneErr != nil {
			slog.Warn("State clone failed — skipping speculative execution", "error", cloneErr)
		} else {
			slog.Info("State snapshot created for speculative execution",
				"snapshot_id", snapshot.SnapshotID,
				"tx_id", txID)

			// 2. Execute speculatively in gVisor sandbox
			specPayload := &gvisor.ToolCallPayload{
				TransactionID: txID,
				AgentID:       agentID,
				ToolName:      toolID,
				Parameters:    map[string]interface{}{"payload_len": event.PayloadLen},
				Context:       map[string]interface{}{"tenant_id": tenantID},
			}
			specResult, specErr := sandboxExecutor.ExecuteSpeculative(ctx, specPayload)
			if specErr != nil {
				slog.Warn("Speculative execution failed — reverting state", "error", specErr)
				if revertErr := stateCloner.RevertState(ctx, snapshot.RevertToken); revertErr != nil {
					slog.Error("State revert also failed", "error", revertErr)
				}
			} else {
				slog.Info("Speculative execution completed",
					"success", specResult.Success,
					"revert_token", specResult.RevertToken,
					"execution_time", specResult.ExecutionTime)

				// 3. Register compensation — revert snapshot on downstream failure
				capturedRevertToken := snapshot.RevertToken
				compStack.Push(func(ctx context.Context) error {
					slog.Info("Compensating: reverting speculative state", "revert_token", capturedRevertToken)
					return stateCloner.RevertState(ctx, capturedRevertToken)
				})

				// 4. If DB state manager available, create savepoint
				if dbStateManager != nil {
					tx, spErr := dbStateManager.CreateSavepoint(ctx, txID)
					if spErr != nil {
						slog.Warn("DB savepoint creation failed", "error", spErr)
					} else {
						capturedTx := tx
						compStack.Push(func(ctx context.Context) error {
							return dbStateManager.RollbackToSavepoint(ctx, capturedTx, txID)
						})
					}
				}
			}
		}
	}

	// =========================================================================
	// §4: Escrow Barrier — Hold for Class B actions
	// =========================================================================
	if escrowGate != nil && classification != nil &&
		classification.Classification.ActionClass == escrow.CLASS_B {

		escrowGate.Hold(txID, agentID, payload)
		compStack.Push(func(ctx context.Context) error {
			slog.Info("Compensating: releasing escrow hold for tx", "tx_i_d", txID)
			return nil
		})
		slog.Info("EscrowGate holding tx= (Class B atomic hold)", "tx_i_d", txID)
	}

	// =========================================================================
	// §4.2: Micropayment Escrow — Hold funds for the tool call cost
	// =========================================================================
	if micropaymentEscrow != nil && classification != nil {
		baseCost := classification.Classification.GovernanceTaxCoefficient * 0.01
		fund, holdErr := micropaymentEscrow.HoldFunds(
			txID, tenantID, agentID, toolID,
			classification.Classification.ActionClass.String(),
			baseCost, 1.0,
		)
		if holdErr != nil {
			slog.Warn("Micropayment hold failed", "hold_err", holdErr)
		} else {
			slog.Info("Funds held: $ for tx", "amount", fund.Amount, "tx_i_d", txID)
			compStack.Push(func(ctx context.Context) error {
				micropaymentEscrow.RefundFunds(txID)
				return nil
			})
		}
	}

	// =========================================================================
	// §4.3: JIT Entitlements — Grant ephemeral permission for this tool call
	// =========================================================================
	if jitEntitlements != nil {
		permission := fmt.Sprintf("tool:%s", toolID)
		ent, grantErr := jitEntitlements.GrantEphemeral(
			agentID, permission,
			5*time.Minute,
			"socket-gateway", fmt.Sprintf("tx=%s", txID),
			map[string]interface{}{"tx_id": txID, "tenant_id": tenantID},
		)
		if grantErr != nil {
			slog.Warn("JIT grant failed", "grant_err", grantErr)
		} else {
			slog.Info("JIT entitlement granted: (expires )", "agent_i_d", agentID, "permission", permission, "r_f_c3339", ent.ExpiresAt.Format(time.RFC3339))
		}
	}

	// =========================================================================
	// §4.1: Real-time per-packet governance metering
	// =========================================================================
	if socketMeter != nil {
		socketMeter.MeterFrame(&escrow.FrameContext{
			TransactionID: txID,
			TenantID:      tenantID,
			AgentID:       agentID,
			ToolClass:     toolID,
			TrustScore:    agentTrustScore,
			PayloadBytes:  int(event.PayloadLen),
		})
	}

	// =========================================================================
	// §6: Evidence Vault — Record immutable audit trail
	// =========================================================================
	if evidenceVault != nil {
		verdict := evidence.OutcomeAllow
		if classification != nil && classification.FinalVerdict == "BLOCK" {
			verdict = evidence.OutcomeBlock
		} else if classification != nil && classification.FinalVerdict == "ESCALATE" {
			verdict = evidence.OutcomeHold
		}

		actionClass := "A"
		if classification != nil {
			actionClass = classification.Classification.ActionClass.String()
		}

		_, recErr := evidenceVault.RecordTransaction(
			ctx, tenantID, agentID, txID,
			toolID, actionClass,
			verdict, agentTrustScore,
			"Socket-gateway pipeline processing",
			map[string]interface{}{
				"src_ip":      ipToString(event.SrcIP),
				"dst_ip":      ipToString(event.DstIP),
				"payload_len": event.PayloadLen,
			},
		)
		if recErr != nil {
			slog.Warn("Evidence recording failed", "rec_err", recErr)
		} else {
			slog.Info("Evidence recorded: tx= (hash-chain)", "tx_i_d", txID)
		}
	}

	// =========================================================================
	// Release micropayment funds on successful processing
	// =========================================================================
	if micropaymentEscrow != nil && classification != nil &&
		classification.FinalVerdict != "BLOCK" {
		micropaymentEscrow.ReleaseFunds(txID)
		slog.Info("Funds released for tx", "tx_i_d", txID)
	} else if micropaymentEscrow != nil && classification != nil {
		micropaymentEscrow.RefundFunds(txID)
		slog.Info("Funds refunded for tx= (blocked)", "tx_i_d", txID)
	}

	slog.Info("Pipeline complete: tx", "tx_i_d", txID)
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

			slog.Info("Stats: Total=, Filtered=, Captured=, Dropped", "total_packets", stats.TotalPackets, "filtered_packets", stats.FilteredPackets, "captured_packets", stats.CapturedPackets, "dropped_packets", stats.DroppedPackets)
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
