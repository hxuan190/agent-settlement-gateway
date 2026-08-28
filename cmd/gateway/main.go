// Command gateway is an MCP server that lets an AI agent request a Solana
// swap through Jupiter, subject to a per-agent spend limit, and get back a
// signed proof of what was decided. It never holds or uses a wallet key.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-settlement-gateway/internal/guardrail"
	"agent-settlement-gateway/internal/identity"
	"agent-settlement-gateway/internal/jupiter"
	"agent-settlement-gateway/internal/proof"
)

func main() {
	configPath := os.Getenv("GUARDRAIL_CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}
	limiter, err := guardrail.Load(configPath)
	if err != nil {
		log.Fatalf("load guardrail config: %v", err)
	}

	identityPath := os.Getenv("IDENTITY_CONFIG")
	if identityPath == "" {
		identityPath = "identity.json"
	}
	registry, err := identity.Load(identityPath)
	if err != nil {
		log.Fatalf("load identity registry: %v", err)
	}

	keyPath := os.Getenv("GATEWAY_KEY_PATH")
	if keyPath == "" {
		keyPath = "gateway_key.json"
	}
	signer, err := proof.LoadOrCreateSigner(keyPath)
	if err != nil {
		log.Fatalf("load signer: %v", err)
	}

	jup := jupiter.NewClient()

	server := mcp.NewServer(&mcp.Implementation{Name: "agent-settlement-gateway", Version: "0.1.0"}, nil)
	registerJupiterQuoteTool(server, jup)
	registerPrepareSwapTool(server, jup, registry, limiter, signer)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
