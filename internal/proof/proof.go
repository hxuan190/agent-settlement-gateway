// Package proof signs a small attestation of what the gateway decided —
// which agent asked for what, whether the guardrail allowed it, and the
// quote it was allowed against. It never touches fund-moving keys.
package proof

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Signer struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

type keyFile struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

// LoadOrCreateSigner is demo-grade key storage: this key only ever signs the
// attestations below, never a transaction that moves funds, so a plaintext
// file is acceptable for a prototype. Do not reuse it for anything that
// touches custody.
func LoadOrCreateSigner(path string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var kf keyFile
		if err := json.Unmarshal(data, &kf); err != nil {
			return nil, fmt.Errorf("parse key file: %w", err)
		}
		pub, err := base64.StdEncoding.DecodeString(kf.PublicKey)
		if err != nil {
			return nil, err
		}
		priv, err := base64.StdEncoding.DecodeString(kf.PrivateKey)
		if err != nil {
			return nil, err
		}
		return &Signer{public: pub, private: priv}, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	kf := keyFile{
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
	}
	out, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return &Signer{public: pub, private: priv}, nil
}

func (s *Signer) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.public)
}

type Attestation struct {
	AgentID          string    `json:"agentId"`
	IdentityVerified bool      `json:"identityVerified"`
	IdentityReason   string    `json:"identityReason"`
	InputMint        string    `json:"inputMint"`
	OutputMint       string    `json:"outputMint"`
	InAmount         string    `json:"inAmount"`
	OutAmount        string    `json:"outAmount,omitempty"`
	PriceImpactPct   string    `json:"priceImpactPct,omitempty"`
	DeclaredUsdValue float64   `json:"declaredUsdValue"`
	GuardrailAllowed bool      `json:"guardrailAllowed"`
	GuardrailReason  string    `json:"guardrailReason"`
	IssuedAt         time.Time `json:"issuedAt"`
}

type Proof struct {
	Attestation     Attestation `json:"attestation"`
	PayloadSha256   string      `json:"payloadSha256"`
	Signature       string      `json:"signature"`
	SignerPublicKey string      `json:"signerPublicKey"`
}

// Sign hashes the attestation and signs the hash, so a verifier who only has
// the gateway's public key can confirm both what was decided and that it
// hasn't been altered since.
func (s *Signer) Sign(a Attestation) (*Proof, error) {
	payload, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(payload)
	sig := ed25519.Sign(s.private, hash[:])
	return &Proof{
		Attestation:     a,
		PayloadSha256:   base64.StdEncoding.EncodeToString(hash[:]),
		Signature:       base64.StdEncoding.EncodeToString(sig),
		SignerPublicKey: s.PublicKeyBase64(),
	}, nil
}
