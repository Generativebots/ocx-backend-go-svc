package multitenancy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ocx/backend/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================================
// MULTI-TENANT SUPPORT - Persistent & Scalable
// ============================================================================

// TenantManager manages multi-tenant organizations and permissions via Database
type TenantManager struct {
	db *database.SupabaseClient
}

// NewTenantManager creates a new persistent tenant manager
func NewTenantManager(db *database.SupabaseClient) *TenantManager {
	return &TenantManager{
		db: db,
	}
}

// ============================================================================
// TENANT OPERATIONS
// ============================================================================

// GetTenant retrieves a tenant by ID
func (tm *TenantManager) GetTenant(ctx context.Context, tenantID string) (*database.Tenant, error) {
	return tm.db.GetTenant(ctx, tenantID)
}

// LoadTenant validates and loads a tenant, ensuring it is active
func (tm *TenantManager) LoadTenant(ctx context.Context, tenantID string) (*database.Tenant, error) {
	tenant, err := tm.db.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, errors.New("tenant not found")
	}

	if tenant.Status != "ACTIVE" && tenant.Status != "TRIAL" {
		return nil, fmt.Errorf("tenant is %s", tenant.Status)
	}

	return tenant, nil
}

// ============================================================================
// API KEY MANAGEMENT
// ============================================================================

// CreateAPIKey creates a new API key with format: ocx_<id>.<secret>
func (tm *TenantManager) CreateAPIKey(ctx context.Context, tenantID, name string, scopes []string) (*database.APIKey, string, error) {
	// Generate Key ID (public)
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	keyID := hex.EncodeToString(idBytes) // 16 chars

	// Generate Secret (private)
	secretBytes := make([]byte, 24)
	rand.Read(secretBytes)
	secret := hex.EncodeToString(secretBytes) // 48 chars

	// Full Key returned to user
	fullKey := fmt.Sprintf("ocx_%s.%s", keyID, secret)

	// Hash Secret
	// We hash ONLY the secret part. The ID is used for lookup.
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}

	apiKey := &database.APIKey{
		KeyID:    keyID,
		TenantID: tenantID,
		Name:     name,
		KeyHash:  string(secretHash), // Store hash of secret
		Scopes:   scopes,
		IsActive: true,
	}

	// Persist
	err = tm.db.CreateAPIKey(ctx, apiKey)
	if err != nil {
		return nil, "", err
	}

	return apiKey, fullKey, nil
}

// ValidateAPIKey validates an API key and returns the Tenant
// Key Format: ocx_<key_id>.<secret>
func (tm *TenantManager) ValidateAPIKey(ctx context.Context, fullKey string) (*database.Tenant, error) {
	// Parse Key
	if !strings.HasPrefix(fullKey, "ocx_") {
		return nil, errors.New("invalid key format")
	}
	parts := strings.Split(strings.TrimPrefix(fullKey, "ocx_"), ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid key format")
	}

	keyID := parts[0]
	secret := parts[1]

	// Lookup by KeyID
	apiKey, err := tm.db.GetAPIKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("lookup failed: %w", err)
	}
	if apiKey == nil {
		return nil, errors.New("invalid api key")
	}

	// Validate Secret
	if err := bcrypt.CompareHashAndPassword([]byte(apiKey.KeyHash), []byte(secret)); err != nil {
		return nil, errors.New("invalid api key secret")
	}

	// Check Active & Expiry
	if !apiKey.IsActive {
		return nil, errors.New("api key inactive")
	}
	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, errors.New("api key expired")
	}

	// Load Tenant
	return tm.LoadTenant(ctx, apiKey.TenantID)
}

// ============================================================================
// CONTEXT HELPERS
// ============================================================================

type contextKey string

const (
	tenantIDKey contextKey = "tenant_id"
	tenantKey   contextKey = "tenant"
)

// WithTenant adds tenant ID to context
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// GetTenantID extracts tenant ID from context
func GetTenantID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(tenantIDKey).(string)
	if !ok || id == "" {
		return "", errors.New("tenant context missing")
	}
	return id, nil
}
