// Package guardrail enforces per-agent spend limits ahead of any swap.
package guardrail

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type AgentLimit struct {
	DailyLimitUsd float64 `json:"dailyLimitUsd"`
	PerTxLimitUsd float64 `json:"perTxLimitUsd"`
}

type Config struct {
	Agents               map[string]AgentLimit `json:"agents"`
	DefaultDailyLimitUsd float64               `json:"defaultDailyLimitUsd"`
	DefaultPerTxLimitUsd float64               `json:"defaultPerTxLimitUsd"`
}

type agentState struct {
	spentTodayUsd float64
	dayStart      time.Time
}

// Limiter tracks spend in memory only — restarting the gateway resets every
// agent's daily counter. Good enough for a prototype; a real deployment
// needs this persisted.
type Limiter struct {
	cfg   Config
	mu    sync.Mutex
	state map[string]*agentState
}

func Load(path string) (*Limiter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read guardrail config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse guardrail config: %w", err)
	}
	return &Limiter{cfg: cfg, state: make(map[string]*agentState)}, nil
}

type Decision struct {
	Allowed           bool    `json:"allowed"`
	Reason            string  `json:"reason"`
	PerTxLimitUsd     float64 `json:"perTxLimitUsd"`
	DailyLimitUsd     float64 `json:"dailyLimitUsd"`
	SpentTodayUsd     float64 `json:"spentTodayUsd"`
	RemainingTodayUsd float64 `json:"remainingTodayUsd"`
}

func (l *Limiter) limitsFor(agentID string) AgentLimit {
	if lim, ok := l.cfg.Agents[agentID]; ok {
		return lim
	}
	return AgentLimit{DailyLimitUsd: l.cfg.DefaultDailyLimitUsd, PerTxLimitUsd: l.cfg.DefaultPerTxLimitUsd}
}

// Check evaluates a trade of usdValue for agentID and, if it's within limits,
// records the spend immediately. There is no separate commit step, so a
// caller must not call Check for a trade it then decides not to submit.
func (l *Limiter) Check(agentID string, usdValue float64) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	lim := l.limitsFor(agentID)
	now := time.Now().UTC()
	st, ok := l.state[agentID]
	if !ok || now.Sub(st.dayStart) > 24*time.Hour {
		st = &agentState{dayStart: now}
		l.state[agentID] = st
	}

	if usdValue > lim.PerTxLimitUsd {
		return Decision{
			Allowed:           false,
			Reason:            fmt.Sprintf("trade $%.2f exceeds per-tx limit $%.2f", usdValue, lim.PerTxLimitUsd),
			PerTxLimitUsd:     lim.PerTxLimitUsd,
			DailyLimitUsd:     lim.DailyLimitUsd,
			SpentTodayUsd:     st.spentTodayUsd,
			RemainingTodayUsd: lim.DailyLimitUsd - st.spentTodayUsd,
		}
	}
	if st.spentTodayUsd+usdValue > lim.DailyLimitUsd {
		return Decision{
			Allowed:           false,
			Reason:            fmt.Sprintf("trade would push daily spend to $%.2f, over limit $%.2f", st.spentTodayUsd+usdValue, lim.DailyLimitUsd),
			PerTxLimitUsd:     lim.PerTxLimitUsd,
			DailyLimitUsd:     lim.DailyLimitUsd,
			SpentTodayUsd:     st.spentTodayUsd,
			RemainingTodayUsd: lim.DailyLimitUsd - st.spentTodayUsd,
		}
	}

	st.spentTodayUsd += usdValue
	return Decision{
		Allowed:           true,
		Reason:            "within limits",
		PerTxLimitUsd:     lim.PerTxLimitUsd,
		DailyLimitUsd:     lim.DailyLimitUsd,
		SpentTodayUsd:     st.spentTodayUsd,
		RemainingTodayUsd: lim.DailyLimitUsd - st.spentTodayUsd,
	}
}
