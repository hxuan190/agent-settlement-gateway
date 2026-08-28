# agent-settlement-gateway

Prototype v0 of the "agent-native settlement layer" wedge: an MCP server an
AI agent talks to instead of calling Jupiter directly, so a spend limit gets
enforced and a signed proof of the decision comes back — before anything
touches a wallet.

What it does:

- `jupiter_quote` — pass-through quote from Jupiter's v6 API.
- `prepare_agent_swap` — checks the calling agent's spend limit
  (`internal/guardrail`), fetches a quote, asks Jupiter to build an unsigned
  swap transaction, and returns everything plus an Ed25519-signed proof
  (`internal/proof`) of what was decided and why.

What it deliberately does **not** do: hold a wallet key, sign a transaction,
or submit one. `prepare_agent_swap` hands back an unsigned, base64-encoded
transaction; signing and submission stay with whatever custodies the
agent's key. That boundary is the point of a v0 — it proves the
identity/guardrail/proof mechanics without taking on custody risk.

## Known simplifications (fix before this touches real money)

- **`declaredUsdValue` is trusted, not verified.** The guardrail checks the
  USD figure the caller supplies — it doesn't price the trade itself. A
  production version needs a price oracle (e.g. Pyth) in that path, not
  caller-declared input.
- **Spend tracking is in-memory.** Restarting the process resets every
  agent's daily counter. Fine for a demo, not for anything that needs to
  hold a limit across restarts.
- **No real agent identity.** `agentId` is just a string key into
  `config.json` — there's no KYA/ERC-8004-style verification that the caller
  actually is that agent. That's the next layer to add, not this one.
- **The signing key is stored in plaintext** (`gateway_key.json`, created on
  first run). Acceptable here because it only ever signs attestations, never
  a fund-moving transaction — don't reuse this pattern for anything that
  does.

## Build

This sandbox has no outbound network, so the SDK dependency in `go.mod`
can't be fetched here. On a machine with network access:

```bash
go mod tidy   # fetches github.com/modelcontextprotocol/go-sdk and writes go.sum
go build -o gateway ./cmd/gateway
```

## Run

```bash
cp config.example.json config.json
GUARDRAIL_CONFIG=config.json ./gateway
```

It speaks MCP over stdio, so point an MCP client at the binary rather than
running it standalone. For Claude Desktop or Claude Code, add to the MCP
config:

```json
{
  "mcpServers": {
    "agent-settlement-gateway": {
      "command": "/absolute/path/to/gateway",
      "env": { "GUARDRAIL_CONFIG": "/absolute/path/to/config.json" }
    }
  }
}
```

## Try it without a real wallet

`prepare_agent_swap` only needs a public key to build the transaction
against — any valid base58 Solana address works for `userPublicKey` since
nothing gets signed or sent. Ask the agent to quote or prepare a swap for
`agentId: "agent-demo-1"` a few times in a row past $500/day to see the
guardrail actually deny one.

## Next (not built yet)

- Real agent identity (KYA / ERC-8004) instead of a bare string `agentId`.
- Price the trade from an oracle instead of trusting `declaredUsdValue`.
- Persist guardrail state past a process restart.
- Publish the attestation schema as the "Swap Mandate Extension" spec so
  other implementations can verify a proof without trusting this server.
