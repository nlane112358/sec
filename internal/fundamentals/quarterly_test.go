package fundamentals

import (
	"context"
	"testing"

	"sec/internal/store"
)

func TestQuarterlyAndTTMLoad(t *testing.T) {
	s, err := store.Open(context.Background(), "../../data/sec.db")
	if err != nil {
		t.Skip("skipping integration test without local sec.db")
		return
	}
	defer s.Close()

	ctx := context.Background()
	for _, ticker := range []string{"AAPL", "MSFT", "NVDA"} {
		qSummary, err := LoadQuarterlySummary(ctx, s, 8, ticker)
		if err != nil {
			t.Fatalf("LoadQuarterlySummary failed for %s: %v", ticker, err)
		}

		if len(qSummary.Periods) == 0 {
			t.Fatalf("expected at least 1 quarterly period for %s, got 0", ticker)
		}

		t.Logf("[%s] Loaded %d quarters. Latest period: %s (%s)", ticker, len(qSummary.Periods), qSummary.Periods[0].Label, qSummary.Periods[0].End)

		ttm, err := LoadTTMSummary(ctx, s, ticker)
		if err != nil {
			t.Fatalf("LoadTTMSummary failed for %s: %v", ticker, err)
		}

		revM := summaryMetricValue(ttm.Metrics, "Revenue", 0)
		if !revM.Present || revM.Value <= 0 {
			t.Fatalf("expected positive TTM revenue for %s, got %v", ticker, revM)
		}
		t.Logf("[%s] TTM Revenue: %.2fB %s", ticker, revM.Value/1e9, revM.Unit)

		for _, inf := range ttm.Inflections {
			t.Logf("[%s] Inflection %s: YoY=%.1f%%, PriorYoY=%.1f%%, Accel=%v", ticker, inf.Metric, inf.YoYGrowth*100, inf.PriorYoYGrowth*100, inf.IsAccelerating)
		}
	}
}
