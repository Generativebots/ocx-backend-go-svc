# ocx-backend-go-svc

Go backend for the OCX Autonomous Operational Control System (AOCS).

## Architecture

| Layer | Description |
|-------|-------------|
| `cmd/api` | HTTP API server entry point |
| `cmd/socket-gateway` | eBPF socket gateway entry point |
| `internal/escrow` | Tri-Factor Gate & Escrow Barrier |
| `internal/fabric` | O(n) Hub-and-Spoke routing engine |
| `internal/federation` | Inter-OCX Ed25519 handshake protocol |
| `internal/evidence` | Hash-chained Evidence Vault |
| `internal/protocol` | 110-byte protocol frames |
| `internal/marketplace` | Marketplace service (connectors, templates, billing) |
| `ebpf/` | eBPF kernel-level interceptor programs |
| `db/` | SQL schema files (master + marketplace) |

## Prerequisites

- Go 1.23+
- PostgreSQL (Supabase)

## Quick Start

```bash
# Install dependencies
go mod download

# Run the API server
go run ./cmd/api

# Build binary
go build -o ocx-api ./cmd/api
```

## Docker

```bash
docker build -t ocx-backend .
docker run -p 8080:8080 --env-file .env ocx-backend
```

## Environment

Copy `.env.example` to `.env` and configure:

```
SUPABASE_URL=https://xxx.supabase.co
SUPABASE_SERVICE_KEY=eyJ...
PORT=8080
```
