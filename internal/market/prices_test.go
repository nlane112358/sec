package market

import (
	"testing"
)

func TestYahooSymbolForTicker(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"AAPL", "AAPL"},
		{"BRK.B", "BRK-B"},
		{"BF.A", "BF-A"},
		{"VOD.L", "VOD.L"},
		{"MC.PA", "MC.PA"},
	}

	for _, tc := range tests {
		got := yahooSymbolForTicker(tc.input)
		if got != tc.expected {
			t.Errorf("yahooSymbolForTicker(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestLiveQuoteStruct(t *testing.T) {
	q := LiveQuote{
		Price:  180.5,
		High52: 200.0,
		Low52:  150.0,
	}
	if q.Price != 180.5 || q.High52 != 200.0 || q.Low52 != 150.0 {
		t.Errorf("unexpected quote values: %+v", q)
	}
}
