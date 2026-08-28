package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-settlement-gateway/internal/guardrail"
	"agent-settlement-gateway/internal/identity"
	"agent-settlement-gateway/internal/jupiter"
	"agent-settlement-gateway/internal/pricing"
	"agent-settlement-gateway/internal/proof"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// signAndRespond signs a, merges it into extra as "proof", and returns the
// whole thing as the tool's JSON text result. Every exit path of
// prepare_agent_swap — identity failure, pricing failure, guardrail denial,
// or success — goes through this so each carries the same signed proof.
func signAndRespond(signer *proof.Signer, a proof.Attestation, extra map[string]interface{}) (*mcp.CallToolResult, any, error) {
	p, err := signer.Sign(a)
	if err != nil {
		return nil, nil, err
	}
	if extra == nil {
		extra = map[string]interface{}{}
	}
	extra["proof"] = p
	text, err := json.MarshalIndent(extra, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(text)), nil, nil
}

type QuoteArgs struct {
	InputMint   string `json:"inputMint" jsonschema:"source token mint address"`
	OutputMint  string `json:"outputMint" jsonschema:"destination token mint address"`
	Amount      string `json:"amount" jsonschema:"amount in the input token's base units"`
	SlippageBps int    `json:"slippageBps,omitempty" jsonschema:"max slippage in basis points, default 50"`
}

func registerJupiterQuoteTool(server *mcp.Server, jup *jupiter.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "jupiter_quote",
		Description: "Get a swap quote from Jupiter without committing to a trade.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args QuoteArgs) (*mcp.CallToolResult, any, error) {
		slippage := args.SlippageBps
		if slippage == 0 {
			slippage = 50
		}
		quote, err := jup.Quote(jupiter.QuoteRequest{
			InputMint:   args.InputMint,
			OutputMint:  args.OutputMint,
			Amount:      args.Amount,
			SlippageBps: slippage,
		})
		if err != nil {
			return nil, nil, err
		}
		text, err := json.MarshalIndent(quote, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(text)), nil, nil
	})
}

type PrepareSwapArgs struct {
	AgentID       string `json:"agentId" jsonschema:"agent id, must be registered in identity.json"`
	Timestamp     int64  `json:"timestamp" jsonschema:"unix seconds when the request was signed; rejected if more than 60s from the gateway's clock"`
	Signature     string `json:"signature" jsonschema:"base64 Ed25519 signature over the canonical payload (see identity.CanonicalPayload), proving the caller holds the agent's private key — see cmd/agentsign"`
	InputMint     string `json:"inputMint" jsonschema:"source token mint address, must have a registered Pyth price feed (see internal/pricing.KnownMints)"`
	OutputMint    string `json:"outputMint" jsonschema:"destination token mint address"`
	Amount        string `json:"amount" jsonschema:"amount in the input token's base units — this, not a declared USD figure, is what gets priced"`
	SlippageBps   int    `json:"slippageBps,omitempty" jsonschema:"max slippage in basis points, default 50"`
	UserPublicKey string `json:"userPublicKey" jsonschema:"base58 public key that will sign and pay for the swap"`
}

// registerPrepareSwapTool wires four checks into one tool, in order:
// identity (a forged signature never reaches pricing or the guardrail),
// pricing (the input amount is priced from Pyth, not declared by the
// caller), the spend guardrail, and finally the Jupiter quote/build call.
// It stops at an unsigned, base64-encoded transaction — signing and
// submission stay with whatever holds the agent's wallet key, never this
// process.
func registerPrepareSwapTool(server *mcp.Server, jup *jupiter.Client, registry *identity.Registry, pricer *pricing.Client, limiter *guardrail.Limiter, signer *proof.Signer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_agent_swap",
		Description: "Verify the calling agent's signature, price the trade from Pyth, check its spend limit, fetch a Jupiter quote, build an unsigned swap transaction, and return a signed proof of every decision. Never signs or submits anything.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args PrepareSwapArgs) (*mcp.CallToolResult, any, error) {
		slippage := args.SlippageBps
		if slippage == 0 {
			slippage = 50
		}

		idDecision := registry.Verify(args.AgentID, args.InputMint, args.OutputMint, args.Amount, args.Timestamp, args.Signature)
		if !idDecision.Verified {
			return signAndRespond(signer, proof.Attestation{
				AgentID:          args.AgentID,
				IdentityVerified: false,
				IdentityReason:   idDecision.Reason,
				InputMint:        args.InputMint,
				OutputMint:       args.OutputMint,
				InAmount:         args.Amount,
				UsdValueSource:   "unavailable",
				GuardrailAllowed: false,
				GuardrailReason:  "identity check failed, pricing and the guardrail were not evaluated",
				IssuedAt:         time.Now().UTC(),
			}, map[string]interface{}{"identityDecision": idDecision})
		}

		usdValue, publishedAt, err := pricer.USDValue(args.InputMint, args.Amount)
		if err != nil {
			return signAndRespond(signer, proof.Attestation{
				AgentID:          args.AgentID,
				IdentityVerified: true,
				IdentityReason:   idDecision.Reason,
				InputMint:        args.InputMint,
				OutputMint:       args.OutputMint,
				InAmount:         args.Amount,
				UsdValueSource:   "unavailable",
				GuardrailAllowed: false,
				GuardrailReason:  "could not price trade from Pyth: " + err.Error(),
				IssuedAt:         time.Now().UTC(),
			}, map[string]interface{}{"identityDecision": idDecision})
		}

		decision := limiter.Check(args.AgentID, usdValue)
		extra := map[string]interface{}{
			"identityDecision":  idDecision,
			"pricedUsdValue":    usdValue,
			"pythPublishedAt":   publishedAt,
			"guardrailDecision": decision,
		}

		if !decision.Allowed {
			return signAndRespond(signer, proof.Attestation{
				AgentID:          args.AgentID,
				IdentityVerified: true,
				IdentityReason:   idDecision.Reason,
				InputMint:        args.InputMint,
				OutputMint:       args.OutputMint,
				InAmount:         args.Amount,
				UsdValue:         usdValue,
				UsdValueSource:   "pyth",
				PythPublishedAt:  publishedAt,
				GuardrailAllowed: false,
				GuardrailReason:  decision.Reason,
				IssuedAt:         time.Now().UTC(),
			}, extra)
		}

		quote, err := jup.Quote(jupiter.QuoteRequest{
			InputMint:   args.InputMint,
			OutputMint:  args.OutputMint,
			Amount:      args.Amount,
			SlippageBps: slippage,
		})
		if err != nil {
			return nil, nil, err
		}

		swapResp, err := jup.BuildSwapTransaction(jupiter.SwapRequest{
			Quote:         quote,
			UserPublicKey: args.UserPublicKey,
		})
		if err != nil {
			return nil, nil, err
		}

		extra["quote"] = quote
		extra["unsignedTransactionBase64"] = swapResp.SwapTransaction

		return signAndRespond(signer, proof.Attestation{
			AgentID:          args.AgentID,
			IdentityVerified: true,
			IdentityReason:   idDecision.Reason,
			InputMint:        quote.InputMint,
			OutputMint:       quote.OutputMint,
			InAmount:         quote.InAmount,
			OutAmount:        quote.OutAmount,
			PriceImpactPct:   quote.PriceImpactPct,
			UsdValue:         usdValue,
			UsdValueSource:   "pyth",
			PythPublishedAt:  publishedAt,
			GuardrailAllowed: true,
			GuardrailReason:  decision.Reason,
			IssuedAt:         time.Now().UTC(),
		}, extra)
	})
}
