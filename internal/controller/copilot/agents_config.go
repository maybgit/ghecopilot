package copilot

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// AgentDef represents a single tool/agent definition for GitHub Copilot.
type AgentDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	URL         string                 `json:"url"`
	Definition  map[string]interface{} `json:"definition"`
}

var (
	agentsOnce sync.Once
	agentsList []*AgentDef
)

// getDefaultAgents returns the built-in default tools when AGENT_TOOLS env is empty.
func getDefaultAgents() []*AgentDef {
	return []*AgentDef{
		{
			Name:        "file_search",
			Description: "Search for files in the workspace.",
			URL:         "/agents/file_search",
			Definition: map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "file_search",
					"description": "Search for files in the workspace that match the given query. Returns file paths and matching line previews.",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "The search query to find matching files.",
							},
						},
						"required": []string{"query"},
					},
				},
			},
		},
	}
}

// getAgents lazily loads agent definitions from the AGENT_TOOLS environment variable.
// Falls back to built-in default agents if the env var is empty or invalid.
func getAgents() []*AgentDef {
	agentsOnce.Do(func() {
		raw := os.Getenv("AGENT_TOOLS")
		if raw == "" || raw == "[]" {
			agentsList = getDefaultAgents()
			log.Println("[agents] using built-in default tools")
			return
		}

		var defs []AgentDef
		if err := json.Unmarshal([]byte(raw), &defs); err != nil {
			log.Printf("[agents] failed to parse AGENT_TOOLS: %v, falling back to defaults", err)
			agentsList = getDefaultAgents()
			return
		}

		for i := range defs {
			if defs[i].Name == "" {
				continue
			}
			if defs[i].URL == "" {
				defs[i].URL = "/agents/" + defs[i].Name
			}
			if defs[i].Definition == nil {
				log.Printf("[agents] skipping agent %q: missing definition", defs[i].Name)
				continue
			}
			agentsList = append(agentsList, &defs[i])
		}

		if len(agentsList) == 0 {
			agentsList = getDefaultAgents()
			log.Println("[agents] no valid custom tools parsed, falling back to defaults")
		} else {
			log.Printf("[agents] loaded %d custom tool(s) from AGENT_TOOLS", len(agentsList))
		}
	})

	return agentsList
}

// findAgentByName looks up an agent by its name. Returns nil if not found.
func findAgentByName(name string) *AgentDef {
	for _, a := range getAgents() {
		if a.Name == name {
			return a
		}
	}
	return nil
}
