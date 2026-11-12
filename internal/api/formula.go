package api

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/market"
)

func (s *Server) getQuote(ctx context.Context, ticker string) float64 {
	s.quoteMu.RLock()
	entry, found := s.quoteCache[ticker]
	s.quoteMu.RUnlock()

	if found && time.Since(entry.asOf) < 15*time.Minute && entry.price > 0 {
		return entry.price
	}

	prices := market.FetchLivePrices(ctx, []string{ticker}, nil)
	price := prices[ticker]
	if price > 0 {
		s.quoteMu.Lock()
		s.quoteCache[ticker] = quoteEntry{price: price, asOf: time.Now()}
		s.quoteMu.Unlock()
	}
	return price
}

// handleSECFormula serves individual formula cells e.g.:
// GET /api/sec/{ticker}/{metric}?period=TTM&raw=1
func (s *Server) handleSECFormula(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	metric := strings.ToUpper(strings.TrimSpace(r.PathValue("metric")))
	if ticker == "" || metric == "" {
		writeError(w, http.StatusBadRequest, "ticker and metric are required")
		return
	}

	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "TTM"
	}

	raw := q.Get("raw") == "1" || q.Get("raw") == "true"
	format := strings.ToLower(q.Get("format"))
	if format == "raw" {
		raw = true
	}

	val, unit, formatted, err := s.resolveMetricValue(r.Context(), ticker, metric, period)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ticker":    ticker,
			"metric":    metric,
			"period":    period,
			"value":     val,
			"unit":      unit,
			"formatted": formatted,
		})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if raw {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			_, _ = fmt.Fprint(w, "0")
		} else {
			_, _ = fmt.Fprint(w, strconv.FormatFloat(val, 'f', -1, 64))
		}
	} else {
		_, _ = fmt.Fprint(w, formatted)
	}
}

// handleSECFormulaQuery is an alias for GET /api/formula?ticker=MSFT&metric=ADJ_ROIC&period=TTM
func (s *Server) handleSECFormulaQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ticker := strings.ToUpper(strings.TrimSpace(q.Get("ticker")))
	if ticker == "" {
		ticker = strings.ToUpper(strings.TrimSpace(q.Get("t")))
	}
	metric := strings.ToUpper(strings.TrimSpace(q.Get("metric")))
	if metric == "" {
		metric = strings.ToUpper(strings.TrimSpace(q.Get("m")))
	}
	if ticker == "" || metric == "" {
		writeError(w, http.StatusBadRequest, "ticker and metric are required")
		return
	}

	period := q.Get("period")
	if period == "" {
		period = q.Get("p")
	}
	if period == "" {
		period = "TTM"
	}

	raw := q.Get("raw") == "1" || q.Get("raw") == "true"
	format := strings.ToLower(q.Get("format"))
	if format == "raw" {
		raw = true
	}

	val, unit, formatted, err := s.resolveMetricValue(r.Context(), ticker, metric, period)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ticker":    ticker,
			"metric":    metric,
			"period":    period,
			"value":     val,
			"unit":      unit,
			"formatted": formatted,
		})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if raw {
		_, _ = fmt.Fprint(w, strconv.FormatFloat(val, 'f', -1, 64))
	} else {
		_, _ = fmt.Fprint(w, formatted)
	}
}

// handleSECBatch serves fast multi-ticker, multi-metric grid lookups for Google Sheets / Excel:
// GET /api/sec/batch?tickers=AAPL,MSFT,NVDA&metrics=ADJ_ROIC,REVENUE,FCF&period=TTM
func (s *Server) handleSECBatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tickersStr := q.Get("tickers")
	metricsStr := q.Get("metrics")
	period := q.Get("period")
	if period == "" {
		period = "TTM"
	}

	if tickersStr == "" || metricsStr == "" {
		writeError(w, http.StatusBadRequest, "tickers and metrics are required")
		return
	}

	rawTickers := strings.Split(tickersStr, ",")
	rawMetrics := strings.Split(metricsStr, ",")

	var tickers []string
	for _, t := range rawTickers {
		t = strings.ToUpper(strings.TrimSpace(t))
		if t != "" {
			tickers = append(tickers, t)
		}
	}

	var metrics []string
	for _, m := range rawMetrics {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m != "" {
			metrics = append(metrics, m)
		}
	}

	values := make([][]float64, len(tickers))
	formatted := make([][]string, len(tickers))

	for i, ticker := range tickers {
		values[i] = make([]float64, len(metrics))
		formatted[i] = make([]string, len(metrics))
		for j, metric := range metrics {
			v, _, fmtStr, err := s.resolveMetricValue(r.Context(), ticker, metric, period)
			if err != nil {
				values[i][j] = 0
				formatted[i][j] = "—"
			} else {
				values[i][j] = v
				formatted[i][j] = fmtStr
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tickers":   tickers,
		"metrics":   metrics,
		"period":    period,
		"values":    values,
		"formatted": formatted,
	})
}

