package pricing

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

type MintInfo struct {
	FeedID   string
	Decimals int
	Symbol   string
}

// KnownMints is a small, manually maintained table — good enough for a
// prototype, not a substitute for a real registry in production. Feed IDs
// verified directly against https://hermes.pyth.network/v2/price_feeds.
// Decimals come from each token's well-known SPL mint, not from Pyth.
var KnownMints = map[string]MintInfo{
	"So11111111111111111111111111111111111111112": {
		FeedID:   "ef0d8b6fda2ceba41da15d4095d1da392a0d2f8ed0c6c7bc0f4cfac8c280b56d",
		Decimals: 9,
		Symbol:   "SOL",
	},
	"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v": {
		FeedID:   "eaa020c61cc479712813461ce153894a96a6c00b21ed0cfc2798d1f9a9e9c94a",
		Decimals: 6,
		Symbol:   "USDC",
	},
}

func tokensFromBaseUnits(amountBaseUnits string, decimals int) (float64, error) {
	raw, err := strconv.ParseFloat(amountBaseUnits, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", amountBaseUnits, err)
	}
	return raw / math.Pow10(decimals), nil
}

// USDValue prices amountBaseUnits of mint using Pyth's latest price. It
// errors on any mint not in KnownMints rather than guessing — failing
// closed is the point of replacing a caller-declared figure with this.
func (c *Client) USDValue(mint, amountBaseUnits string) (float64, time.Time, error) {
	info, ok := KnownMints[mint]
	if !ok {
		return 0, time.Time{}, fmt.Errorf("no Pyth price feed registered for mint %s", mint)
	}
	tokens, err := tokensFromBaseUnits(amountBaseUnits, info.Decimals)
	if err != nil {
		return 0, time.Time{}, err
	}
	price, publishedAt, err := c.LatestPrice(info.FeedID)
	if err != nil {
		return 0, time.Time{}, err
	}
	return tokens * price, publishedAt, nil
}
