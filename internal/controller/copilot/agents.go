package copilot

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

// GetAgents lists all available tools/agents for Copilot.
func GetAgents(c *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	c.Header("x-github-request-id", requestID)

	agents := make([]gin.H, 0, len(getAgents()))
	for _, a := range getAgents() {
		agents = append(agents, gin.H{
			"name":        a.Name,
			"url":         a.URL,
			"definition":  a.Definition,
			"description": a.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// GetAgentDefinition returns the definition for a single tool/agent.
func GetAgentDefinition(c *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	c.Header("x-github-request-id", requestID)

	name := c.Param("name")
	agent := findAgentByName(name)
	if agent == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found: " + name})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":        agent.Name,
		"url":         agent.URL,
		"definition":  agent.Definition,
		"description": agent.Description,
	})
}
