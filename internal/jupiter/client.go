// Package jupiter wraps the public Jupiter v6 quote/swap HTTP API.
package jupiter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// quote-api.jup.ag/v6 is unreachable from this environment (and appears
// retired); lite-api.jup.ag/swap/v1 is Jupiter's current free-tier
// endpoint and was verified live against these exact paths.
const (
	quoteURL = "https://lite-api.jup.ag/swap/v1/quote"
	swapURL  = "https://lite-api.jup.ag/swap/v1/swap"
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second}}
}

type QuoteRequest struct {
	InputMint   string
	OutputMint  string
	Amount      string
	SlippageBps int
}

type RoutePlanStep struct {
	SwapInfo struct {
		AmmKey     string `json:"ammKey"`
		Label      string `json:"label"`
		InputMint  string `json:"inputMint"`
		OutputMint string `json:"outputMint"`
		InAmount   string `json:"inAmount"`
		OutAmount  string `json:"outAmount"`
	} `json:"swapInfo"`
	Percent int `json:"percent"`
}

type Quote struct {
	InputMint            string          `json:"inputMint"`
	InAmount             string          `json:"inAmount"`
	OutputMint           string          `json:"outputMint"`
	OutAmount            string          `json:"outAmount"`
	OtherAmountThreshold string          `json:"otherAmountThreshold"`
	SwapMode             string          `json:"swapMode"`
	SlippageBps          int             `json:"slippageBps"`
	PriceImpactPct       string          `json:"priceImpactPct"`
	RoutePlan            []RoutePlanStep `json:"routePlan"`
}

func (c *Client) Quote(req QuoteRequest) (*Quote, error) {
	q := url.Values{}
	q.Set("inputMint", req.InputMint)
	q.Set("outputMint", req.OutputMint)
	q.Set("amount", req.Amount)
	q.Set("slippageBps", strconv.Itoa(req.SlippageBps))

	httpReq, err := http.NewRequest(http.MethodGet, quoteURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jupiter quote request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jupiter quote returned %d: %s", resp.StatusCode, body)
	}

	var quote Quote
	if err := json.Unmarshal(body, &quote); err != nil {
		return nil, fmt.Errorf("decode quote: %w", err)
	}
	return &quote, nil
}

type SwapRequest struct {
	Quote         *Quote
	UserPublicKey string
}

type SwapResponse struct {
	SwapTransaction string `json:"swapTransaction"`
}

// BuildSwapTransaction returns Jupiter's unsigned, base64-encoded transaction
// for the given quote. The gateway never signs or submits it — that stays
// with whatever holds the agent's keys.
func (c *Client) BuildSwapTransaction(req SwapRequest) (*SwapResponse, error) {
	payload := map[string]interface{}{
		"quoteResponse":    req.Quote,
		"userPublicKey":    req.UserPublicKey,
		"wrapAndUnwrapSol": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, swapURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jupiter swap request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jupiter swap returned %d: %s", resp.StatusCode, respBody)
	}

	var swapResp SwapResponse
	if err := json.Unmarshal(respBody, &swapResp); err != nil {
		return nil, fmt.Errorf("decode swap response: %w", err)
	}
	return &swapResp, nil
}