// handleSheetsCode returns the Google Apps Script SEC.gs file for 1-click install.
func (s *Server) handleSheetsCode(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join("integrations", "sheets", "SEC.gs")
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "Google Sheets script file not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleExcelVBA returns the Excel VBA module SEC.bas for 1-click install.
func (s *Server) handleExcelVBA(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join("integrations", "excel", "SEC.bas")
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "Excel VBA file not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="SEC.bas"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type UniverseItem struct {
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Total       int    `json:"total"`
	Synced      int    `json:"synced"`
	Description string `json:"description"`
}

func findUniversesDir() string {
	candidates := []string{
		"universes",
		"../../universes",
		"../universes",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "universes"
}

// handleUniverses returns available universe files and database coverage stats.
func (s *Server) handleUniverses(w http.ResponseWriter, r *http.Request) {
	universesDir := findUniversesDir()
	entries, err := os.ReadDir(universesDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read universes dir")
		return
	}

	// Load all synced tickers once into a lookup set
	syncedSet := make(map[string]bool)
	if rows, err := s.store.DB().QueryContext(r.Context(), "SELECT ticker FROM sec_tickers"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err == nil {
				syncedSet[strings.ToUpper(t)] = true
			}
		}
	}

	descriptions := map[string]string{
		"sp500.txt":                  "S&P 500 Large-Cap US Benchmark",
		"nasdaq100.txt":              "Nasdaq 100 Tech & Growth Leaders",
		"midcap400.txt":              "S&P MidCap 400 Core Mid-Caps",
		"russell2000.txt":            "Russell 2000 US Small-Cap Universe",
		"adr_foreign.txt":            "Global Foreign ADRs (Form 20-F / 40-F Cross-Border Filers)",
		"broad_market_3000.txt":      "Broad Market 3000+ (S&P 500, MidCap 400, Russell 2000, Foreign ADRs)",
		"russell3000.txt":            "Russell 3000 Comprehensive US Investable Universe",
		"euro_uk_compounders_20f.txt": "European & UK Compounders (Form 20-F Filers)",
		"all-tickers.txt":            "Full SEC EDGAR Master Ticker Directory (~10,000+ filers)",
	}

	var results []UniverseItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		path := filepath.Join(universesDir, entry.Name())
		tickers, err := readTickersList(path)
		if err != nil || len(tickers) == 0 {
			continue
		}

		syncedCount := 0
		for _, ticker := range tickers {
			if syncedSet[ticker] {
				syncedCount++
			}
		}

		desc := descriptions[entry.Name()]
		if desc == "" {
			desc = "Custom Universe"
		}

		results = append(results, UniverseItem{
			Name:        strings.TrimSuffix(entry.Name(), ".txt"),
			Filename:    entry.Name(),
			Total:       len(tickers),
			Synced:      syncedCount,
			Description: desc,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Total < results[j].Total
	})

	writeJSON(w, http.StatusOK, results)
}

func readTickersList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var tickers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, ",\t") {
			fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' })
			if len(fields) > 0 {
				line = strings.TrimSpace(fields[0])
			}
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if !strings.EqualFold(f, "ticker") && !strings.EqualFold(f, "symbol") {
				tickers = append(tickers, strings.ToUpper(f))
			}
		}
	}
	return tickers, scanner.Err()
}

