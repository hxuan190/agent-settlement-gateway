package guardrail

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": {"agent-a": {"dailyLimitUsd": 500, "perTxLimitUsd": 100}},
		"defaultDailyLimitUsd": 10,
		"defaultPerTxLimitUsd": 5
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return l
}

func TestCheckAllowsWithinLimits(t *testing.T) {
	l := newTestLimiter(t)
	d := l.Check("agent-a", 80)
	if !d.Allowed {
		t.Fatalf("expected allowed, got denied: %s", d.Reason)
	}
	if d.SpentTodayUsd != 80 {
		t.Fatalf("expected spentTodayUsd=80, got %v", d.SpentTodayUsd)
	}
}

func TestCheckDeniesOverPerTxLimit(t *testing.T) {
	l := newTestLimiter(t)
	d := l.Check("agent-a", 150)
	if d.Allowed {
		t.Fatal("expected per-tx limit to deny a $150 trade against a $100 cap")
	}
}

func TestCheckDeniesOverDailyLimit(t *testing.T) {
	l := newTestLimiter(t)
	for i := 0; i < 5; i++ {
		if d := l.Check("agent-a", 90); !d.Allowed {
			t.Fatalf("trade %d unexpectedly denied: %s", i, d.Reason)
		}
	}
	// five trades of $90 = $450 spent; a sixth $90 trade would cross $500.
	if d := l.Check("agent-a", 90); d.Allowed {
		t.Fatal("expected daily limit to deny after $450 already spent")
	}
}

func TestCheckFallsBackToDefaultLimitsForUnknownAgent(t *testing.T) {
	l := newTestLimiter(t)
	d := l.Check("unregistered-agent", 6)
	if d.Allowed {
		t.Fatal("expected default per-tx limit ($5) to deny a $6 trade")
	}
}
