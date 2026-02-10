package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	gateway := os.Getenv("OCX_GATEWAY_URL")
	if gateway == "" {
		gateway = "http://localhost:8080"
	}

	apiKey := os.Getenv("OCX_API_KEY")
	tenantID := os.Getenv("OCX_TENANT_ID")
	if tenantID == "" {
		tenantID = "default"
	}

	switch os.Args[1] {
	case "govern":
		cmdGovern(gateway, apiKey, tenantID)
	case "trust":
		cmdTrust(gateway, apiKey)
	case "tools":
		cmdTools(gateway, apiKey)
	case "plugins":
		cmdPlugins(gateway, apiKey)
	case "version":
		fmt.Printf("ocx-cli v%s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`OCX Governance CLI v` + version + `

Usage: ocx <command> [flags]

Commands:
  govern    Govern an AI tool call
  trust     Get agent trust score
  tools     List/register/remove tools
  plugins   List connector plugins
  version   Print version
  help      Show this help

Environment:
  OCX_GATEWAY_URL   Gateway URL (default: http://localhost:8080)
  OCX_API_KEY       API key for authentication
  OCX_TENANT_ID     Tenant ID (default: "default")

Examples:
  ocx govern --tool execute_payment --args '{"amount":100}'
  ocx trust --agent agent-123
  ocx tools list
  ocx tools register --name my_tool --class CLASS_B --min-trust 0.8`)
}

// ----------------------------------------------------------------
// govern command
// ----------------------------------------------------------------

func cmdGovern(gateway, apiKey, tenantID string) {
	var toolName, argsJSON, agentID, model string

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tool", "-t":
			i++
			if i < len(args) {
				toolName = args[i]
			}
		case "--args", "-a":
			i++
			if i < len(args) {
				argsJSON = args[i]
			}
		case "--agent":
			i++
			if i < len(args) {
				agentID = args[i]
			}
		case "--model":
			i++
			if i < len(args) {
				model = args[i]
			}
		}
	}

	if toolName == "" {
		fmt.Fprintln(os.Stderr, "Error: --tool is required")
		os.Exit(1)
	}
	if agentID == "" {
		agentID = fmt.Sprintf("cli-%d", time.Now().UnixNano()%10000)
	}

	var parsedArgs map[string]interface{}
	if argsJSON != "" {
		json.Unmarshal([]byte(argsJSON), &parsedArgs)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"tool_name": toolName,
		"agent_id":  agentID,
		"tenant_id": tenantID,
		"arguments": parsedArgs,
		"model":     model,
		"protocol":  "ocx-cli",
	})

	resp, err := doRequest("POST", gateway+"/api/v1/govern", body, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	json.Unmarshal(resp, &result)

	verdict := result["verdict"]
	switch verdict {
	case "ALLOW":
		fmt.Printf("✅ ALLOW | trust=%.2f | tax=$%.4f | tx=%s\n",
			toFloat(result["trust_score"]), toFloat(result["governance_tax"]), result["transaction_id"])
	case "BLOCK":
		fmt.Printf("⛔ BLOCK | reason=%s | tx=%s\n", result["reason"], result["transaction_id"])
	case "ESCROW":
		fmt.Printf("⏳ ESCROW | escrow_id=%s | tx=%s\n", result["escrow_id"], result["transaction_id"])
	default:
		fmt.Printf("🔄 %s | tx=%s\n", verdict, result["transaction_id"])
	}
}

// ----------------------------------------------------------------
// trust command
// ----------------------------------------------------------------

func cmdTrust(gateway, apiKey string) {
	if len(os.Args) < 4 || os.Args[2] != "--agent" {
		fmt.Fprintln(os.Stderr, "Usage: ocx trust --agent <agent-id>")
		os.Exit(1)
	}
	agentID := os.Args[3]

	resp, err := doRequest("GET", gateway+"/api/reputation/"+agentID, nil, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	json.Unmarshal(resp, &result)

	fmt.Printf("Agent:  %s\nScore:  %.2f\nTier:   %s\n",
		agentID, toFloat(result["score"]), result["tier"])
}

// ----------------------------------------------------------------
// tools command
// ----------------------------------------------------------------

func cmdTools(gateway, apiKey string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: ocx tools <list|register|remove>")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "list":
		resp, err := doRequest("GET", gateway+"/api/v1/tools", nil, apiKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
			os.Exit(1)
		}
		var result map[string]interface{}
		json.Unmarshal(resp, &result)

		tools, ok := result["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			fmt.Println("No tools registered.")
			return
		}

		fmt.Printf("%-25s %-10s %s\n", "TOOL", "CLASS", "MIN TRUST")
		fmt.Println("--------------------------------------------------")
		for _, t := range tools {
			tool := t.(map[string]interface{})
			policy, _ := tool["governance_policy"].(map[string]interface{})
			minTrust := 0.0
			if policy != nil {
				minTrust = toFloat(policy["min_trust_score"])
			}
			fmt.Printf("%-25s %-10s %.2f\n",
				tool["name"], tool["action_class"], minTrust)
		}

	case "register":
		var name, class string
		var minTrust float64 = 0.5
		args := os.Args[3:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--name":
				i++
				if i < len(args) {
					name = args[i]
				}
			case "--class":
				i++
				if i < len(args) {
					class = args[i]
				}
			case "--min-trust":
				i++
				if i < len(args) {
					fmt.Sscanf(args[i], "%f", &minTrust)
				}
			}
		}
		if name == "" || class == "" {
			fmt.Fprintln(os.Stderr, "Usage: ocx tools register --name <name> --class <CLASS_A|CLASS_B> [--min-trust 0.5]")
			os.Exit(1)
		}
		body, _ := json.Marshal(map[string]interface{}{
			"name":         name,
			"action_class": class,
			"governance_policy": map[string]interface{}{
				"min_trust_score": minTrust,
			},
		})
		_, err := doRequest("POST", gateway+"/api/v1/tools", body, apiKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Registered tool: %s (%s, min_trust=%.2f)\n", name, class, minTrust)

	case "remove":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: ocx tools remove <tool-name>")
			os.Exit(1)
		}
		name := os.Args[3]
		_, err := doRequest("DELETE", gateway+"/api/v1/tools/"+name, nil, apiKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("🗑️  Removed tool: %s\n", name)
	}
}

// ----------------------------------------------------------------
// plugins command
// ----------------------------------------------------------------

func cmdPlugins(gateway, apiKey string) {
	resp, err := doRequest("GET", gateway+"/api/v1/plugins", nil, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	json.Unmarshal(resp, &result)

	plugins, ok := result["plugins"].([]interface{})
	if !ok || len(plugins) == 0 {
		fmt.Println("No plugins registered.")
		return
	}

	fmt.Printf("%-20s %-10s %-10s %s\n", "PLUGIN", "VERSION", "PRIORITY", "PROTOCOLS")
	fmt.Println("----------------------------------------------------------")
	for _, p := range plugins {
		plugin := p.(map[string]interface{})
		fmt.Printf("%-20s %-10s %-10.0f %v\n",
			plugin["name"], plugin["version"], toFloat(plugin["priority"]), plugin["protocols"])
	}
}

// ----------------------------------------------------------------
// helpers
// ----------------------------------------------------------------

func doRequest(method, url string, body []byte, apiKey string) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func toFloat(v interface{}) float64 {
	switch f := v.(type) {
	case float64:
		return f
	case int:
		return float64(f)
	default:
		return 0
	}
}
