package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/catalog"
	"github.com/ocx/backend/internal/events"
	"github.com/ocx/backend/internal/webhooks"
	"github.com/ocx/backend/pkg/plugins"
)

// HandleListTools lists all registered tools.
func HandleListTools(tc *catalog.ToolCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tools := tc.List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tools": tools,
			"count": len(tools),
		})
	}
}

// HandleRegisterTool registers a new tool in the catalog.
func HandleRegisterTool(tc *catalog.ToolCatalog, bus events.EventEmitter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tool catalog.ToolDefinition
		if err := json.NewDecoder(r.Body).Decode(&tool); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := tc.Register(&tool); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		bus.Emit("ocx.tool.registered", "/api/v1/tools", tool.Name, map[string]interface{}{
			"tool_name":    tool.Name,
			"action_class": string(tool.ActionClass),
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(tool)
	}
}

// HandleGetTool returns a single tool by name.
func HandleGetTool(tc *catalog.ToolCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["toolName"]
		tool, ok := tc.Get(name)
		if !ok {
			http.Error(w, `{"error":"tool not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tool)
	}
}

// HandleDeleteTool removes a tool from the catalog.
func HandleDeleteTool(tc *catalog.ToolCatalog, bus events.EventEmitter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["toolName"]
		if err := tc.Delete(name); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}

		bus.Emit("ocx.tool.removed", "/api/v1/tools", name, map[string]interface{}{
			"tool_name": name,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleListWebhooks lists all registered webhooks.
func HandleListWebhooks(reg *webhooks.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hooks := reg.ListAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"webhooks": hooks,
			"count":    len(hooks),
		})
	}
}

// HandleRegisterWebhook registers a new webhook.
func HandleRegisterWebhook(reg *webhooks.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sub webhooks.WebhookSubscription
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		tenantID := r.Header.Get("X-Tenant-ID")
		sub.TenantID = tenantID

		if err := reg.Register(&sub); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(sub)
	}
}

// HandleDeleteWebhook deletes a webhook by ID.
func HandleDeleteWebhook(reg *webhooks.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["webhookId"]
		if err := reg.Unregister(id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleListPlugins lists all registered plugins.
func HandleListPlugins(reg *plugins.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pluginList := reg.List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"plugins": pluginList,
			"count":   len(pluginList),
		})
	}
}
