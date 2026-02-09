// interceptor.bpf.c - LSM-based Active Blocking Interceptor
// Provides enforcement capabilities for the OCX Protocol

#include <vmlinux.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

char __license[] SEC("license") = "Dual MIT/GPL";

// Verdict constants matching Protobuf/Go enums
#define ACTION_ALLOW 0
#define ACTION_BLOCK 1
#define ACTION_HOLD  2  // Speculative execution

// Maximum entries for production scale
#define MAX_VERDICTS 100000
#define MAX_IDENTITIES 100000

// ============================================================================
// BPF MAPS
// ============================================================================

// Verdict cache: PID -> Action (Allow/Block/Hold)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_VERDICTS);
    __type(key, u32);   // PID
    __type(value, u32); // Action
} verdict_cache SEC(".maps");

// Identity Value Structure
struct identity_t {
    u64 binary_hash; // First 64 bits of SHA-256
    u32 tenant_id;   // Hashed Tenant ID
};

// Identity cache: PID -> Identity Info
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_IDENTITIES);
    __type(key, u32);               // PID
    __type(value, struct identity_t); // Identity Info
} identity_cache SEC(".maps");

// Trust level cache: PID -> Trust Level (0-100)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_VERDICTS);
    __type(key, u32);   // PID
    __type(value, u32); // Trust level (0-100)
} trust_cache SEC(".maps");

// Event ring buffer for userspace communication
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} events SEC(".maps");

// ============================================================================
// AOCS TOOL CLASSIFICATION (Per AOCS Specification)
// ============================================================================

// Action classification constants
#define CLASS_A 0  // Reversible - Ghost-Turn (speculative execution)
#define CLASS_B 1  // Irreversible - Atomic-Hold (HITL required)

// Tool metadata for classification
struct tool_meta_t {
    u64 tool_id_hash;           // SHA-256 hash of tool_id string (first 64 bits)
    u32 action_class;           // CLASS_A or CLASS_B
    u32 reversibility_index;    // 0-100 score (0=irreversible, 100=fully reversible)
    u32 min_reputation_score;   // Minimum trust (0-100) required to invoke
    u32 governance_tax_mult;    // Multiplier for audit cost (100 = 1.0x)
    u64 required_entitlements;  // Bitmask of required JIT entitlements
    u32 hitl_required;          // 1 if HITL mandatory, 0 otherwise
};

// Tool registry: Tool ID Hash -> Tool Metadata
#define MAX_TOOLS 1000
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_TOOLS);
    __type(key, u64);                 // Tool ID hash
    __type(value, struct tool_meta_t); // Tool metadata
} tool_registry SEC(".maps");

// Agent entitlements: PID -> Entitlement bitmask
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_IDENTITIES);
    __type(key, u32);   // PID
    __type(value, u64); // Entitlement bitmask
} entitlement_cache SEC(".maps");

// Escrow event ring buffer for Class B actions
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB for escrow events
} escrow_events SEC(".maps");

// Escrow event structure for Tri-Factor Gate processing
struct escrow_event {
    u32 pid;
    u32 tid;
    u64 cgroup_id;
    u64 timestamp;
    u64 tool_id_hash;
    u32 action_class;
    u32 tenant_id;
    u64 binary_hash;
    u32 trust_level;
    u32 reversibility_index;
    u64 required_entitlements;
    u64 present_entitlements;
    u32 entitlement_valid;  // 1 if all required entitlements present
    u32 data_size;
    u8 verdict;             // 0=pending, 1=allow, 2=block
};

// ============================================================================
// EVENT STRUCTURE
// ============================================================================

struct socket_event {
    u32 pid;
    u32 tid;
    u64 cgroup_id; // Added Production-Ready Multi-Tenancy
    u64 timestamp;
    u64 binary_hash;
    u32 tenant_id; // Added Multi-Tenancy
    u32 action;
    u32 trust_level;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u32 data_size;
    u8 protocol;
    u8 blocked;
};

// ============================================================================
// LSM HOOK: socket_sendmsg - ACTIVE BLOCKING
// ============================================================================

