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
