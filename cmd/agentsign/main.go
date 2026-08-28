// Command agentsign signs a swap request the same way a real agent would,
// for exercising prepare_agent_swap's identity check without wiring up a
// full agent.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"time"

	"agent-settlement-gateway/internal/identity"
)

func main() {
	agentID := flag.String("agent", "", "agent id (must be registered in identity.json)")
	inputMint := flag.String("input-mint", "", "source token mint")
	outputMint := flag.String("output-mint", "", "destination token mint")
	amount := flag.String("amount", "", "amount in the input token's base units")
	privKeyB64 := flag.String("private-key", "", "agent's base64 Ed25519 private key, from agentkey")
	flag.Parse()

	if *agentID == "" || *privKeyB64 == "" || *inputMint == "" || *outputMint == "" || *amount == "" {
		fmt.Fprintln(os.Stderr, "usage: agentsign -agent ID -private-key KEY -input-mint MINT -output-mint MINT -amount N")
		os.Exit(1)
	}

	priv, err := base64.StdEncoding.DecodeString(*privKeyB64)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		fmt.Fprintln(os.Stderr, "invalid -private-key")
		os.Exit(1)
	}

	timestamp := time.Now().Unix()
	payload := identity.CanonicalPayload(*agentID, *inputMint, *outputMint, *amount, timestamp)
	sig := ed25519.Sign(ed25519.PrivateKey(priv), payload)

	fmt.Printf("timestamp: %d\n", timestamp)
	fmt.Printf("signature: %s\n", base64.StdEncoding.EncodeToString(sig))
}
