// Package identity is the KYA-style check: agentId alone proves nothing, so
// every request must also carry a signature only the registered key holder
// could have produced, over a payload bound to that exact trade.
package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Agents map[string]string `json:"agents"` // agentId -> base64 Ed25519 public key
}

type Registry struct {
	keys map[string]ed25519.PublicKey
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity registry %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse identity registry: %w", err)
	}
	keys := make(map[string]ed25519.PublicKey, len(cfg.Agents))
	for id, pubB64 := range cfg.Agents {
		pub, err := base64.StdEncoding.DecodeString(pubB64)
		if err != nil {
			return nil, fmt.Errorf("agent %s: decode public key: %w", id, err)
		}
		if len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("agent %s: public key has the wrong length", id)
		}
		keys[id] = ed25519.PublicKey(pub)
	}
	return &Registry{keys: keys}, nil
}

// CanonicalPayload is the exact byte sequence an agent signs to prove it
// authored a given swap request. Every field that affects money movement or
// spend-limit accounting is included, so a valid signature can't be replayed
// against a different trade — and the timestamp keeps it from being replayed
// against the same one later.
func CanonicalPayload(agentID, inputMint, outputMint, amount string, declaredUsdValue float64, timestamp int64) []byte {
	s := fmt.Sprintf("%s|%s|%s|%s|%s|%d",
		agentID, inputMint, outputMint, amount,
		strconv.FormatFloat(declaredUsdValue, 'f', -1, 64), timestamp)
	return []byte(s)
}

const maxClockSkew = 60 * time.Second

type Decision struct {
	Verified bool   `json:"verified"`
	Reason   string `json:"reason"`
}

// Verify checks that agentID is registered, that timestamp is recent enough
// to rule out a replayed signature, and that signatureB64 matches the
// canonical payload for these exact trade parameters under the agent's
// registered public key.
func (r *Registry) Verify(agentID, inputMint, outputMint, amount string, declaredUsdValue float64, timestamp int64, signatureB64 string) Decision {
	pub, ok := r.keys[agentID]
	if !ok {
		return Decision{Verified: false, Reason: fmt.Sprintf("agent %q is not registered", agentID)}
	}

	skew := time.Since(time.Unix(timestamp, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > maxClockSkew {
		return Decision{Verified: false, Reason: "timestamp is outside the allowed window (60s) — request may be replayed, or clocks are out of sync"}
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return Decision{Verified: false, Reason: "signature is not valid base64"}
	}

	payload := CanonicalPayload(agentID, inputMint, outputMint, amount, declaredUsdValue, timestamp)
	if !ed25519.Verify(pub, payload, sig) {
		return Decision{Verified: false, Reason: "signature does not match the registered public key for this agent"}
	}
	return Decision{Verified: true, Reason: "signature verified"}
}
