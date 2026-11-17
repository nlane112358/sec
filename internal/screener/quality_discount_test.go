package screener

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteQualityDiscountTable(t *testing.T) {
	candidates := []QualityDiscountCandidate{
		{
			Ticker:           "CRUS",
			Company:          "Cirrus Logic, Inc.",
			QualityScore:     76.1,
			ROIC:             0.23,
			HasROIC:          true,
			AdjROIC:          0.147,
			HasAdjROIC:       true,
			Price:            121.71,
			DCFValue:         415.77,
			Discount:         0.707,
			Upside:           2.416,
			FCFYield:         0.103,
			EV_FCF:           8.4,
			ImpliedGrowth:    -0.045,
			HistoricalGrowth: 0.152,
			HasImpliedGrowth: true,
			AlphaSpread:      0.197,
			QAADScore:        84.8,
		},
		{
			Ticker:           "POWL",
			Company:          "Powell Industries Inc",
			QualityScore:     75.0,
			ROIC:             0.691,
			HasROIC:          true,
			Price:            141.77,
			DCFValue:         273.19,
			Discount:         0.481,
			Upside:           0.927,
			FCFYield:         0.091,
			EV_FCF:           8.1,
			ImpliedGrowth:    0.02,
			HistoricalGrowth: 0.20,
			HasImpliedGrowth: true,
			AlphaSpread:      0.18,
			QAADScore:        78.5,
		},
	}

	var buf bytes.Buffer
	err := WriteQualityDiscountTable(&buf, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "CRUS") {
		t.Errorf("expected table to contain CRUS, got:\n%s", output)
	}
	if !strings.Contains(output, "POWL") {
		t.Errorf("expected table to contain POWL, got:\n%s", output)
	}
	if !strings.Contains(output, "QAAD SCORE") {
		t.Errorf("expected table header to contain QAAD SCORE, got:\n%s", output)
	}
	if !strings.Contains(output, "+70.7%") {
		t.Errorf("expected table to contain discount +70.7%%, got:\n%s", output)
	}
}
