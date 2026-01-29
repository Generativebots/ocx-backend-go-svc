//go:build ignore
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>

char __license[] SEC("license") = "Dual MIT/GPL";

// Tenant Map: PID -> TenantID (u32 hash)
// Populated by specific userspace loader (PoolManager)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u32); // TenantID Hash
} tenant_map SEC(".maps");

SEC("socket")
int socket_filter(struct __sk_buff *skb) {
    // 1. Parse Ethernet Header
    // Note: In socket filters, we might be getting raw packets or cooked
    // Assuming raw for this sophisticated interceptor
    
    // We only care about TCP port 8080 (Tool Protocol)
    // Simplified parsing logic for this restricted environment
    
    // 2. Prepare Event
    struct event_t *e;
    e = bpf_ringbuf_reserve(&events, sizeof(struct event_t), 0);
    if (!e) {
        return 0; // Ring buffer full
    }

    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id; // PID in userspace is TGID in kernel

    e->pid = pid;
    e->uid = bpf_get_current_uid_gid();
    e->len = skb->len;
    
    // Capture Payload (Truncated)
    // bpf_skb_load_bytes handles bounds checking
    bpf_skb_load_bytes(skb, 0, e->payload, sizeof(e->payload));

    // Multi-tenancy: Lookup Tenant ID from Map (populated by PoolManager)
    __u32 *tenant_id = bpf_map_lookup_elem(&tenant_map, &pid);
    if (tenant_id) {
        e->tenant_id_hash = *tenant_id;
    } else {
        // Fallback: Use UID or specific header if visible
        e->tenant_id_hash = e->uid; 
    }

    // 3. Submit to RingBuffer
    bpf_ringbuf_submit(e, 0);

    return skb->len; // Pass packet (Passive Tap)
}
