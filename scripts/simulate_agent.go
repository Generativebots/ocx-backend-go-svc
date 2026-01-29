package main

import (
	"fmt"
	"log"
	"github.com/ocx/backend/pkg/trust"
	"time"
)

func main() {
	client := trust.NewClient(trust.Config{
		ExchangeURL: "http://localhost:8080",
		AgentID:     "agent-procurement-01",
		AgentName:   "Procurement Agent",
	})

	fmt.Println("🤖 Agent Starting: Procurement Agent")

	// 1. Simulate Check-in
	// For now check-in is a no-op / placeholder in client, but let's say we do it.
	fmt.Println("📡 Connecting to OCX Trust Exchange...")
	time.Sleep(1 * time.Second)
	fmt.Println("✅ Identity Verified by OCX.")

	// 2. Simulate Intent
	action := "BUY_GPU_CLUSTER"
	payload := map[string]interface{}{
		"units":  500,
		"vendor": "NVIDIA",
		"amount": 2500000,
	}

	fmt.Printf("\n🤔 Intent Formed: %s (Value: $2.5M)\n", action)
	fmt.Println("⏳ Requesting Trust Token from OCX Jury...")

	token, err := client.VerifyIntent(action, payload)
	if err != nil {
		log.Fatalf("❌ OCX BLOCKED Transaction: %v", err)
	}

	fmt.Printf("\n🎟️  TRUST TOKEN RECEIVED!\n")
	fmt.Printf("Token: %s...\n", token[:20]) // truncated
	fmt.Println("✅ Proceeding with Transaction Execution...")
}
