# Stage 1: Builder (Go + eBPF Toolchain)
FROM golang:1.23-bullseye as builder

# Install eBPF dependencies
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    gcc-multilib \
    linux-headers-generic \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy Source Code
COPY . .

# Build the binary
# Note: In a real eBPF setup, we would run `go generate` here to trigger bpf2go
RUN go build -o ocx-probe ./cmd/probe

# Stage 2: Runner (Distroless / Minimal Debian)
FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /root/

COPY --from=builder /app/ocx-probe .

# Expose ports
EXPOSE 8080 50051

# Run
CMD ["./ocx-probe"]
