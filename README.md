# agent-settlement-gateway

Prototype v0 of the "agent-native settlement layer" wedge: an MCP server an
AI agent talks to instead of calling Jupiter directly, so a spend limit gets
enforced and a signed proof of the decision comes back — before anything
touches a wallet.

Verified live end to end on 2026-08-28: real Pyth prices (SOL ~$104,
USDC ~$1), a real Jupiter quote and unsigned transaction on an allowed
trade, and a correct guardrail denial on an over-limit one — see "Try it"
below to reproduce.

What it does:

- `jupiter_quote` — pass-through quote from Jupiter's `lite-api.jup.ag`
  endpoint (the old `quote-api.jup.ag/v6` host is unreachable / retired).
- `prepare_agent_swap` — verifies the calling agent's signature
  (`internal/identity`), prices the input amount from Pyth
  (`internal/pricing`), checks the resulting USD figure against the agent's
  spend limit (`internal/guardrail`), fetches a quote, asks Jupiter to build
  an unsigned swap transaction, and returns everything plus an
  Ed25519-signed proof (`internal/proof`) of every decision and why it was
  made. The four steps run in that order, and each one gates the next: a
  forged signature never reaches pricing, an unpriceable trade never reaches
  the guardrail.

What it deliberately does **not** do: hold a wallet key, sign a transaction,
or submit one. `prepare_agent_swap` hands back an unsigned, base64-encoded
transaction; signing and submission stay with whatever custodies the
agent's key. That boundary is the point of a v0 — it proves the
identity/guardrail/proof mechanics without taking on custody risk.

## Known simplifications (fix before this touches real money)

- **Only two mints have a registered price feed.** `internal/pricing.KnownMints`
  hardcodes SOL and USDC's Pyth feed ID and decimals — a trade in any other
  mint is denied with "no Pyth price feed registered", not silently mispriced.
  Production needs a real mint→feed registry (and mint decimals read from the
  chain, not a hardcoded table), not this list.
- **Pyth Hermes requires an API key as of 2026-08-26.** Set `PYTH_API_KEY`
  (see "Run" below) — without it every `prepare_agent_swap` call denies with
  a 401 from Pyth baked into the reason, which is the correct fail-closed
  behavior but means the tool does nothing useful until it's set.
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

Get a Pyth API key first (required by Hermes since 2026-08-26) — see
[docs.pyth.network](https://docs.pyth.network) for how to obtain one.

```bash
cp config.example.json config.json
cp identity.example.json identity.json   # or register your own agent, see below
PYTH_API_KEY=<your key> GUARDRAIL_CONFIG=config.json IDENTITY_CONFIG=identity.json ./gateway
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
        "IDENTITY_CONFIG": "/absolute/path/to/identity.json",
        "PYTH_API_KEY": "<your key>"
      }
    }
  }
}
```

`PYTH_HERMES_URL` overrides the default `https://hermes.pyth.network` if
you're pointed at a different Hermes instance.

## Register an agent and sign a request

Every `prepare_agent_swap` call needs `timestamp` + `signature` proving the
caller holds the private key registered for `agentId`. Generate a keypair:

```bash
go run ./cmd/agentkey -agent my-agent
# prints a public key (put it in identity.json) and a private key (keep it)
```

Then sign the exact trade you're about to request — the signature is bound
to `agentId`, `inputMint`, `outputMint`, and `amount`, so it can't be
replayed against a different trade, and it expires after 60s so it can't be
replayed later either. There's no USD value to sign: the gateway prices
`amount` from Pyth itself once identity passes.

```bash
go run ./cmd/agentsign -agent my-agent -private-key <PRIVATE_KEY> \
  -input-mint <MINT> -output-mint <MINT> -amount 1000000
# prints the timestamp and signature to pass into prepare_agent_swap
```

`identity.example.json` ships with a working demo agent (`agent-demo-1`); its
private key is `YpznZvckQItqckgN8eRxLc191nod8in5L57tjc6TpWiDq91qB1P2LtNF5fkjoXBtHgaBhcd2Y/xrDtK8TrVu/g==` —
published here on purpose, it exists only to exercise the flow above and
must never be treated as a real secret.

## Try it without a real wallet

`prepare_agent_swap` only needs a public key to build the transaction
against — any valid base58 Solana address works for `userPublicKey` since
nothing gets signed or sent. Try, for `agent-demo-1`: a forged signature, a
stale timestamp, an input mint that isn't SOL or USDC, a SOL amount priced
over $500/day — each denies at a different one of the four steps, and every
denial still comes back as a signed proof explaining which step and why.

## Next (not built yet)

- Onboarding/revocation for `identity.json` and a real registry standard
  (ERC-8004) instead of a local file.
- A real mint→feed registry (Pyth publishes one) instead of the two-entry
  `KnownMints` table, and mint decimals read from the chain.
- Persist guardrail state past a process restart.
- Publish the attestation schema as the "Swap Mandate Extension" spec so
  other implementations can verify a proof without trusting this server.
