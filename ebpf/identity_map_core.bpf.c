// eBPF CO-RE Identity Mapper
// Uses BTF for kernel portability across versions

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_AGENT_ID_LEN 36
#define MAX_ENTRIES 10240

// Identity structure stored in kernel
struct identity_t {
    char agent_id[MAX_AGENT_ID_LEN];
    __u32 trust_level;           // Trust score * 100 (0-10000)
    __u64 spiffe_svid_hash;      // Hash of SPIFFE SVID
    __u64 registered_at;         // Timestamp
    __u32 parent_pid;            // Parent PID
};

// PID → Identity mapping
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u32);          // PID
    __type(value, struct identity_t);
} pid_identity_map SEC(".maps");

// Statistics
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} identity_stats SEC(".maps");

// Event structure
struct identity_event {
    __u32 pid;
    __u32 parent_pid;
    __u8 event_type;  // 0=fork, 1=exec, 2=exit, 3=lookup
    char agent_id[MAX_AGENT_ID_LEN];
    __u64 timestamp;
};

// Perf event array
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} identity_events SEC(".maps");

// Helper to increment stats
static __always_inline void inc_stat(__u32 index) {
    __u32 key = 0;
    __u64 *val = bpf_map_lookup_elem(&identity_stats, &key);
    if (val) {
        __sync_fetch_and_add(val, 1);
    }
}

// CO-RE: Process fork hook
SEC("tp/sched/sched_process_fork")
int trace_fork(struct trace_event_raw_sched_process_fork *ctx) {
    // CO-RE: Read parent and child PIDs using BTF
    __u32 parent_pid = BPF_CORE_READ(ctx, parent_pid);
    __u32 child_pid = BPF_CORE_READ(ctx, child_pid);
    
    // Lookup parent identity
    struct identity_t *parent_id = bpf_map_lookup_elem(&pid_identity_map, &parent_pid);
    if (!parent_id) {
        return 0;  // Parent has no identity
    }
    
    // Copy parent identity to child
    struct identity_t child_id = *parent_id;
    child_id.parent_pid = parent_pid;
    
    // Update child PID mapping
    bpf_map_update_elem(&pid_identity_map, &child_pid, &child_id, BPF_ANY);
    
    // Send event to userspace
    struct identity_event event = {};
    event.pid = child_pid;
    event.parent_pid = parent_pid;
    event.event_type = 0;  // fork
    event.timestamp = bpf_ktime_get_ns();
    __builtin_memcpy(event.agent_id, parent_id->agent_id, MAX_AGENT_ID_LEN);
    
    bpf_perf_event_output(ctx, &identity_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    inc_stat(0);
    return 0;
}

// CO-RE: Process exec hook
SEC("tp/sched/sched_process_exec")
int trace_exec(struct trace_event_raw_sched_process_exec *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Check if PID has identity
    struct identity_t *id = bpf_map_lookup_elem(&pid_identity_map, &pid);
    if (!id) {
        return 0;
    }
    
    // Identity persists across exec
    struct identity_event event = {};
    event.pid = pid;
    event.parent_pid = id->parent_pid;
    event.event_type = 1;  // exec
    event.timestamp = bpf_ktime_get_ns();
    __builtin_memcpy(event.agent_id, id->agent_id, MAX_AGENT_ID_LEN);
    
    bpf_perf_event_output(ctx, &identity_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    inc_stat(1);
    return 0;
}

// CO-RE: Process exit hook
SEC("tp/sched/sched_process_exit")
int trace_exit(struct trace_event_raw_sched_process_template *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Lookup identity
    struct identity_t *id = bpf_map_lookup_elem(&pid_identity_map, &pid);
    if (!id) {
        return 0;
    }
    
    // Send exit event
    struct identity_event event = {};
    event.pid = pid;
    event.parent_pid = id->parent_pid;
    event.event_type = 2;  // exit
    event.timestamp = bpf_ktime_get_ns();
    __builtin_memcpy(event.agent_id, id->agent_id, MAX_AGENT_ID_LEN);
    
    bpf_perf_event_output(ctx, &identity_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    // Remove from map
    bpf_map_delete_elem(&pid_identity_map, &pid);
    
    inc_stat(2);
    return 0;
}

// CO-RE: TCP connect hook
SEC("kprobe/tcp_connect")
int BPF_KPROBE(kprobe_tcp_connect, struct sock *sk) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Lookup identity
    struct identity_t *id = bpf_map_lookup_elem(&pid_identity_map, &pid);
    if (!id) {
        return 0;
    }
    
    // Send lookup event
    struct identity_event event = {};
    event.pid = pid;
    event.parent_pid = id->parent_pid;
    event.event_type = 3;  // lookup
    event.timestamp = bpf_ktime_get_ns();
    __builtin_memcpy(event.agent_id, id->agent_id, MAX_AGENT_ID_LEN);
    
    bpf_perf_event_output(ctx, &identity_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