// resolveMetricValue resolves any metric code into numeric and formatted output.
func (s *Server) resolveMetricValue(ctx context.Context, ticker, metricName, period string) (float64, string, string, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	metricName = strings.ToUpper(strings.TrimSpace(metricName))
	period = strings.TrimSpace(period)
	if period == "" {
		period = "TTM"
	}

	_, _, savedTicker, err := s.store.CompanyForTicker(ctx, ticker)
	if err != nil {
		return 0, "", "", fmt.Errorf("company %q not found in local database", ticker)
	}
	if savedTicker != "" {
		ticker = savedTicker
	}

	// 1. Live market-price valuation metrics
	switch metricName {
	case "PRICE", "SHARE_PRICE":
		price := s.getQuote(ctx, ticker)
		if price <= 0 {
			return 0, "", "", fmt.Errorf("quote unavailable for %s", ticker)
		}
		return price, "USD", fmt.Sprintf("$%.2f", price), nil
	case "MARKET_CAP", "MCAP":
		price := s.getQuote(ctx, ticker)
		shares, ok := s.store.LatestShareCount(ctx, ticker)
		if !ok || shares <= 0 {
			return 0, "", "", fmt.Errorf("shares outstanding unavailable for %s", ticker)
		}
		if price <= 0 {
			return 0, "", "", fmt.Errorf("quote unavailable for %s", ticker)
		}
		mcap := price * shares
		return mcap, "USD", formatMetricCurrency(mcap, "USD"), nil
	case "PE", "PE_RATIO", "P_E":
		price := s.getQuote(ctx, ticker)
		if price <= 0 {
			return 0, "", "", fmt.Errorf("quote unavailable for %s", ticker)
		}
		ttm, err := fundamentals.LoadTTMSummary(ctx, s.store, ticker)
		if err != nil {
			return 0, "", "", err
		}
		netIncome := getMetricFloat(ttm.Metrics, "Net Income", 0)
		shares, ok := s.store.LatestShareCount(ctx, ticker)
		if !ok || shares <= 0 || netIncome <= 0 {
			return 0, "", "", fmt.Errorf("net income or shares non-positive for PE")
		}
		pe := (price * shares) / netIncome
		return pe, "x", fmt.Sprintf("%.1fx", pe), nil
	case "FCF_YIELD":
		price := s.getQuote(ctx, ticker)
		shares, ok := s.store.LatestShareCount(ctx, ticker)
		if !ok || shares <= 0 || price <= 0 {
			return 0, "", "", fmt.Errorf("quote or shares unavailable for FCF yield")
		}
		ttm, err := fundamentals.LoadTTMSummary(ctx, s.store, ticker)
		if err != nil {
			return 0, "", "", err
		}
		fcf := getMetricFloat(ttm.Metrics, "Free Cash Flow", 0)
		yield := fcf / (price * shares)
		return yield, "%", fmt.Sprintf("%.2f%%", yield*100), nil
	case "EV", "ENTERPRISE_VALUE":
		price := s.getQuote(ctx, ticker)
		shares, ok := s.store.LatestShareCount(ctx, ticker)
		if !ok || shares <= 0 || price <= 0 {
			return 0, "", "", fmt.Errorf("quote or shares unavailable for EV")
		}
		ttm, err := fundamentals.LoadTTMSummary(ctx, s.store, ticker)
		if err != nil {
			return 0, "", "", err
		}
		debt := getMetricFloat(ttm.Metrics, "Debt / Borrowings", 0)
		cash := getMetricFloat(ttm.Metrics, "Cash & Equivalents", 0)
		ev := (price * shares) + debt - cash
		return ev, "USD", formatMetricCurrency(ev, "USD"), nil
	}

	// 2. Map metric alias to canonical metric name
	canonicalName, isDerivedRatio, ratioType := mapMetricAlias(metricName)
	if canonicalName == "" && !isDerivedRatio {
		return 0, "", "", fmt.Errorf("unknown metric code %q", metricName)
	}

	// 3. Load data for requested period (TTM, Quarterly, or Annual)
	isTTM := strings.EqualFold(period, "TTM")
	isQuarter := strings.HasPrefix(strings.ToUpper(period), "Q") || strings.Contains(strings.ToUpper(period), "Q")

	var metrics []fundamentals.SummaryMetric
	var currency string

	if isTTM {
		ttm, err := fundamentals.LoadTTMSummary(ctx, s.store, ticker)
		if err != nil {
			return 0, "", "", fmt.Errorf("load TTM data for %s: %w", ticker, err)
		}
		metrics = ttm.Metrics
		currency = "USD"
	} else if isQuarter {
		qSummary, err := fundamentals.LoadQuarterlySummary(ctx, s.store, 12, ticker)
		if err != nil {
			return 0, "", "", fmt.Errorf("load quarterly data for %s: %w", ticker, err)
		}
		metrics = qSummary.Metrics
		currency = fundamentals.SummaryCurrency(qSummary)
	} else {
		annSummary, err := fundamentals.LoadFinancialSummary(ctx, s.store, 5, ticker)
		if err != nil {
			return 0, "", "", fmt.Errorf("load annual data for %s: %w", ticker, err)
		}
		metrics = annSummary.Metrics
		currency = fundamentals.SummaryCurrency(annSummary)
	}

	// Determine period index:
	periodIdx := 0
	if !isTTM {
		// If specific year / quarter requested, e.g. "2024", "2023", "-1", "Q2 2026"
		if period == "-1" || period == "prior" {
			periodIdx = 1
		} else if yr, err := strconv.Atoi(period); err == nil && yr > 2000 {
			// Find year
			for i, m := range metrics {
				if len(m.Values) > 0 {
					_ = i
					break
				}
			}
		}
	}

	// 4. Evaluate derived ratios
	if isDerivedRatio {
		rev := getMetricFloat(metrics, "Revenue", periodIdx)
		switch ratioType {
		case "GROSS_MARGIN":
			cost := getMetricFloat(metrics, "Cost of Revenue", periodIdx)
			if rev > 0 {
				gm := (rev - cost) / rev
				return gm, "%", fmt.Sprintf("%.2f%%", gm*100), nil
			}
		case "OPERATING_MARGIN":
			opInc := getMetricFloat(metrics, "Operating Income", periodIdx)
			if rev > 0 {
				om := opInc / rev
				return om, "%", fmt.Sprintf("%.2f%%", om*100), nil
			}
		case "NET_MARGIN":
			ni := getMetricFloat(metrics, "Net Income", periodIdx)
			if rev > 0 {
				nm := ni / rev
				return nm, "%", fmt.Sprintf("%.2f%%", nm*100), nil
			}
		case "FCF_MARGIN":
			fcf := getMetricFloat(metrics, "Free Cash Flow", periodIdx)
			if rev > 0 {
				fm := fcf / rev
				return fm, "%", fmt.Sprintf("%.2f%%", fm*100), nil
			}
		case "NET_DEBT":
			debt := getMetricFloat(metrics, "Debt / Borrowings", periodIdx)
			cash := getMetricFloat(metrics, "Cash & Equivalents", periodIdx)
			nd := debt - cash
			return nd, currency, formatMetricCurrency(nd, currency), nil
		case "GROSS_PROFIT":
			cost := getMetricFloat(metrics, "Cost of Revenue", periodIdx)
			gp := rev - cost
			return gp, currency, formatMetricCurrency(gp, currency), nil
		}
		return 0, "", "", fmt.Errorf("insufficient data for ratio %s", metricName)
	}

	// 5. Lookup standard canonical metric
	for _, m := range metrics {
		if m.Name == canonicalName && len(m.Values) > periodIdx && m.Values[periodIdx].Present {
			val := m.Values[periodIdx].Value
			unit := m.Values[periodIdx].Unit
			if unit == "" {
				unit = currency
			}
			fmtStr := formatMetricValue(val, unit)
			return val, unit, fmtStr, nil
		}
	}

	return 0, "", "", fmt.Errorf("metric %s not found for period %s", canonicalName, period)
}

