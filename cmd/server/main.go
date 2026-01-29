package main

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq" // Postgres driver

	"github.com/ocx/backend/internal/api"
	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/reputation"
)

func main() {
	log.Println("🔥 Starting Content Control Middleware Backend (v2.0)...")

	// 1. Initialize Microservices

	// Pool Manager (gVisor)
	pool := ghostpool.NewPoolManager(5, 20, "ghost-sandbox:latest")

	// Escrow Gate
	gate := escrow.NewEscrowGate(
		escrow.NewMockJuryClient(),
		escrow.NewMockEntropyMonitor(),
	)

	// Reputation Wallet (Mock DB for now)
	var db *sql.DB // connect to Spanner/Postgres here
	wallet := reputation.NewReputationWallet(db)

	// 2. Start API Gateway (REST -> gRPC/Internal)
	server := api.NewAPIServer(pool, gate, wallet)

	// Listen on Port 8080
	if err := server.Start(8080); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