SEC("lsm/socket_sendmsg")
int BPF_PROG(ocx_enforce_send, struct socket *sock, struct msghdr *msg, int size)
{
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u32 tid = (u32)bpf_get_current_pid_tgid();

    // 1. Look up verdict from cache
    u32 *verdict = bpf_map_lookup_elem(&verdict_cache, &pid);

    // 2. Check trust level
    u32 *trust_level = bpf_map_lookup_elem(&trust_cache, &pid);
    u32 trust = trust_level ? *trust_level : 50; // Default to 50%

    // 3. Enforcement logic
    if (verdict) {
        if (*verdict == ACTION_BLOCK) {
            // ACTIVE BLOCKING: Return -EPERM to kernel
            bpf_printk("OCX BLOCK: PID %d attempted unauthorized sendmsg (trust=%d)", pid, trust);
            
            // Get Identity for Tenant ID
            struct identity_t *ident = bpf_map_lookup_elem(&identity_cache, &pid);
            u32 tenant_id = ident ? ident->tenant_id : 0;
            u64 bin_hash = ident ? ident->binary_hash : 0;

            // Log blocked event
            struct socket_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
            if (event) {
                event->pid = pid;
                event->tid = tid;
                event->cgroup_id = bpf_get_current_cgroup_id(); // Capture Cgroup ID
                event->timestamp = bpf_ktime_get_ns();
                event->tenant_id = tenant_id;
                event->binary_hash = bin_hash;
                event->action = ACTION_BLOCK;
                event->trust_level = trust;
                event->data_size = size;
                event->blocked = 1;
                bpf_ringbuf_submit(event, 0);
            }
            
            return -EPERM; // BLOCK THE SYSCALL
        }
        
        if (*verdict == ACTION_HOLD) {
            // SPECULATIVE EXECUTION: Hold until verdict arrives
            bpf_printk("OCX HOLD: PID %d in speculative execution", pid);
            return -EAGAIN; // Tell kernel to retry
        }
    }

    // ========================================================================
    // AOCS TOOL CLASSIFICATION CHECK (Per AOCS Specification)
    // ========================================================================
    
    // In production, tool_id_hash would be extracted from payload via Deep Packet Inspection
    // For now, we use a placeholder that would be set by the control plane
    // The userspace control plane populates tool_registry based on classifier.go
    
    // Try to lookup tool metadata (tool_id_hash would be extracted from packet)
    // This is a simplified version - production would parse the AOCS packet header
    // to extract the 32-byte tool_id field and hash it
    
    // For demonstration, we check if there's an escrow hold required
    // The control plane can populate verdict_cache with ACTION_HOLD for Class B tools
    
    // Get agent's current entitlements
    u64 *agent_entitlements = bpf_map_lookup_elem(&entitlement_cache, &pid);
    u64 entitlements = agent_entitlements ? *agent_entitlements : 0;
    
    // Get identity information
    struct identity_t *ident = bpf_map_lookup_elem(&identity_cache, &pid);
    u32 tenant_id = ident ? ident->tenant_id : 0;
    u64 bin_hash = ident ? ident->binary_hash : 0;
    
    // Check if this is a CLASS_B action that requires Tri-Factor Gate
    // In production, this would be determined by parsing the tool_id from payload
    // For now, we use a heuristic: trust < 65 AND large payload = Class B
    u32 is_class_b = (trust < 65 && size > 1024);
    
    if (is_class_b) {
        // CLASS_B: Emit escrow event for Tri-Factor Gate validation
        // Then BLOCK until control plane releases
        bpf_printk("OCX CLASS_B: PID %d requires Tri-Factor Gate (size=%d, trust=%d)", pid, size, trust);
        
        struct escrow_event *escrow_ev = bpf_ringbuf_reserve(&escrow_events, sizeof(*escrow_ev), 0);
        if (escrow_ev) {
            escrow_ev->pid = pid;
            escrow_ev->tid = tid;
            escrow_ev->cgroup_id = bpf_get_current_cgroup_id();
            escrow_ev->timestamp = bpf_ktime_get_ns();
            escrow_ev->tool_id_hash = 0; // Would be extracted from packet in production
            escrow_ev->action_class = CLASS_B;
            escrow_ev->tenant_id = tenant_id;
            escrow_ev->binary_hash = bin_hash;
            escrow_ev->trust_level = trust;
            escrow_ev->reversibility_index = 5; // Low - Class B default
            escrow_ev->required_entitlements = 0; // From tool_registry lookup
            escrow_ev->present_entitlements = entitlements;
            escrow_ev->entitlement_valid = 1; // Would check required & present
            escrow_ev->data_size = size;
            escrow_ev->verdict = 0; // Pending - awaiting Tri-Factor Gate
            bpf_ringbuf_submit(escrow_ev, 0);
        }
        
        // ATOMIC_HOLD: Block until control plane updates verdict_cache
        // Control plane will set verdict_cache[pid] = ACTION_ALLOW after Tri-Factor passes
        return -EAGAIN; // Tell kernel to retry - we're in HOLD state
    }

    // 4. Trust-based enforcement
    // If trust level is below threshold, block even without explicit verdict
    if (trust < 30) { // 30% minimum trust threshold
        bpf_printk("OCX BLOCK: PID %d below trust threshold (trust=%d)", pid, trust);
        return -EPERM;
    }

    // 5. Allow traffic (default fail-open for compatibility)
    // In strict mode, change to fail-closed: return -EPERM unless explicitly allowed
    
    // Log allowed event
    struct socket_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        struct identity_t *ident = bpf_map_lookup_elem(&identity_cache, &pid);
        
        event->pid = pid;
        event->tid = tid;
        event->cgroup_id = bpf_get_current_cgroup_id(); // Capture Cgroup ID
        event->timestamp = bpf_ktime_get_ns();
        event->tenant_id = ident ? ident->tenant_id : 0;
        event->binary_hash = ident ? ident->binary_hash : 0;
        event->action = ACTION_ALLOW;
        event->trust_level = trust;
        event->data_size = size;
        event->blocked = 0;
        bpf_ringbuf_submit(event, 0);
    }

    return 0; // ALLOW
}

