// eBPF CO-RE TLS Interception
// Uses BTF and libbpf for kernel portability (5.10+)

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_DATA_SIZE 4096
#define MAX_ENTRIES 10240

// Event structure for plaintext data
struct tls_event {
    __u32 pid;
    __u32 tid;
    __u64 timestamp;
    __u32 data_len;
    __u8 direction;  // 0 = write (outbound), 1 = read (inbound)
    __u8 library;    // 0 = OpenSSL, 1 = BoringSSL, 2 = Go
    char data[MAX_DATA_SIZE];
    char comm[16];
};

// Perf event array for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} tls_events SEC(".maps");

// Temporary storage for SSL pointers
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u64);  // tid
    __type(value, __u64); // buffer pointer
} ssl_buffers SEC(".maps");

// Helper to get current task info
static __always_inline void fill_task_info(struct tls_event *event) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    event->pid = pid_tgid >> 32;
    event->tid = (__u32)pid_tgid;
    event->timestamp = bpf_ktime_get_ns();
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
}

// OpenSSL: SSL_write (outbound) - CO-RE version
SEC("uprobe/SSL_write")
int BPF_KPROBE(uprobe_ssl_write, void *ssl, const void *buf, int num) {
    struct tls_event event = {};
    
    fill_task_info(&event);
    event.direction = 0;  // write = outbound
    event.library = 0;    // OpenSSL
    
    // Limit data size
    int len = num;
    if (len > MAX_DATA_SIZE) {
        len = MAX_DATA_SIZE;
    }
    event.data_len = len;
    
    // CO-RE: Read plaintext data from userspace buffer
    // This works across different kernel versions
    if (bpf_probe_read_user(event.data, len & (MAX_DATA_SIZE - 1), buf) != 0) {
        return 0;  // Failed to read
    }
    
    // Send event to userspace
    bpf_perf_event_output(ctx, &tls_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    return 0;
}

// OpenSSL: SSL_read (inbound) - CO-RE version
SEC("uretprobe/SSL_read")
int BPF_KRETPROBE(uretprobe_ssl_read, int ret) {
    if (ret <= 0) {
        return 0;  // No data read
    }
    
    __u64 tid = bpf_get_current_pid_tgid();
    __u64 *buf_ptr = bpf_map_lookup_elem(&ssl_buffers, &tid);
    if (!buf_ptr) {
        return 0;
    }
    
    struct tls_event event = {};
    fill_task_info(&event);
    event.direction = 1;  // read = inbound
    event.library = 0;    // OpenSSL
    
    // Limit data size
    int len = ret;
    if (len > MAX_DATA_SIZE) {
        len = MAX_DATA_SIZE;
    }
    event.data_len = len;
    
    // CO-RE: Read plaintext data
    if (bpf_probe_read_user(event.data, len & (MAX_DATA_SIZE - 1), (void *)*buf_ptr) != 0) {
        bpf_map_delete_elem(&ssl_buffers, &tid);
        return 0;
    }
    
    // Send event
    bpf_perf_event_output(ctx, &tls_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    // Cleanup
    bpf_map_delete_elem(&ssl_buffers, &tid);
    
    return 0;
}

// OpenSSL: SSL_read entry (store buffer pointer)
SEC("uprobe/SSL_read")
int BPF_KPROBE(uprobe_ssl_read, void *ssl, void *buf, int num) {
    __u64 tid = bpf_get_current_pid_tgid();
    __u64 buf_addr = (__u64)buf;
    bpf_map_update_elem(&ssl_buffers, &tid, &buf_addr, BPF_ANY);
    return 0;
}

// BoringSSL: Same hooks (compatible with OpenSSL API)
SEC("uprobe/SSL_write_boring")
int BPF_KPROBE(uprobe_ssl_write_boring, void *ssl, const void *buf, int num) {
    struct tls_event event = {};
    
    fill_task_info(&event);
    event.direction = 0;
    event.library = 1;  // BoringSSL
    
    int len = num;
    if (len > MAX_DATA_SIZE) {
        len = MAX_DATA_SIZE;
    }
    event.data_len = len;
    
    if (bpf_probe_read_user(event.data, len & (MAX_DATA_SIZE - 1), buf) != 0) {
        return 0;
    }
    
    bpf_perf_event_output(ctx, &tls_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    return 0;
}

// Go crypto/tls: (*Conn).Write - CO-RE version
SEC("uprobe/go_tls_write")
int BPF_KPROBE(uprobe_go_tls_write) {
    struct tls_event event = {};
    
    fill_task_info(&event);
    event.direction = 0;
    event.library = 2;  // Go
    
    // CO-RE: Extract data from Go struct
    // Note: Go uses different calling conventions
    void *buf_ptr = (void *)PT_REGS_PARM2(ctx);
    __u64 len = PT_REGS_PARM3(ctx);
    
    if (len > MAX_DATA_SIZE) {
        len = MAX_DATA_SIZE;
    }
    event.data_len = len;
    
    if (bpf_probe_read_user(event.data, len & (MAX_DATA_SIZE - 1), buf_ptr) != 0) {
        return 0;
    }
    
    bpf_perf_event_output(ctx, &tls_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
