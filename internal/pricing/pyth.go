// Package pricing prices a trade from Pyth so the guardrail checks a real
// USD figure instead of trusting whatever the caller declares.
package pricing

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// defaultBaseURL is Douro Labs' authenticated Hermes endpoint, the one Pyth's
// docs point to since the 2026-08-26 API-key cutover — hermes.pyth.network
// still works but requires the same key and isn't the documented target.
const defaultBaseURL = "https://pyth.dourolabs.app/hermes"

type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

// NewClient falls back to the public Hermes instance and the PYTH_API_KEY
// env var when baseURL/apiKey are empty. Hermes has required this key for
// every request since 2026-08-26; a missing or wrong key surfaces as a 401
// from LatestPrice, not a startup failure — a call denies with that reason
// rather than the gateway silently trusting an unpriced trade.
func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if apiKey == "" {
		apiKey = os.Getenv("PYTH_API_KEY")
	}
	return &Client{http: &http.Client{Timeout: 8 * time.Second}, baseURL: baseURL, apiKey: apiKey}
}

type rpcPrice struct {
	Price       string `json:"price"`
	Expo        int32  `json:"expo"`
	PublishTime int64  `json:"publish_time"`
}

type parsedPriceUpdate struct {
	ID    string   `json:"id"`
	Price rpcPrice `json:"price"`
}

type priceUpdateResponse struct {
	Parsed []parsedPriceUpdate `json:"parsed"`
}

// LatestPrice returns the current USD price for feedID (hex, no 0x prefix)
// and the time Pyth published it, from GET /v2/updates/price/latest.
func (c *Client) LatestPrice(feedID string) (float64, time.Time, error) {
	q := url.Values{}
	q.Add("ids[]", feedID)

	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/v2/updates/price/latest?"+q.Encode(), nil)
	if err != nil {
		return 0, time.Time{}, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("pyth price request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, time.Time{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, time.Time{}, fmt.Errorf("pyth price request returned %d: %s", resp.StatusCode, body)
	}

	var parsed priceUpdateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, time.Time{}, fmt.Errorf("decode pyth response: %w", err)
	}
	if len(parsed.Parsed) == 0 {
		return 0, time.Time{}, fmt.Errorf("pyth returned no price for feed %s", feedID)
	}

	p := parsed.Parsed[0]
	raw, err := strconv.ParseInt(p.Price.Price, 10, 64)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse pyth price: %w", err)
	}
	price := float64(raw) * math.Pow10(int(p.Price.Expo))
	return price, time.Unix(p.Price.PublishTime, 0).UTC(), nil
}
