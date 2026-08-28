package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-settlement-gateway/internal/guardrail"
	"agent-settlement-gateway/internal/identity"
	"agent-settlement-gateway/internal/jupiter"
	"agent-settlement-gateway/internal/proof"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
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
	AgentID          string  `json:"agentId" jsonschema:"agent id, must be registered in identity.json"`
	Timestamp        int64   `json:"timestamp" jsonschema:"unix seconds when the request was signed; rejected if more than 60s from the gateway's clock"`
	Signature        string  `json:"signature" jsonschema:"base64 Ed25519 signature over the canonical payload (see identity.CanonicalPayload), proving the caller holds the agent's private key — see cmd/agentsign"`
	InputMint        string  `json:"inputMint" jsonschema:"source token mint address"`
	OutputMint       string  `json:"outputMint" jsonschema:"destination token mint address"`
	Amount           string  `json:"amount" jsonschema:"amount in the input token's base units"`
	SlippageBps      int     `json:"slippageBps,omitempty" jsonschema:"max slippage in basis points, default 50"`
	UserPublicKey    string  `json:"userPublicKey" jsonschema:"base58 public key that will sign and pay for the swap"`
	DeclaredUsdValue float64 `json:"declaredUsdValue" jsonschema:"caller-supplied USD value of the trade, checked against the agent's spend limit; v0 trusts this input, production must price it from an oracle instead"`
}

// registerPrepareSwapTool wires identity verification, the guardrail check,
// the Jupiter quote/build call, and the signed proof into one tool. It stops
// at an unsigned, base64-encoded transaction — signing and submission stay
// with whatever holds the agent's wallet key, never this process.
func registerPrepareSwapTool(server *mcp.Server, jup *jupiter.Client, registry *identity.Registry, limiter *guardrail.Limiter, signer *proof.Signer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_agent_swap",
		Description: "Verify the calling agent's signature, check its spend limit, fetch a Jupiter quote, build an unsigned swap transaction, and return a signed proof of every decision. Never signs or submits anything.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args PrepareSwapArgs) (*mcp.CallToolResult, any, error) {
		slippage := args.SlippageBps
		if slippage == 0 {
			slippage = 50
		}

		idDecision := registry.Verify(args.AgentID, args.InputMint, args.OutputMint, args.Amount, args.DeclaredUsdValue, args.Timestamp, args.Signature)
		if !idDecision.Verified {
			p, err := signer.Sign(proof.Attestation{
				AgentID:          args.AgentID,
				IdentityVerified: false,
				IdentityReason:   idDecision.Reason,
				InputMint:        args.InputMint,
				OutputMint:       args.OutputMint,
				InAmount:         args.Amount,
				DeclaredUsdValue: args.DeclaredUsdValue,
				GuardrailAllowed: false,
				GuardrailReason:  "identity check failed, guardrail was not evaluated",
				IssuedAt:         time.Now().UTC(),
			})
			if err != nil {
				return nil, nil, err
			}
			result := map[string]interface{}{"identityDecision": idDecision, "proof": p}
			text, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(text)), nil, nil
		}

		decision := limiter.Check(args.AgentID, args.DeclaredUsdValue)
		result := map[string]interface{}{"identityDecision": idDecision, "guardrailDecision": decision}

		if !decision.Allowed {
			p, err := signer.Sign(proof.Attestation{
				AgentID:          args.AgentID,
				IdentityVerified: true,
				IdentityReason:   idDecision.Reason,
				InputMint:        args.InputMint,
				OutputMint:       args.OutputMint,
				InAmount:         args.Amount,
				DeclaredUsdValue: args.DeclaredUsdValue,
				GuardrailAllowed: false,
				GuardrailReason:  decision.Reason,
				IssuedAt:         time.Now().UTC(),
			})
			if err != nil {
				return nil, nil, err
			}
			result["proof"] = p
			text, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(text)), nil, nil
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

		p, err := signer.Sign(proof.Attestation{
			AgentID:          args.AgentID,
			IdentityVerified: true,
			IdentityReason:   idDecision.Reason,
			InputMint:        quote.InputMint,
			OutputMint:       quote.OutputMint,
			InAmount:         quote.InAmount,
			OutAmount:        quote.OutAmount,
			PriceImpactPct:   quote.PriceImpactPct,
			DeclaredUsdValue: args.DeclaredUsdValue,
			GuardrailAllowed: true,
			GuardrailReason:  decision.Reason,
			IssuedAt:         time.Now().UTC(),
		})
		if err != nil {
			return nil, nil, err
		}

		result["quote"] = quote
		result["unsignedTransactionBase64"] = swapResp.SwapTransaction
		result["proof"] = p

		text, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(text)), nil, nil
	})
}
