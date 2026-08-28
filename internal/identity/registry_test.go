package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T, agentID string) (*Registry, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cfg := Config{Agents: map[string]string{agentID: base64.StdEncoding.EncodeToString(pub)}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return reg, priv
}

func sign(t *testing.T, priv ed25519.PrivateKey, agentID, inputMint, outputMint, amount string, ts int64) string {
	t.Helper()
	sig := ed25519.Sign(priv, CanonicalPayload(agentID, inputMint, outputMint, amount, ts))
	return base64.StdEncoding.EncodeToString(sig)
}

func TestVerifyAcceptsValidSignature(t *testing.T) {
	reg, priv := newTestRegistry(t, "agent-a")
	ts := time.Now().Unix()
	sig := sign(t, priv, "agent-a", "SOL", "USDC", "1000000", ts)

	d := reg.Verify("agent-a", "SOL", "USDC", "1000000", ts, sig)
	if !d.Verified {
		t.Fatalf("expected verified, got denied: %s", d.Reason)
	}
}

func TestVerifyRejectsUnregisteredAgent(t *testing.T) {
	reg, priv := newTestRegistry(t, "agent-a")
	ts := time.Now().Unix()
	sig := sign(t, priv, "agent-a", "SOL", "USDC", "1000000", ts)

	d := reg.Verify("agent-b", "SOL", "USDC", "1000000", ts, sig)
	if d.Verified {
		t.Fatal("expected unregistered agent to be rejected")
	}
}

func TestVerifyRejectsTamperedField(t *testing.T) {
	reg, priv := newTestRegistry(t, "agent-a")
	ts := time.Now().Unix()
	sig := sign(t, priv, "agent-a", "SOL", "USDC", "1000000", ts)

	// same signature, but amount changed after signing
	d := reg.Verify("agent-a", "SOL", "USDC", "999999", ts, sig)
	if d.Verified {
		t.Fatal("expected signature to be invalid once a signed field changes")
	}
}

func TestVerifyRejectsStaleTimestamp(t *testing.T) {
	reg, priv := newTestRegistry(t, "agent-a")
	ts := time.Now().Add(-5 * time.Minute).Unix()
	sig := sign(t, priv, "agent-a", "SOL", "USDC", "1000000", ts)

	d := reg.Verify("agent-a", "SOL", "USDC", "1000000", ts, sig)
	if d.Verified {
		t.Fatal("expected a 5-minute-old timestamp to be rejected as outside the replay window")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	reg, _ := newTestRegistry(t, "agent-a")
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ts := time.Now().Unix()
	sig := sign(t, otherPriv, "agent-a", "SOL", "USDC", "1000000", ts)

	d := reg.Verify("agent-a", "SOL", "USDC", "1000000", ts, sig)
	if d.Verified {
		t.Fatal("expected signature from an unregistered key to be rejected")
	}
}