// ============================================================================
// LSM HOOK: socket_connect - HANDSHAKE ENFORCEMENT
// ============================================================================

SEC("lsm/socket_connect")
int BPF_PROG(ocx_enforce_connect, struct socket *sock, struct sockaddr *address, int addrlen)
{
    u32 pid = bpf_get_current_pid_tgid() >> 32;

    // Check verdict before allowing connection
    u32 *verdict = bpf_map_lookup_elem(&verdict_cache, &pid);
    
    if (verdict && *verdict == ACTION_BLOCK) {
        bpf_printk("OCX BLOCK: PID %d connection blocked", pid);
        return -EPERM;
    }

    // Check trust level
    u32 *trust_level = bpf_map_lookup_elem(&trust_cache, &pid);
    u32 trust = trust_level ? *trust_level : 50;
    
    if (trust < 30) {
        bpf_printk("OCX BLOCK: PID %d connection blocked (low trust=%d)", pid, trust);
        return -EPERM;
    }

    return 0; // ALLOW
}

// ============================================================================
// TRACEPOINT: Process Exit - Cleanup
// ============================================================================

SEC("tp/sched/sched_process_exit")
int handle_exit(struct trace_event_raw_sched_process_template *ctx)
{
    u32 pid = ctx->pid;

    // Clean up all caches for this PID to prevent PID recycling attacks
    bpf_map_delete_elem(&verdict_cache, &pid);
    bpf_map_delete_elem(&identity_cache, &pid);
    bpf_map_delete_elem(&trust_cache, &pid);

    bpf_printk("OCX CLEANUP: PID %d exited, caches cleared", pid);

    return 0;
}

// ============================================================================
// KPROBE: Binary Hash Capture (for identity persistence)
// ============================================================================

SEC("kprobe/do_execve")
int capture_binary_hash(struct pt_regs *ctx)
{
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // In production, calculate SHA-256 hash of binary
    // For now, use a placeholder hash based on PID
    u64 binary_hash = (u64)pid * 0x123456789ABCDEF;
    
    // Store identity (Tenant ID will be updated by Userspace later)
    struct identity_t ident = {
        .binary_hash = binary_hash,
        .tenant_id = 0, // Unknown initially
    };
    
    bpf_map_update_elem(&identity_cache, &pid, &ident, BPF_ANY);
    
    // Default to HOLD for new processes (require explicit verdict)
    u32 action = ACTION_HOLD;
    bpf_map_update_elem(&verdict_cache, &pid, &action, BPF_ANY);
    
    // Default trust level
    u32 trust = 50;
    bpf_map_update_elem(&trust_cache, &pid, &trust, BPF_ANY);
    
    bpf_printk("OCX IDENTITY: PID %d registered with hash %llx", pid, binary_hash);
    
    return 0;
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

static __always_inline u32 get_trust_level(u32 pid)
{
    u32 *trust = bpf_map_lookup_elem(&trust_cache, &pid);
    return trust ? *trust : 50; // Default 50%
}

static __always_inline bool is_blocked(u32 pid)
{
    u32 *verdict = bpf_map_lookup_elem(&verdict_cache, &pid);
    return verdict && (*verdict == ACTION_BLOCK);
}

static __always_inline bool is_trusted(u32 pid, u32 threshold)
{
    u32 trust = get_trust_level(pid);
    return trust >= threshold;
}
