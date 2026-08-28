package pricing

import "testing"

func TestTokensFromBaseUnits(t *testing.T) {
	cases := []struct {
		amount   string
		decimals int
		want     float64
	}{
		{"1000000000", 9, 1},  // 1 SOL in lamports
		{"1000000", 6, 1},     // 1 USDC in base units
		{"1500000", 6, 1.5},   // 1.5 USDC
		{"1", 9, 0.000000001}, // 1 lamport
	}
	for _, c := range cases {
		got, err := tokensFromBaseUnits(c.amount, c.decimals)
		if err != nil {
			t.Fatalf("tokensFromBaseUnits(%q, %d): %v", c.amount, c.decimals, err)
		}
		if diff := got - c.want; diff > 1e-12 || diff < -1e-12 {
			t.Fatalf("tokensFromBaseUnits(%q, %d) = %v, want %v", c.amount, c.decimals, got, c.want)
		}
	}
}

func TestTokensFromBaseUnitsRejectsGarbage(t *testing.T) {
	if _, err := tokensFromBaseUnits("not-a-number", 6); err == nil {
		t.Fatal("expected an error for a non-numeric amount")
	}
}

func TestUSDValueRejectsUnknownMint(t *testing.T) {
	c := NewClient("", "unused")
	if _, _, err := c.USDValue("some-unlisted-mint", "1000000"); err == nil {
		t.Fatal("expected an error for a mint with no registered Pyth feed")
	}
}
