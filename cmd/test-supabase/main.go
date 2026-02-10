package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/ocx/backend/internal/database"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, using environment variables")
	}

	// Check env vars
	fmt.Println("SUPABASE_URL:", os.Getenv("SUPABASE_URL"))
	fmt.Println("SUPABASE_SERVICE_KEY:", os.Getenv("SUPABASE_SERVICE_KEY")[:20]+"...")

	// Create client
	client, err := database.NewSupabaseClient()
	if err != nil {
		log.Fatalf("Failed to create Supabase client: %v", err)
	}

	fmt.Println("\n✅ Supabase client created successfully!")

	// Test: Get tenant
	ctx := context.Background()
	tenant, err := client.GetTenant(ctx, "acme-corp")
	if err != nil {
		log.Fatalf("Failed to get tenant: %v", err)
	}

	if tenant != nil {
		fmt.Printf("\n✅ Retrieved tenant: %s (%s)\n", tenant.TenantName, tenant.SubscriptionTier)
	} else {
		fmt.Println("\n⚠️  Tenant 'acme-corp' not found")
	}

	fmt.Println("\n✅ Go backend ↔ Supabase connection verified!")
}
