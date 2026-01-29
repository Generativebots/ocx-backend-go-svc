package middleware

import (
	"net/http"
	"strings"

	"github.com/ocx/backend/internal/multitenancy"
)

// TenantMiddleware ensures a valid tenant context exists for the request
func TenantMiddleware(tm *multitenancy.TenantManager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var tenantID string

		// 1. Check Authorization Header (API Key)
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ocx_") {
			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			tenant, err := tm.ValidateAPIKey(ctx, apiKey)
			if err != nil {
				http.Error(w, "Invalid API Key", http.StatusUnauthorized)
				return
			}
			tenantID = tenant.TenantID
		}

		// 2. Check X-Tenant-ID Header (Trusted/Internal/Dev)
		// This acts as a fallback or override if no API key is present,
		// but should ideally be behind a firewall or gateway in production.
		if tenantID == "" {
			reqTenantID := r.Header.Get("X-Tenant-ID")
			if reqTenantID != "" {
				// Validate existence
				tenant, err := tm.LoadTenant(ctx, reqTenantID)
				if err != nil {
					http.Error(w, "Invalid Tenant ID", http.StatusUnauthorized)
					return
				}
				tenantID = tenant.TenantID
			}
		}

		// 3. Enforce Tenant Context
		if tenantID == "" {
			http.Error(w, "Missing Tenant Context (API Key or X-Tenant-ID)", http.StatusUnauthorized)
			return
		}

		// 4. Inject into Context
		ctx = multitenancy.WithTenant(ctx, tenantID)
		next(w, r.WithContext(ctx))
	}
}
