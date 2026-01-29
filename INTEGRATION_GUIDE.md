# OCX Integration Guide - Fixing Compilation Errors

## Current Status

The socket gateway (`cmd/socket-gateway/main.go`) is working and can intercept MCP traffic at the kernel level. The integrated audit pipeline (`integrated_audit.go`) has been created but requires additional setup to compile and run.

---

## Compilation Errors Explained

### Error 1: Missing Module Imports

```
could not import github.com/ocx/backend/internal/gvisor
could not import github.com/ocx/backend/internal/escrow
```

**Cause**: The `integrated_audit.go` file references internal packages that need to be in a proper Go module structure.

**Solution**: Create a proper Go module structure:

```bash
cd /Users/483863/Documents/content-control-middleware/backend

# Initialize Go module
go mod init github.com/ocx/backend

# Add internal packages
mkdir -p internal/gvisor internal/escrow pb/jury
```

### Error 2: Undefined Global Variables

```
undefined: stateCloner
undefined: sandboxExecutor
undefined: escrowGate
```

**Cause**: These variables are referenced but not initialized in `main.go`.

**Solution**: Initialize them in `main()` or create a separate initialization function.

---

## Quick Fix: Use Simplified Version

For immediate testing, the socket gateway works without the full integrated audit. The current `main.go` will:

1. ✅ Intercept MCP traffic at socket layer
2. ✅ Log intercepted payloads
3. ✅ Provide statistics

To enable full tri-factor auditing, follow the setup below.

---

## Full Integration Setup

### Step 1: Set Up Go Module Structure

```bash
cd /Users/483863/Documents/content-control-middleware/backend

# Create module
cat > go.mod << 'EOF'
module github.com/ocx/backend

go 1.21

require (
    github.com/cilium/ebpf v0.12.3
    github.com/go-redis/redis/v8 v8.11.5
    github.com/google/uuid v1.5.0
    google.golang.org/grpc v1.60.1
)
EOF

# Download dependencies
go mod tidy
```

### Step 2: Move Internal Packages

```bash
# Ensure internal packages are in correct location
ls -la internal/gvisor/
# Should contain: sandbox_executor.go, state_cloner.go

ls -la internal/escrow/
# Should contain: gate_v2.go
```

### Step 3: Generate Protobuf Code

```bash
# Install protoc compiler
# On macOS: brew install protobuf
# On Ubuntu: apt-get install protobuf-compiler

# Install Go protobuf plugin
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate code from jury.proto
cd pb/jury
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       jury.proto
```

### Step 4: Update integrated_audit.go

The file needs to be updated to work with the module structure:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "math"
    "time"

    "github.com/ocx/backend/internal/escrow"
    "github.com/ocx/backend/internal/gvisor"
    pb "github.com/ocx/backend/pb/jury"
    "google.golang.org/grpc"
)

// Global instances (initialized in init())
var (
    sandboxExecutor *gvisor.SandboxExecutor
    stateCloner     *gvisor.StateCloner
    escrowGate      *escrow.EscrowGate
)

func init() {
    // Initialize components
    sandboxExecutor = gvisor.NewSandboxExecutor("/usr/local/bin/runsc", "/var/ocx/rootfs")
    stateCloner = gvisor.NewStateCloner("localhost:6379")
    escrowGate = escrow.NewEscrowGate()
}

// ... rest of the code
```

### Step 5: Build

```bash
cd cmd/socket-gateway

# Build socket gateway
go build -o ../../bin/socket-gateway

# If successful, you'll have:
# ../../bin/socket-gateway
```

---

## Alternative: Run Without Full Integration

The current `main.go` works standalone for Phase 1 (socket interception). To run it:

```bash
cd backend/ebpf

# Build eBPF program
make -f Makefile.socket all

# Run socket gateway (requires root)
sudo ../../bin/socket-gateway
```

This will:
- ✅ Intercept MCP traffic at kernel level
- ✅ Log payloads
- ✅ Provide statistics
- ⚠️ Not perform gVisor execution (Phase 2)
- ⚠️ Not perform Jury audit (Phase 3)
- ⚠️ Not perform Entropy monitoring (Phase 3)

---

## Phased Deployment

### Phase 1: Socket Interception (Working Now)
```bash
sudo bin/socket-gateway
```

### Phase 2: Add gVisor (Requires Setup)
1. Install gVisor: `curl -fsSL https://gvisor.dev/archive.key | ...`
2. Create rootfs: `debootstrap --variant=minbase focal /var/ocx/rootfs`
3. Update `integrated_audit.go` to use gVisor

### Phase 3: Add Jury + Entropy (Requires Services)
1. Start Jury gRPC server: `python services/jury/grpc_server.py`
2. Start Redis: `redis-server`
3. Update `integrated_audit.go` to call Jury

---

## Testing Each Phase

### Test Phase 1 (Socket Interception)
```bash
# Terminal 1: Start socket gateway
sudo bin/socket-gateway

# Terminal 2: Send test traffic
echo '{"jsonrpc":"2.0","method":"test"}' | nc localhost 8080

# Expected: See "Intercepted MCP Traffic" in Terminal 1
```

### Test Phase 2 (gVisor)
```bash
# Requires gVisor setup
# See PHASE2_GVISOR_GUIDE.md
```

### Test Phase 3 (Full Pipeline)
```bash
# Requires all services running
# See phase3_parallel_auditing_complete.md
```

---

## Summary

**Current State**: Phase 1 (socket interception) is working ✅

**To Enable Full Integration**:
1. Set up Go module structure
2. Generate protobuf code
3. Install gVisor runtime
4. Start Jury gRPC server
5. Start Redis
6. Update `integrated_audit.go` with proper initialization

**Quick Start**: Use current `main.go` for socket interception testing without full integration.
