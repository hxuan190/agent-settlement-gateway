# agent-settlement-gateway

Prototype v0 of the "agent-native settlement layer" wedge: an MCP server an
AI agent talks to instead of calling Jupiter directly, so a spend limit gets
enforced and a signed proof of the decision comes back — before anything
touches a wallet.

What it does:

- `jupiter_quote` — pass-through quote from Jupiter's v6 API.
- `prepare_agent_swap` — verifies the calling agent's signature
  (`internal/identity`), checks its spend limit (`internal/guardrail`),
  fetches a quote, asks Jupiter to build an unsigned swap transaction, and
  returns everything plus an Ed25519-signed proof (`internal/proof`) of
  every decision and why it was made. Identity is checked before the
  guardrail: a forged signature never reaches spend-limit logic.

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
- **Identity verification is signature-only, not a full KYA/ERC-8004
  flow.** `agentId` must now be registered in `identity.json` with a public
  key, and every `prepare_agent_swap` call must carry a fresh Ed25519
  signature over the trade (`internal/identity`, see "Try it" below) — so a
  bare string can no longer impersonate an agent. What's still missing:
  onboarding/revocation of agents, and a real reputation/registry standard
  (ERC-8004) instead of a local JSON file.
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
cp identity.example.json identity.json   # or register your own agent, see below
GUARDRAIL_CONFIG=config.json IDENTITY_CONFIG=identity.json ./gateway
```

It speaks MCP over stdio, so point an MCP client at the binary rather than
running it standalone. For Claude Desktop or Claude Code, add to the MCP
config:

```json
{
  "mcpServers": {
    "agent-settlement-gateway": {
      "command": "/absolute/path/to/gateway",
      "env": {
        "GUARDRAIL_CONFIG": "/absolute/path/to/config.json",
        "IDENTITY_CONFIG": "/absolute/path/to/identity.json"
      }
    }
  }
}
```

## Register an agent and sign a request

Every `prepare_agent_swap` call needs `timestamp` + `signature` proving the
caller holds the private key registered for `agentId`. Generate a keypair:

```bash
go run ./cmd/agentkey -agent my-agent
# prints a public key (put it in identity.json) and a private key (keep it)
```

Then sign the exact trade you're about to request — the signature is bound
to `agentId`, `inputMint`, `outputMint`, `amount`, and `declaredUsdValue`, so
it can't be replayed against a different trade, and it expires after 60s so
it can't be replayed later either:

```bash
go run ./cmd/agentsign -agent my-agent -private-key <PRIVATE_KEY> \
  -input-mint <MINT> -output-mint <MINT> -amount 1000000 -usd-value 50
# prints the timestamp and signature to pass into prepare_agent_swap
```

`identity.example.json` ships with a working demo agent (`agent-demo-1`); its
private key is `YpznZvckQItqckgN8eRxLc191nod8in5L57tjc6TpWiDq91qB1P2LtNF5fkjoXBtHgaBhcd2Y/xrDtK8TrVu/g==` —
published here on purpose, it exists only to exercise the flow above and
must never be treated as a real secret.

## Try it without a real wallet

`prepare_agent_swap` only needs a public key to build the transaction
against — any valid base58 Solana address works for `userPublicKey` since
nothing gets signed or sent. Sign a few trades in a row for `agent-demo-1`
past $500/day (or a forged signature, or a stale timestamp) to see the
identity check and the guardrail each deny in their own way.

## Next (not built yet)

- Onboarding/revocation for `identity.json` and a real registry standard
  (ERC-8004) instead of a local file.
- Price the trade from an oracle instead of trusting `declaredUsdValue`.
- Persist guardrail state past a process restart.
- Publish the attestation schema as the "Swap Mandate Extension" spec so
  other implementations can verify a proof without trusting this server.
