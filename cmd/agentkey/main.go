// Command agentkey generates an Ed25519 keypair for one agent. Put the
// public key in identity.json under that agent's id; keep the private key
// wherever the agent signs its own requests from.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	agentID := flag.String("agent", "", "agent id this key belongs to (only used for the printed identity.json snippet)")
	flag.Parse()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate key:", err)
		os.Exit(1)
	}

	pubB64 := base64.StdEncoding.EncodeToString(pub)
	privB64 := base64.StdEncoding.EncodeToString(priv)

	fmt.Printf("public key  (identity.json):        %s\n", pubB64)
	fmt.Printf("private key (keep with the agent):  %s\n", privB64)
	if *agentID != "" {
		fmt.Printf("\nidentity.json entry:\n  \"%s\": \"%s\"\n", *agentID, pubB64)
	}
}
