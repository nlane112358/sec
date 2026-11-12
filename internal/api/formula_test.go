package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sec/internal/fundamentals"
)

func TestFormulaEndpoints(t *testing.T) {
	srv, err := NewServer("../../data/sec.db", "", nil)
	if err != nil {
		t.Skip("skipping integration test without local sec.db")
		return
	}
	defer srv.Close()

	handler := srv.Handler()

	// 1. Test /api/sec/MSFT/ADJ_ROIC?raw=1
	t.Run("MSFT ADJ_ROIC raw", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sec/MSFT/ADJ_ROIC?raw=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		val, err := strconv.ParseFloat(body, 64)
		if err != nil {
			t.Fatalf("failed to parse float %q: %v", body, err)
		}
		if val <= 0 || val > 1 {
			t.Fatalf("expected MSFT ADJ_ROIC between 0 and 1, got %f", val)
		}
		t.Logf("MSFT ADJ_ROIC raw: %f (%.1f%%)", val, val*100)
	})

	// 2. Test /api/sec/AAPL/REVENUE?period=TTM
	t.Run("AAPL REVENUE text", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sec/AAPL/REVENUE?period=TTM", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		if !strings.Contains(body, "B") && !strings.Contains(body, "$") {
			t.Fatalf("expected formatted billions, got %s", body)
		}
		t.Logf("AAPL TTM Revenue: %s", body)
	})

	// 3. Test /api/sec/NVDA/FCF?format=json
	t.Run("NVDA FCF JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sec/NVDA/FCF?format=json", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var res struct {
			Ticker    string  `json:"ticker"`
			Metric    string  `json:"metric"`
			Value     float64 `json:"value"`
			Formatted string  `json:"formatted"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal json: %v", err)
		}
		if res.Ticker != "NVDA" || res.Value <= 0 {
			t.Fatalf("unexpected json result: %+v", res)
		}
		t.Logf("NVDA FCF JSON: %+v", res)
	})

	// 4. Test /api/sec/batch
	t.Run("Batch grid lookup", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sec/batch?tickers=AAPL,MSFT&metrics=REVENUE,ADJ_ROIC&period=TTM", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var res struct {
			Tickers   []string    `json:"tickers"`
			Metrics   []string    `json:"metrics"`
			Values    [][]float64 `json:"values"`
			Formatted [][]string  `json:"formatted"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal batch: %v", err)
		}
		if len(res.Values) != 2 || len(res.Values[0]) != 2 {
			t.Fatalf("expected 2x2 grid, got %v", res.Values)
		}
		t.Logf("Batch results: AAPL Rev=%s ROIC=%s | MSFT Rev=%s ROIC=%s",
			res.Formatted[0][0], res.Formatted[0][1], res.Formatted[1][0], res.Formatted[1][1])
	})

	// 5. Test /api/company/AAPL/summary?view=quarterly
	t.Run("Company summary quarterly view", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/summary?view=quarterly&quarters=6", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var res SummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal summary: %v", err)
		}
		if res.View != "quarterly" || len(res.Periods) != 6 {
			t.Fatalf("expected 6 quarterly periods with view=quarterly, got %s and len %d", res.View, len(res.Periods))
		}
		t.Logf("Quarterly summary periods: %v", res.Periods[0].Label)
	})

	// 6. Test /api/company/AAPL/summary?view=ttm
	t.Run("Company summary TTM view with inflections", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/summary?view=ttm", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var res SummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal summary: %v", err)
		}
		if res.View != "ttm" || len(res.Periods) == 0 || res.Periods[0].Label != "TTM" {
			t.Fatalf("expected TTM as period 0, got %+v", res.Periods)
		}
		if len(res.Inflections) == 0 {
			t.Fatalf("expected quarterly inflections in TTM response, got 0")
		}
		t.Logf("TTM Inflections: %d found. Revenue YoY=%.1f%%", len(res.Inflections), res.Inflections[0].YoYGrowth*100)
	})

	// 7. Test /api/universes
	t.Run("Universes endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/universes", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var items []UniverseItem
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("unmarshal universes: %v", err)
		}
		if len(items) == 0 {
			t.Fatalf("expected universe items, got 0")
		}
		t.Logf("Universes loaded: %d universes. First: %s (%d total, %d synced)", len(items), items[0].Name, items[0].Total, items[0].Synced)
	})

	// 8. Test /api/company/AAPL/quote
	t.Run("Company quote endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/quote", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var q QuoteResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
			t.Fatalf("unmarshal quote: %v", err)
		}
		if q.Ticker != "AAPL" {
			t.Fatalf("expected ticker AAPL, got %s", q.Ticker)
		}
		t.Logf("AAPL quote: Price=%.2f, 52W High=%.2f, 52W Low=%.2f", q.Price, q.High52, q.Low52)
	})

	// 9. Test Summary Values Provenance Enrichment
	t.Run("Summary Values Provenance", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/summary", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var summary SummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
			t.Fatalf("unmarshal summary: %v", err)
		}

		foundProvenance := false
		for _, m := range summary.Metrics {
			for _, v := range m.Values {
				if v.Present && v.Formula != "" {
					foundProvenance = true
					if m.Name == "Research & Development" && v.Concept != "" {
						t.Logf("Found R&D provenance: concept=%s form=%s filed=%s accession=%s formula=%s",
							v.Concept, v.Form, v.FilingDate, v.Accession, v.Formula)
					}
				}
			}
		}
		if !foundProvenance {
			t.Fatalf("expected summary values to contain provenance formula")
		}
	})

	// 10. Test /api/company/AAPL/filings/diff
	t.Run("Company filings diff endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/filings/diff?item=1A", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var diff fundamentals.FilingDiffResult
		if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
			t.Fatalf("unmarshal diff: %v", err)
		}
		if diff.Ticker != "AAPL" {
			t.Fatalf("expected ticker AAPL, got %s", diff.Ticker)
		}
		if diff.Item != "1A" {
			t.Fatalf("expected item 1A, got %s", diff.Item)
		}
		if len(diff.Available10KFilings) < 2 {
			t.Fatalf("expected at least 2 available 10-K filings, got %d", len(diff.Available10KFilings))
		}
		t.Logf("AAPL Item 1A Diff stats: added=%d deleted=%d modified=%d unchanged=%d similarity=%.1f%%",
			diff.Stats.AddedParagraphs, diff.Stats.DeletedParagraphs, diff.Stats.ModifiedParagraphs, diff.Stats.UnchangedParagraphs, diff.Stats.SimilarityPct)
	})

	// 10b. Test /api/company/AAPL/filings/diff for Item 7 (MD&A)
	t.Run("Company filings diff Item 7 endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/filings/diff?item=7", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var diff fundamentals.FilingDiffResult
		if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
			t.Fatalf("unmarshal diff: %v", err)
		}
		if diff.Item != "7" {
			t.Fatalf("expected item 7, got %s", diff.Item)
		}
		t.Logf("AAPL Item 7 Diff stats: added=%d deleted=%d modified=%d unchanged=%d similarity=%.1f%%",
			diff.Stats.AddedParagraphs, diff.Stats.DeletedParagraphs, diff.Stats.ModifiedParagraphs, diff.Stats.UnchangedParagraphs, diff.Stats.SimilarityPct)
	})

	// 11. Test /api/company/AAPL/filing-html
	t.Run("Company filing HTML proxy endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/company/AAPL/filing-html?concept=ResearchAndDevelopmentExpense", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, "<base href=") {
			t.Fatalf("expected HTML to contain injected <base href=> tag")
		}
		if !strings.Contains(body, "sec-provenance-highlight") {
			t.Fatalf("expected HTML to contain sec-provenance-highlight CSS/JS")
		}
		t.Logf("AAPL filing HTML served: length=%d bytes, contains highlight script", len(body))
	})
}
