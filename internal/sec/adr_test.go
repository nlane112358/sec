package sec

import (
	"math"
	"testing"
)

func TestADRRatioAndShareConversion(t *testing.T) {
	// AstraZeneca (AZN): 1 ADR = 0.5 Ordinary Share (2 ADRs = 1 Ord)
	aznRatio := ADRRatio("AZN")
	if aznRatio != 0.5 {
		t.Fatalf("expected AZN ADR ratio 0.5, got %v", aznRatio)
	}
	ordSharesAZN := 1.55e9
	adrSharesAZN := OrdinaryToADRShares("AZN", ordSharesAZN)
	if !closeEnough(adrSharesAZN, 3.10e9) {
		t.Fatalf("expected 3.10B ADR shares for AZN, got %v", adrSharesAZN)
	}

	// Shell (SHEL): 1 ADR = 2.0 Ordinary Shares
	shelRatio := ADRRatio("SHEL")
	if shelRatio != 2.0 {
		t.Fatalf("expected SHEL ADR ratio 2.0, got %v", shelRatio)
	}
	ordSharesSHEL := 6.4e9
	adrSharesSHEL := OrdinaryToADRShares("SHEL", ordSharesSHEL)
	if !closeEnough(adrSharesSHEL, 3.2e9) {
		t.Fatalf("expected 3.2B ADR shares for SHEL, got %v", adrSharesSHEL)
	}

	// Novo Nordisk (NVO): 1 ADR = 1.0 Ordinary B-Share
	nvoRatio := ADRRatio("NVO")
	if nvoRatio != 1.0 {
		t.Fatalf("expected NVO ADR ratio 1.0, got %v", nvoRatio)
	}

	// Unknown US ticker: defaults to 1.0
	unknownRatio := ADRRatio("AAPL")
	if unknownRatio != 1.0 {
		t.Fatalf("expected unknown ticker AAPL ADR ratio 1.0, got %v", unknownRatio)
	}
}

func TestResolveMacroProfiles(t *testing.T) {
	eu := ResolveMacroProfile("EU")
	if eu.Code != "EU" || eu.RiskFreeRate != 2.40 || eu.TaxRate != 25.0 {
		t.Fatalf("unexpected EU profile: %+v", eu)
	}

	uk := ResolveMacroProfile("GBP")
	if uk.Code != "UK" || uk.RiskFreeRate != 4.30 || uk.TaxRate != 25.0 {
		t.Fatalf("unexpected UK profile from GBP: %+v", uk)
	}

	ch := ResolveMacroProfile("CHF")
	if ch.Code != "CH" || ch.RiskFreeRate != 0.60 {
		t.Fatalf("unexpected CH profile from CHF: %+v", ch)
	}

	us := ResolveMacroProfile("USD")
	if us.Code != "US" || us.RiskFreeRate != 4.84 || us.TaxRate != 21.0 {
		t.Fatalf("unexpected US profile from USD: %+v", us)
	}
}

func TestFXConversion(t *testing.T) {
	// 100 EUR -> USD
	usd := ToUSD(100, "EUR")
	if usd <= 100 {
		t.Fatalf("expected >100 USD for 100 EUR, got %v", usd)
	}

	// Convert back from USD to EUR
	eur := FromUSD(usd, "EUR")
	if math.Abs(eur-100) > 0.001 {
		t.Fatalf("expected roundtrip to EUR to equal 100, got %v", eur)
	}

	// USD to USD is identity
	if ToUSD(100, "USD") != 100 {
		t.Fatalf("USD identity failed")
	}
}

func TestParseValuationNumberMultiCurrency(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{"$100.50", 100.50},
		{"€85.20", 85.20},
		{"£120.00", 120.00},
		{"EUR 50M", 50e6},
		{"GBP 2.5B", 2.5e9},
		{"100K", 100e3},
	}
	for _, c := range cases {
		got, ok := parseValuationNumber(c.input)
		if !ok || !closeEnough(got, c.want) {
			t.Errorf("parseValuationNumber(%q) = %v (ok=%v), want %v", c.input, got, ok, c.want)
		}
	}
}
