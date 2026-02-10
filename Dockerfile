# Stage 1: Builder
FROM golang:1.23-bullseye as builder

WORKDIR /app

# Copy Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy Source Code
COPY . .

# Build the API server binary
RUN CGO_ENABLED=0 GOOS=linux go build -o ocx-api ./cmd/api

# Stage 2: Runner (Distroless / Minimal Debian)
FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /root/

COPY --from=builder /app/ocx-api .
COPY --from=builder /app/config.yaml .
COPY --from=builder /app/tenants.yaml .

# Cloud Run injects PORT=8080 by default; our config reads $PORT
ENV PORT=8080

# Expose ports (HTTP + gRPC)
EXPOSE 8080 50051

# Run the API server
CMD ["./ocx-api"]