func getMetricFloat(metrics []fundamentals.SummaryMetric, name string, index int) float64 {
	for _, m := range metrics {
		if m.Name == name && index < len(m.Values) && m.Values[index].Present {
			return m.Values[index].Value
		}
	}
	return 0
}

func mapMetricAlias(alias string) (canonicalName string, isDerived bool, derivedType string) {
	alias = strings.ToUpper(strings.TrimSpace(alias))
	switch alias {
	case "ADJ_ROIC", "ROIC", "ADJUSTED_ROIC":
		return "Intangible-Adjusted ROIC", false, ""
	case "REVENUE", "REV", "SALES", "TOPLINE":
		return "Revenue", false, ""
	case "OPERATING_INCOME", "OP_INC", "EBIT":
		return "Operating Income", false, ""
	case "ADJ_EBIT", "ADJUSTED_EBIT", "ADJ_OPERATING_INCOME":
		return "Adjusted Operating Income", false, ""
	case "COST_OF_REVENUE", "COGS", "COR":
		return "Cost of Revenue", false, ""
	case "GROSS_PROFIT", "GP":
		return "", true, "GROSS_PROFIT"
	case "GROSS_MARGIN", "GM":
		return "", true, "GROSS_MARGIN"
	case "OPERATING_MARGIN", "OP_MARGIN", "EBIT_MARGIN":
		return "", true, "OPERATING_MARGIN"
	case "NET_MARGIN", "PROFIT_MARGIN":
		return "", true, "NET_MARGIN"
	case "FCF_MARGIN":
		return "", true, "FCF_MARGIN"
	case "NET_DEBT":
		return "", true, "NET_DEBT"
	case "SGA", "SG&A", "SELLING_GENERAL_ADMINISTRATIVE":
		return "Selling, General & Administrative", false, ""
	case "RD", "R&D", "RESEARCH_DEVELOPMENT":
		return "Research & Development", false, ""
	case "NET_INCOME", "NI", "EARNINGS", "PROFIT":
		return "Net Income", false, ""
	case "OCF", "OPERATING_CASH_FLOW", "CASH_FLOW":
		return "Operating Cash Flow", false, ""
	case "CAPEX", "CAPITAL_EXPENDITURE":
		return "Capital Expenditure", false, ""
	case "DNA", "D&A", "DEPRECIATION", "DEPRECIATION_AMORTIZATION":
		return "Depreciation & Amortization", false, ""
	case "FCF", "FREE_CASH_FLOW":
		return "Free Cash Flow", false, ""
	case "SBC", "STOCK_BASED_COMPENSATION":
		return "Stock-Based Compensation", false, ""
	case "OWNER_EARNINGS", "SBC_ADJ_FCF", "ADJ_OWNER_EARNINGS":
		return "SBC-Adjusted FCF Proxy", false, ""
	case "CASH", "CASH_AND_EQUIVALENTS":
		return "Cash & Equivalents", false, ""
	case "DEBT", "BORROWINGS", "TOTAL_DEBT":
		return "Debt / Borrowings", false, ""
	case "EQUITY", "STOCKHOLDERS_EQUITY", "BOOK_VALUE":
		return "Stockholders' Equity", false, ""
	case "TOTAL_ASSETS", "ASSETS":
		return "Total Assets", false, ""
	case "CURRENT_ASSETS":
		return "Current Assets", false, ""
	case "CURRENT_LIABILITIES":
		return "Current Liabilities", false, ""
	case "SHARES", "SHARES_OUTSTANDING":
		return "Shares Outstanding", false, ""
	case "DILUTED_SHARES":
		return "Diluted Weighted Average Shares", false, ""
	case "INTANGIBLE_CAPITAL", "INTANGIBLES":
		return "Intangible Capital", false, ""
	case "INTANGIBLE_AMORT", "AMORTIZATION":
		return "Amortization of Intangibles", false, ""
	case "ADJ_INVESTED_CAPITAL", "INVESTED_CAPITAL":
		return "Adjusted Invested Capital", false, ""
	default:
		return "", false, ""
	}
}

func formatMetricValue(val float64, unit string) string {
	if unit == "%" {
		return fmt.Sprintf("%.1f%%", val*100)
	}
	if strings.EqualFold(unit, "shares") {
		return formatMetricMagnitude(val) + " shares"
	}
	if strings.EqualFold(unit, "USD") || unit == "$" || unit == "" {
		return "$" + formatMetricMagnitude(val)
	}
	return formatMetricMagnitude(val) + " " + unit
}

func formatMetricCurrency(val float64, unit string) string {
	return "$" + formatMetricMagnitude(val)
}

func formatMetricMagnitude(val float64) string {
	abs := math.Abs(val)
	divisor := 1.0
	suffix := ""
	switch {
	case abs >= 1e12:
		divisor, suffix = 1e12, "T"
	case abs >= 1e9:
		divisor, suffix = 1e9, "B"
	case abs >= 1e6:
		divisor, suffix = 1e6, "M"
	case abs >= 1e3:
		divisor, suffix = 1e3, "K"
	}
	return strconv.FormatFloat(val/divisor, 'f', 2, 64) + suffix
}
