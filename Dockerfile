# Stage 1: Builder
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Copy Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy Source Code
COPY . .

# Build the API server binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o ocx-api ./cmd/api

# Stage 2: Runner (Minimal Debian)
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /root/

COPY --from=builder /app/ocx-api .
COPY --from=builder /app/config.yaml .
COPY --from=builder /app/tenants.yaml .

# Cloud Run injects PORT=8080 by default
ENV PORT=8080

EXPOSE 8080

CMD ["./ocx-api"]
