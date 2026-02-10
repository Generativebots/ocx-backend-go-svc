package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/escrow"
)

// HandleEscrowItems lists held escrow items (§4).
func HandleEscrowItems(gate *escrow.EscrowGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items := gate.ListHeld()

		var response []map[string]interface{}
		for _, item := range items {
			response = append(response, map[string]interface{}{
				"id":         item.ID,
				"agent_id":   item.AgentID,
				"tenant_id":  item.TenantID,
				"status":     "HELD",
				"created_at": item.CreatedAt,
			})
		}

		if response == nil {
			response = make([]map[string]interface{}, 0)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// HandleEscrowRelease releases or rejects an escrow item (§4).
func HandleEscrowRelease(gate *escrow.EscrowGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			EscrowID string `json:"escrow_id"`
			Decision string `json:"decision"` // APPROVED / REJECTED
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		approved := req.Decision == "APPROVED"
		payload, err := gate.ProcessSignal(req.EscrowID, "APIOverride", approved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "processed",
			"released": payload != nil,
		})
	}
}

// HandleActiveEntitlements lists active JIT entitlements (§4.3).
func HandleActiveEntitlements(jit *escrow.JITEntitlementManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := r.URL.Query().Get("agent_id")

		var response interface{}

		if agentID != "" {
			entitlements := jit.GetActiveEntitlements(agentID)
			response = map[string]interface{}{
				"agent_id":     agentID,
				"entitlements": entitlements,
				"count":        len(entitlements),
			}
		} else {
			response = map[string]interface{}{
				"total_held": jit.GetAllHeldCount(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// HandleAgentEntitlements lists active entitlements for a specific agent.
func HandleAgentEntitlements(jit *escrow.JITEntitlementManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := mux.Vars(r)["agentId"]
		entitlements := jit.GetActiveEntitlements(agentID)

		type entitlementView struct {
			ID         string `json:"id"`
			Permission string `json:"permission"`
			TTL        string `json:"ttl"`
			Remaining  string `json:"remaining"`
			GrantedBy  string `json:"granted_by"`
			Reason     string `json:"reason"`
			Status     string `json:"status"`
		}

		var views []entitlementView
		for _, ent := range entitlements {
			remaining := jit.RemainingTTL(agentID, ent.Permission)
			views = append(views, entitlementView{
				ID:         ent.ID,
				Permission: ent.Permission,
				TTL:        ent.TTL.String(),
				Remaining:  remaining.String(),
				GrantedBy:  ent.GrantedBy,
				Reason:     ent.Reason,
				Status:     string(ent.Status),
			})
		}

		if views == nil {
			views = []entitlementView{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_id":     agentID,
			"entitlements": views,
			"total":        len(views),
			"all_active":   jit.GetAllHeldCount(),
		})
	}
}

// HandleRevokeEntitlement revokes a specific entitlement.
func HandleRevokeEntitlement(jit *escrow.JITEntitlementManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["agentId"]
		permission := vars["permission"]

		err := jit.RevokeEntitlement(agentID, permission, "API revocation")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_id":   agentID,
			"permission": permission,
			"status":     "REVOKED",
		})
	}
}

// HandleMicropaymentStatus returns micropayment escrow status (§4.2).
func HandleMicropaymentStatus(mp *escrow.MicropaymentEscrow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		heldFunds := mp.GetHeldFunds()

		var totalHeld float64
		var funds []map[string]interface{}
		for _, f := range heldFunds {
			totalHeld += f.Amount
			funds = append(funds, map[string]interface{}{
				"escrow_id": f.ID,
				"tenant_id": f.TenantID,
				"agent_id":  f.AgentID,
				"tool_id":   f.ToolID,
				"amount":    f.Amount,
				"status":    f.Status,
				"held_at":   f.HeldAt,
			})
		}

		if funds == nil {
			funds = make([]map[string]interface{}, 0)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_held_amount": totalHeld,
			"held_count":        len(heldFunds),
			"funds":             funds,
		})
	}
}

// HandleCompensationPending shows pending compensations (§9).
func HandleCompensationPending(cs *escrow.CompensationStack) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pending := cs.GetPending()
		if pending == nil {
			pending = make(map[string]int)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"pending_transactions":  pending,
			"total_pending":         cs.TotalPending(),
			"total_transaction_ids": len(pending),
		})
	}
}

// Ensure time is used (for entitlement TTL in other handlers)
var _ = time.Now
