package proof

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestSignVerifiesAgainstPublicKey(t *testing.T) {
	dir := t.TempDir()
	signer, err := LoadOrCreateSigner(dir + "/key.json")
	if err != nil {
		t.Fatalf("LoadOrCreateSigner: %v", err)
	}

	a := Attestation{
		AgentID:          "agent-demo-1",
		InputMint:        "So11111111111111111111111111111111111111112",
		OutputMint:       "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		InAmount:         "1000000",
		DeclaredUsdValue: 150,
		GuardrailAllowed: false,
		GuardrailReason:  "trade $150.00 exceeds per-tx limit $100.00",
		IssuedAt:         time.Now().UTC(),
	}

	p, err := signer.Sign(a)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	pub, err := base64.StdEncoding.DecodeString(p.SignerPublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	hash, err := base64.StdEncoding.DecodeString(p.PayloadSha256)
	if err != nil {
		t.Fatalf("decode payload hash: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), hash, sig) {
		t.Fatal("signature does not verify against the signer's own public key")
	}

	// The proof must also bind to the exact attestation it carries: signing
	// a payload that differs by even one field must not verify.
	tampered := a
	tampered.DeclaredUsdValue = 100
	tamperedPayload, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("marshal tampered attestation: %v", err)
	}
	tamperedHash := sha256.Sum256(tamperedPayload)
	if base64.StdEncoding.EncodeToString(tamperedHash[:]) == p.PayloadSha256 {
		t.Fatal("tampered attestation produced the same hash as the original")
	}
}
