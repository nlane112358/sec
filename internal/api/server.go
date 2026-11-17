package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sec/internal/edgar"
	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
	"sec/internal/valuation"
)

var secDocumentName = regexp.MustCompile(`^[A-Za-z0-9_.~%-]+$`)

type quoteEntry struct {
	price  float64
	high52 float64
	low52  float64
	asOf   time.Time
}

type cachedSummary struct {
	response SummaryResponse
	asOf     time.Time
	epoch    int64
}

type cachedFilings struct {
	items []FilingItem
	asOf  time.Time
}

type cachedDiff struct {
	result fundamentals.FilingDiffResult
	asOf   time.Time
}

type Server struct {
	storePath      string
	store          *store.Store
	staticFS       fs.FS
	staticDir      string
	startTime      time.Time
	edgarClient    *edgar.Client
	filingCacheDir string
	rankMu         sync.RWMutex
	cachedRank     *screener.RankResult
	rankTime       time.Time
	rankEpoch      int64
	statsMu        sync.RWMutex
	cachedStats    *StatsResponse
	statsTime      time.Time
	quoteMu        sync.RWMutex
	quoteCache     map[string]quoteEntry
	gaapArbMu      sync.RWMutex
	cachedArb      []screener.ModernGAAPCandidate
	arbTime        time.Time
	arbEpoch       int64
	ownerYieldMu   sync.RWMutex
	cachedYield    *screener.OwnerYieldResult
	yieldTime      time.Time
	yieldEpoch     int64
	qaadMu         sync.RWMutex
	cachedQAAD     []screener.QualityDiscountCandidate
	qaadTime       time.Time
	qaadEpoch      int64
	summaryMu      sync.RWMutex
	summaryCache   map[string]cachedSummary
	filingsMu      sync.RWMutex
	filingsCache   map[string]cachedFilings
	diffMu         sync.RWMutex
	diffCache      map[string]cachedDiff
}

func NewServer(storePath string, staticDir string, staticFS fs.FS) (*Server, error) {
	ctx := context.Background()
	s, err := store.Open(ctx, storePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store %s: %w", storePath, err)
	}
	ua := os.Getenv("SEC_USER_AGENT")
	if strings.TrimSpace(ua) == "" {
		ua = "SECAnalysisApp research@secanalysis.local"
	}
	filingDir := filepath.Join(filepath.Dir(storePath), "filings")
	_ = os.MkdirAll(filingDir, 0755)

	srv := &Server{
		storePath:      storePath,
		store:          s,
		staticFS:       staticFS,
		staticDir:      staticDir,
		startTime:      time.Now(),
		edgarClient:    edgar.NewClient(ua),
		filingCacheDir: filingDir,
		quoteCache:     make(map[string]quoteEntry),
		summaryCache:   make(map[string]cachedSummary),
		filingsCache:   make(map[string]cachedFilings),
		diffCache:      make(map[string]cachedDiff),
	}

	// Initialize stats quickly using max(rowid) instead of full table scans
	var totalCompanies, totalFilings, totalFacts int
	_ = s.DB().QueryRow("SELECT count(*) FROM sec_tickers").Scan(&totalCompanies)
	_ = s.DB().QueryRow("SELECT max(rowid) FROM sec_filings").Scan(&totalFilings)
	_ = s.DB().QueryRow("SELECT max(rowid) FROM sec_facts").Scan(&totalFacts)
	srv.cachedStats = &StatsResponse{
		TotalCompanies: totalCompanies,
		TotalFilings:   totalFilings,
		TotalFacts:     totalFacts,
		DatabasePath:   storePath,
	}

	// Warm screener ranking & GAAP arbitrage cache in background
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		epochBefore, _ := s.CacheEpoch(bgCtx)
		if res, err := screener.RankCompanies(bgCtx, s, 5, 0, nil, false); err == nil {
			epochNow, _ := s.CacheEpoch(bgCtx)
			if epochNow < epochBefore {
				epochNow = epochBefore
			}
			srv.rankMu.Lock()
			srv.cachedRank = &res
			srv.rankTime = time.Now()
			srv.rankEpoch = epochNow
			srv.rankMu.Unlock()
		}

		pricesPath := filepath.Join(filepath.Dir(storePath), "prices.csv")
		marketCaps, prices, _ := market.LoadValuationInputs(pricesPath)

		filter := screener.ModernGAAPFilter{
			MinAdjustedROIC:      0.10,
			MinRevenue:           100_000_000,
			Years:                5,
			RequireSurging:       false,
			RequireGAAPExpensive: false,
			SortBy:               "spread",
		}
		if arbRes, err := screener.ScreenModernGAAPArbitrage(bgCtx, s, nil, marketCaps, prices, filter); err == nil {
			epochNow, _ := s.CacheEpoch(bgCtx)
			srv.gaapArbMu.Lock()
			srv.cachedArb = arbRes
			srv.arbTime = time.Now()
			srv.arbEpoch = epochNow
			srv.gaapArbMu.Unlock()
		}

		if yieldRes, err := screener.ScreenOwnerYield(bgCtx, s, nil, marketCaps, prices, screener.OwnerYieldFilter{
			Years:      5,
			MinRevenue: 50_000_000,
		}); err == nil {
			epochNow, _ := s.CacheEpoch(bgCtx)
			srv.ownerYieldMu.Lock()
			srv.cachedYield = &yieldRes
			srv.yieldTime = time.Now()
			srv.yieldEpoch = epochNow
			srv.ownerYieldMu.Unlock()
		}
	}()

	return srv, nil
}

func (s *Server) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/companies", s.handleCompanies)
	mux.HandleFunc("GET /api/company/{ticker}/summary", s.handleCompanySummary)
	mux.HandleFunc("GET /api/company/{ticker}/dcf", s.handleCompanyDCF)
	mux.HandleFunc("GET /api/company/{ticker}/reverse-dcf", s.handleCompanyReverseDCF)
	mux.HandleFunc("GET /api/company/{ticker}/filings", s.handleCompanyFilings)
	mux.HandleFunc("GET /api/company/{ticker}/filing-html", s.handleFilingHTML)
	mux.HandleFunc("GET /api/company/{ticker}/filings/diff", s.handleFilingsDiff)
	mux.HandleFunc("GET /api/company/{ticker}/quote", s.handleCompanyQuote)
	mux.HandleFunc("GET /api/company/{ticker}/audit", s.handleCompanyAudit)
	mux.HandleFunc("GET /api/screener/rank", s.handleScreenerRank)
	mux.HandleFunc("GET /api/screener/owner-yield", s.handleScreenerOwnerYield)
	mux.HandleFunc("GET /api/screener/gaap-arbitrage", s.handleScreenerGAAPArbitrage)
	mux.HandleFunc("GET /api/screener/discount", s.handleScreenerQualityDiscount)
	mux.HandleFunc("GET /api/screener/quality-discount", s.handleScreenerQualityDiscount)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/universes", s.handleUniverses)

	// Excel & Google Sheets Add-in formula endpoints
	mux.HandleFunc("GET /api/sec/{ticker}/{metric}", s.handleSECFormula)
	mux.HandleFunc("GET /api/sec/batch", s.handleSECBatch)
	mux.HandleFunc("GET /api/formula", s.handleSECFormulaQuery)
	mux.HandleFunc("GET /api/integrations/sheets/code", s.handleSheetsCode)
	mux.HandleFunc("GET /api/integrations/excel/vba", s.handleExcelVBA)

	// Static files / SPA fallback
	mux.HandleFunc("GET /", s.handleSPA)

	// Chain institutional middleware pipeline:
	// 1. recovery (top-level panic barrier)
	// 2. logging (latency & request metrics)
	// 3. cors (cross-origin headers & preflight)
	// 4. cacheControl (conditional cache policies)
	// 5. gzip (transparent HTTP compression)
	// 6. etag (SHA-256 entity hashing & 304 Not Modified)
	var handler http.Handler = mux
	handler = etagMiddleware(handler)
	handler = gzipMiddleware(handler)
	handler = cacheControlMiddleware(handler)
	handler = corsMiddleware(handler)
	handler = loggingMiddleware(handler)
	handler = recoveryMiddleware(handler)

	return handler
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type CompanyItem struct {
	CIK            string `json:"cik"`
	Ticker         string `json:"ticker"`
	Name           string `json:"name"`
	SIC            string `json:"sic"`
	SICDescription string `json:"sic_description"`
}

func (s *Server) handleCompanies(w http.ResponseWriter, r *http.Request) {
	companies, err := s.store.Companies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	sicFilter := strings.TrimSpace(r.URL.Query().Get("sic"))
	limitStr := r.URL.Query().Get("limit")

	limit := len(companies)
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l < limit {
			limit = l
		}
	}

	results := make([]CompanyItem, 0, len(companies))
	for _, c := range companies {
		if sicFilter != "" && c.SIC != sicFilter {
			continue
		}
		if query != "" {
			tMatch := strings.Contains(strings.ToLower(c.Ticker), query)
			nMatch := strings.Contains(strings.ToLower(c.Name), query)
			sMatch := strings.Contains(strings.ToLower(c.SICDescription), query)
			if !tMatch && !nMatch && !sMatch {
				continue
			}
		}
		results = append(results, CompanyItem{
			CIK:            c.CIK,
			Ticker:         c.Ticker,
			Name:           c.Name,
			SIC:            c.SIC,
			SICDescription: c.SICDescription,
		})
		if len(results) >= limit && query == "" && sicFilter == "" {
			break
		}
	}

	writeJSON(w, http.StatusOK, results)
}

type SummaryResponse struct {
	CIK            string                             `json:"cik"`
	Ticker         string                             `json:"ticker"`
	Company        string                             `json:"company"`
	SIC            string                             `json:"sic"`
	SICDescription string                             `json:"sic_description"`
	View           string                             `json:"view,omitempty"`
	Periods        []fundamentals.SummaryPeriod       `json:"periods"`
	Metrics        []fundamentals.SummaryMetric       `json:"metrics"`
	Intangibles    fundamentals.IntangiblesResult     `json:"intangibles"`
	Inflections    []fundamentals.QuarterlyInflection `json:"inflections,omitempty"`
}

func (s *Server) handleCompanySummary(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	years := 5
	if yStr := r.URL.Query().Get("years"); yStr != "" {
		if y, err := strconv.Atoi(yStr); err == nil && y >= 3 && y <= 10 {
			years = y
		}
	}

	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	quarters := 8
	if qStr := r.URL.Query().Get("quarters"); qStr != "" {
		if q, err := strconv.Atoi(qStr); err == nil && q >= 1 && q <= 16 {
			quarters = q
		}
	}

	dbEpoch, _ := s.store.CacheEpoch(r.Context())
	cacheKey := fmt.Sprintf("%s:%s:%d:%d", ticker, view, years, quarters)
	s.summaryMu.RLock()
	cached, found := s.summaryCache[cacheKey]
	s.summaryMu.RUnlock()
	if found && time.Since(cached.asOf) < 10*time.Minute && cached.epoch == dbEpoch {
		writeJSON(w, http.StatusOK, cached.response)
		return
	}

	cik, name, savedTicker, err := s.store.CompanyForTicker(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("company not found for ticker %s", ticker))
		return
	}
	if savedTicker != "" {
		ticker = savedTicker
	}

	var sic, sicDesc string
	_ = s.store.DB().QueryRowContext(r.Context(), "SELECT sic, sic_description FROM sec_companies WHERE cik = ?", cik).Scan(&sic, &sicDesc)

	var summary fundamentals.FinancialSummary
	var inflections []fundamentals.QuarterlyInflection

	switch view {
	case "quarterly", "q":
		summary, err = fundamentals.LoadQuarterlySummary(r.Context(), s.store, quarters, ticker)
	case "ttm":
		summary, err = fundamentals.LoadTTMFinancialSummary(r.Context(), s.store, ticker)
		if ttmDetails, errTTM := fundamentals.LoadTTMSummary(r.Context(), s.store, ticker); errTTM == nil {
			inflections = ttmDetails.Inflections
		}
	default:
		view = "annual"
		summary, err = fundamentals.LoadFinancialSummary(r.Context(), s.store, years, ticker)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load financial summary: %v", err))
		return
	}

	if len(summary.Periods) == 0 && s.edgarClient != nil {
		syncCtx, syncCancel := context.WithTimeout(r.Context(), 15*time.Second)
		factsData, errFacts := s.edgarClient.Facts(syncCtx, cik)
		syncCancel()
		if errFacts == nil && factsData != nil {
			facts := edgar.ExtractFacts(factsData, cik)
			if len(facts) > 0 {
				_ = s.store.SaveFacts(r.Context(), ticker, facts)
				subCtx, subCancel := context.WithTimeout(r.Context(), 10*time.Second)
				if sub, errSub := s.edgarClient.Submissions(subCtx, cik); errSub == nil {
					filings := sub.AllFilings(cik)
					_ = s.store.SaveFilings(r.Context(), filings)
					compName := sub.Name
					if strings.TrimSpace(compName) == "" {
						compName = factsData.Entity
					}
					_ = s.store.SaveCompany(r.Context(), cik, compName, ticker, string(sub.SIC), sub.SICDescription)
					_ = s.store.DB().QueryRowContext(r.Context(), "SELECT sic, sic_description FROM sec_companies WHERE cik = ?", cik).Scan(&sic, &sicDesc)
					name = compName
				}
				subCancel()

				_ = screener.PrecomputeCompany(r.Context(), s.store, store.LocalCompany{
					CIK:            cik,
					Name:           name,
					Ticker:         ticker,
					SIC:            sic,
					SICDescription: sicDesc,
				})
				dbEpoch, _ = s.store.CacheEpoch(r.Context())
				s.invalidateScreenerCaches()

				switch view {
				case "quarterly", "q":
					summary, _ = fundamentals.LoadQuarterlySummary(r.Context(), s.store, quarters, ticker)
				case "ttm":
					summary, _ = fundamentals.LoadTTMFinancialSummary(r.Context(), s.store, ticker)
					if ttmDetails, errTTM := fundamentals.LoadTTMSummary(r.Context(), s.store, ticker); errTTM == nil {
						inflections = ttmDetails.Inflections
					}
				default:
					summary, _ = fundamentals.LoadFinancialSummary(r.Context(), s.store, years, ticker)
				}
			}
		}
	}

	intangibles := fundamentals.CalculateIntangibles(summary)

	res := SummaryResponse{
		CIK:            cik,
		Ticker:         ticker,
		Company:        name,
		SIC:            sic,
		SICDescription: sicDesc,
		View:           view,
		Periods:        summary.Periods,
		Metrics:        summary.Metrics,
		Intangibles:    intangibles,
		Inflections:    inflections,
	}

	s.summaryMu.Lock()
	s.summaryCache[cacheKey] = cachedSummary{
		response: res,
		asOf:     time.Now(),
		epoch:    dbEpoch,
	}
	s.summaryMu.Unlock()

	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCompanyDCF(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	q := r.URL.Query()
	modeStr := q.Get("mode")
	mode := valuation.DCFModeFCF
	if modeStr == "revenue" {
		mode = valuation.DCFModeRevenue
	}

	growthProvided := false
	var growthRate float64
	if gStr := q.Get("growth"); gStr != "" {
		if g, err := strconv.ParseFloat(gStr, 64); err == nil {
			if g > 1 || g < -1 {
				growthRate = g / 100.0
			} else {
				growthRate = g
			}
			growthProvided = true
		}
	}

	discountRate := 0.0
	discountProvided := false
	if dStr := q.Get("discount"); dStr != "" {
		if d, err := strconv.ParseFloat(dStr, 64); err == nil && d > 0 {
			if d > 1 {
				discountRate = d / 100.0
			} else {
				discountRate = d
			}
			discountProvided = true
		}
	}

	terminalRate := 0.025
	if tStr := q.Get("terminal"); tStr != "" {
		if t, err := strconv.ParseFloat(tStr, 64); err == nil {
			if t > 1 {
				terminalRate = t / 100.0
			} else {
				terminalRate = t
			}
		}
	}

	years := 5
	if yStr := q.Get("years"); yStr != "" {
		if y, err := strconv.Atoi(yStr); err == nil && y >= 1 && y <= 20 {
			years = y
		}
	}

	normalizeCapEx := false
	if cMode := q.Get("capex_mode"); cMode == "normalized" || q.Get("normalize_capex") == "true" {
		normalizeCapEx = true
	}

	var debtOverride *float64
	debtProvided := false
	if dStr := q.Get("debt"); dStr != "" {
		if d, err := strconv.ParseFloat(dStr, 64); err == nil {
			debtOverride = &d
			debtProvided = true
		}
	}

	marketCap := 0.0
	if mStr := q.Get("market_cap"); mStr != "" {
		if m, err := strconv.ParseFloat(mStr, 64); err == nil {
			marketCap = m
		}
	}

	beta := 1.0
	if bStr := q.Get("beta"); bStr != "" {
		if b, err := strconv.ParseFloat(bStr, 64); err == nil && b > 0 {
			beta = b
		}
	}

	riskFree := 0.0484
	riskFreeProvided := false
	if rfStr := q.Get("risk_free"); rfStr != "" {
		if rf, err := strconv.ParseFloat(rfStr, 64); err == nil {
			if rf > 1 {
				riskFree = rf / 100.0
			} else {
				riskFree = rf
			}
			riskFreeProvided = true
		}
	}

	erp := 0.055
	if erpStr := q.Get("erp"); erpStr != "" {
		if ep, err := strconv.ParseFloat(erpStr, 64); err == nil {
			if ep > 1 {
				erp = ep / 100.0
			} else {
				erp = ep
			}
		}
	}

	summary, err := fundamentals.LoadFinancialSummary(r.Context(), s.store, 5, ticker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load summary for DCF: %v", err))
		return
	}

	profileKey := ""
	for _, m := range summary.Metrics {
		if m.Name == "Revenue" && len(m.Values) > 0 && m.Values[0].Present {
			profileKey = m.Values[0].Unit
			break
		}
	}
	profile := market.ResolveMacroProfile(profileKey)
	if !riskFreeProvided {
		riskFree = profile.RiskFreeRate / 100.0
	}

	waccConfig := &valuation.WACCConfig{
		RiskFreeRate:         riskFree,
		EquityRiskPremium:    erp,
		Beta:                 beta,
		MarketCap:            marketCap,
		CostOfDebt:           0.05,
		TaxRate:              profile.TaxRate / 100.0,
		DebtOverride:         debtOverride,
		UseEffectiveTaxRate:  true,
		UseEstimatedCostDebt: true,
	}

	config := valuation.DCFConfig{
		Mode:                mode,
		GrowthRate:          growthRate,
		UseHistoricalGrowth: !growthProvided,
		DiscountRate:        discountRate,
		TerminalRate:        terminalRate,
		Years:               years,
		NormalizeCapEx:      normalizeCapEx,
		DebtOverride:        debtOverride,
		UseEffectiveTaxRate: true,
		WACC:                waccConfig,
	}
	_ = discountProvided
	_ = debtProvided

	result, err := valuation.CalculateDCF(summary, config)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("calculate DCF: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCompanyReverseDCF(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	q := r.URL.Query()
	marketCap := 0.0
	if mStr := q.Get("market_cap"); mStr != "" {
		if m, err := strconv.ParseFloat(mStr, 64); err == nil {
			marketCap = m
		}
	}

	price := 0.0
	if pStr := q.Get("price"); pStr != "" {
		if p, err := strconv.ParseFloat(pStr, 64); err == nil && p > 0 {
			price = p
		}
	}

	mode := valuation.DCFModeOwnerEarnings
	if m := q.Get("mode"); m == "fcf" {
		mode = valuation.DCFModeFCF
	} else if m == "revenue" {
		mode = valuation.DCFModeRevenue
	}

	summary, err := fundamentals.LoadFinancialSummary(r.Context(), s.store, 5, ticker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load summary: %v", err))
		return
	}

	if marketCap <= 0 && price > 0 {
		var shares float64
		for _, m := range summary.Metrics {
			if (m.Name == "Shares Outstanding" || m.Name == "Diluted Weighted Average Shares") && len(m.Values) > 0 && m.Values[0].Present && m.Values[0].Value > 0 {
				shares = m.Values[0].Value
				break
			}
		}
		if shares > 0 {
			marketCap = price * shares
		}
	}

	if marketCap <= 0 {
		writeError(w, http.StatusBadRequest, "market_cap or price query parameter must be a positive number")
		return
	}

	terminalRate := 0.025
	if tStr := q.Get("terminal"); tStr != "" {
		if t, err := strconv.ParseFloat(tStr, 64); err == nil && t > 0 {
			if t > 1 {
				terminalRate = t / 100.0
			} else {
				terminalRate = t
			}
		}
	}

	years := 5
	if yStr := q.Get("years"); yStr != "" {
		if y, err := strconv.Atoi(yStr); err == nil && y >= 1 && y <= 20 {
			years = y
		}
	}

	discountRate := 0.0
	if dStr := q.Get("discount"); dStr != "" {
		if d, err := strconv.ParseFloat(dStr, 64); err == nil && d > 0 {
			if d > 1 {
				discountRate = d / 100.0
			} else {
				discountRate = d
			}
		}
	}

	profileKey := ""
	for _, m := range summary.Metrics {
		if m.Name == "Revenue" && len(m.Values) > 0 && m.Values[0].Present {
			profileKey = m.Values[0].Unit
			break
		}
	}
	profile := market.ResolveMacroProfile(profileKey)

	config := valuation.DCFConfig{
		Mode:                mode,
		TerminalRate:        terminalRate,
		Years:               years,
		DiscountRate:        discountRate,
		UseEffectiveTaxRate: true,
		WACC: &valuation.WACCConfig{
			MarketCap:            marketCap,
			RiskFreeRate:         profile.RiskFreeRate / 100.0,
			EquityRiskPremium:    0.055,
			Beta:                 1.0,
			CostOfDebt:           0.05,
			TaxRate:              profile.TaxRate / 100.0,
			UseEffectiveTaxRate:  true,
			UseEstimatedCostDebt: true,
		},
	}

	result, err := valuation.SolveReverseDCF(summary, config)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("solve reverse DCF: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}

type FilingItem struct {
	CIK             string `json:"cik"`
	Accession       string `json:"accession"`
	Form            string `json:"form"`
	FilingDate      string `json:"filing_date"`
	ReportDate      string `json:"report_date"`
	PrimaryDocument string `json:"primary_document"`
	DocumentTitle   string `json:"document_title"`
	URL             string `json:"url"`
}

func (s *Server) handleCompanyFilings(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	formFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("form")))

	cacheKey := fmt.Sprintf("%s:%s", ticker, formFilter)
	s.filingsMu.RLock()
	cached, found := s.filingsCache[cacheKey]
	s.filingsMu.RUnlock()
	if found && time.Since(cached.asOf) < 15*time.Minute {
		writeJSON(w, http.StatusOK, cached.items)
		return
	}

	filings, err := s.store.LoadFilings(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load filings: %v", err))
		return
	}

	items := make([]FilingItem, 0, len(filings))
	for _, f := range filings {
		if formFilter != "" && !strings.EqualFold(f.Form, formFilter) {
			continue
		}
		item := FilingItem{
			CIK:             f.CIK,
			Accession:       f.Accession,
			Form:            f.Form,
			FilingDate:      f.FilingDate,
			ReportDate:      f.ReportDate,
			PrimaryDocument: f.PrimaryDocument,
			DocumentTitle:   f.DocumentTitle,
		}
		item.URL = buildFilingURL(f)
		items = append(items, item)
	}

	s.filingsMu.Lock()
	s.filingsCache[cacheKey] = cachedFilings{
		items: items,
		asOf:  time.Now(),
	}
	s.filingsMu.Unlock()

	writeJSON(w, http.StatusOK, items)
}

func buildFilingURL(filing store.Filing) string {
	numericCIK, err := strconv.ParseUint(strings.TrimSpace(filing.CIK), 10, 64)
	if err != nil || numericCIK == 0 {
		return ""
	}
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(filing.Accession), "-", "")
	if len(accessionDigits) != 18 {
		return ""
	}
	primaryDoc := path.Clean(strings.TrimSpace(filing.PrimaryDocument))
	if primaryDoc == "." || primaryDoc == "" || strings.HasPrefix(primaryDoc, "..") || !secDocumentName.MatchString(primaryDoc) {
		return ""
	}
	return fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s", numericCIK, accessionDigits, url.PathEscape(primaryDoc))
}

func (s *Server) getFilingHTML(ctx context.Context, cik, accession, primaryDoc string) ([]byte, string, error) {
	numericCIK, err := strconv.ParseUint(strings.TrimSpace(cik), 10, 64)
	if err != nil || numericCIK == 0 {
		return nil, "", fmt.Errorf("invalid CIK: %s", cik)
	}
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(accession), "-", "")
	if len(accessionDigits) != 18 {
		return nil, "", fmt.Errorf("invalid accession number: %s", accession)
	}
	primaryDoc = path.Clean(strings.TrimSpace(primaryDoc))
	if primaryDoc == "." || primaryDoc == "" || strings.HasPrefix(primaryDoc, "..") || !secDocumentName.MatchString(primaryDoc) {
		return nil, "", fmt.Errorf("invalid primary document: %s", primaryDoc)
	}

	secURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s", numericCIK, accessionDigits, url.PathEscape(primaryDoc))
	cacheKey := fmt.Sprintf("%s_%s_%s", cik, accessionDigits, primaryDoc)
	cacheFile := filepath.Join(s.filingCacheDir, cacheKey)

	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		return data, secURL, nil
	}

	data, err := s.edgarClient.FetchDocument(ctx, secURL)
	if err != nil {
		return nil, secURL, fmt.Errorf("fetch SEC filing: %w", err)
	}

	_ = os.WriteFile(cacheFile, data, 0644)
	return data, secURL, nil
}

func (s *Server) handleFilingHTML(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	cik, _, savedTicker, err := s.store.CompanyForTicker(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	if savedTicker != "" {
		ticker = savedTicker
	}

	accession := strings.TrimSpace(r.URL.Query().Get("accession"))
	concept := strings.TrimSpace(r.URL.Query().Get("concept"))
	period := strings.TrimSpace(r.URL.Query().Get("period"))

	var filing store.Filing
	if accession != "" {
		filings, err := s.store.LoadFilings(r.Context(), ticker)
		if err == nil {
			for _, f := range filings {
				if strings.EqualFold(f.Accession, accession) {
					filing = f
					break
				}
			}
		}
	}

	if filing.Accession == "" {
		filings, err := s.store.LoadFilings(r.Context(), ticker)
		if err != nil || len(filings) == 0 {
			writeError(w, http.StatusNotFound, "no filings found for ticker")
			return
		}
		for _, f := range filings {
			if strings.EqualFold(f.Form, "10-K") || strings.EqualFold(f.Form, "20-F") {
				filing = f
				break
			}
		}
		if filing.Accession == "" {
			filing = filings[0]
		}
	}

	rawHTML, _, err := s.getFilingHTML(r.Context(), cik, filing.Accession, filing.PrimaryDocument)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("load filing HTML: %v", err))
		return
	}

	numericCIK, _ := strconv.ParseUint(strings.TrimSpace(cik), 10, 64)
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(filing.Accession), "-", "")
	baseURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/", numericCIK, accessionDigits)

	htmlStr := string(rawHTML)
	baseTag := fmt.Sprintf("<base href=\"%s\" target=\"_blank\">", baseURL)
	highlightInjection := fmt.Sprintf(`
<style>
.sec-provenance-highlight {
    background-color: rgba(245, 158, 11, 0.45) !important;
    outline: 3px solid #f59e0b !important;
    border-radius: 4px !important;
    box-shadow: 0 0 15px rgba(245, 158, 11, 0.6) !important;
    transition: all 0.3s ease-in-out !important;
}
.sec-provenance-row {
    background-color: rgba(245, 158, 11, 0.15) !important;
}
</style>
<script>
window.addEventListener('DOMContentLoaded', () => {
    const targetConcept = "%s";
    const targetPeriod = "%s";
    const cleanConcept = targetConcept.includes(':') ? targetConcept.split(':')[1] : targetConcept;
    let target = null;
    if (cleanConcept) {
        const query = "[name*='" + cleanConcept + "']";
        const matches = document.querySelectorAll(query);
        if (matches.length > 0) {
            target = matches[0];
            matches.forEach(el => {
                el.classList.add('sec-provenance-highlight');
                const tr = el.closest('tr');
                if (tr) tr.classList.add('sec-provenance-row');
            });
        }
    }
    if (!target && cleanConcept) {
        const words = cleanConcept.replace(/([A-Z])/g, ' $1').trim().toLowerCase();
        const cells = document.querySelectorAll('td, th, span');
        for (const cell of cells) {
            if (cell.textContent && cell.textContent.toLowerCase().includes(words)) {
                target = cell;
                cell.classList.add('sec-provenance-highlight');
                const tr = cell.closest('tr');
                if (tr) tr.classList.add('sec-provenance-row');
                break;
            }
        }
    }
    if (target) {
        setTimeout(() => {
            target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }, 300);
    }
});
</script>
`, templateEscape(concept), templateEscape(period))

	if idx := strings.Index(strings.ToLower(htmlStr), "<head>"); idx != -1 {
		htmlStr = htmlStr[:idx+6] + baseTag + highlightInjection + htmlStr[idx+6:]
	} else {
		htmlStr = baseTag + highlightInjection + htmlStr
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Del("X-Frame-Options")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(htmlStr))
}

func (s *Server) handleFilingsDiff(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	cik, _, savedTicker, err := s.store.CompanyForTicker(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	if savedTicker != "" {
		ticker = savedTicker
	}

	itemType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("item")))
	if itemType != "7" {
		itemType = "1A"
	}
	itemTitle := "Item 1A. Risk Factors"
	if itemType == "7" {
		itemTitle = "Item 7. Management's Discussion and Analysis"
	}

	currentAcc := strings.TrimSpace(r.URL.Query().Get("current"))
	priorAcc := strings.TrimSpace(r.URL.Query().Get("prior"))

	filings, err := s.store.LoadFilings(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load filings: %v", err))
		return
	}

	var tenKs []store.Filing
	for _, f := range filings {
		if strings.EqualFold(f.Form, "10-K") || strings.EqualFold(f.Form, "20-F") {
			tenKs = append(tenKs, f)
		}
	}
	if len(tenKs) < 2 {
		writeError(w, http.StatusBadRequest, "at least two 10-K filings are required to compute a redline diff")
		return
	}

	var currentFiling, priorFiling store.Filing
	if currentAcc != "" {
		for _, f := range tenKs {
			if strings.EqualFold(f.Accession, currentAcc) {
				currentFiling = f
				break
			}
		}
	}
	if currentFiling.Accession == "" {
		currentFiling = tenKs[0]
	}

	if priorAcc != "" {
		for _, f := range tenKs {
			if strings.EqualFold(f.Accession, priorAcc) {
				priorFiling = f
				break
			}
		}
	}
	if priorFiling.Accession == "" {
		for idx, f := range tenKs {
			if f.Accession == currentFiling.Accession && idx+1 < len(tenKs) {
				priorFiling = tenKs[idx+1]
				break
			}
		}
		if priorFiling.Accession == "" {
			priorFiling = tenKs[1]
		}
	}

	cacheKey := fmt.Sprintf("%s:%s:%s:%s", ticker, itemType, currentFiling.Accession, priorFiling.Accession)
	s.diffMu.RLock()
	cached, found := s.diffCache[cacheKey]
	s.diffMu.RUnlock()
	if found && time.Since(cached.asOf) < 30*time.Minute {
		writeJSON(w, http.StatusOK, cached.result)
		return
	}

	currentRaw, _, errCurr := s.getFilingHTML(r.Context(), cik, currentFiling.Accession, currentFiling.PrimaryDocument)
	if errCurr != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("load current 10-K HTML (%s): %v", currentFiling.Accession, errCurr))
		return
	}

	priorRaw, _, errPrior := s.getFilingHTML(r.Context(), cik, priorFiling.Accession, priorFiling.PrimaryDocument)
	if errPrior != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("load prior 10-K HTML (%s): %v", priorFiling.Accession, errPrior))
		return
	}

	currParagraphs := fundamentals.ExtractItemFromHTML(string(currentRaw), itemType)
	priorParagraphs := fundamentals.ExtractItemFromHTML(string(priorRaw), itemType)

	diffItems, stats := fundamentals.ComputeDiff(priorParagraphs, currParagraphs)

	available := make([]fundamentals.FilingVersion, len(tenKs))
	for i, f := range tenKs {
		year := ""
		if len(f.FilingDate) >= 4 {
			year = f.FilingDate[:4]
		}
		available[i] = fundamentals.FilingVersion{
			Accession:       f.Accession,
			FilingDate:      f.FilingDate,
			ReportDate:      f.ReportDate,
			Form:            f.Form,
			PrimaryDocument: f.PrimaryDocument,
			Year:            year,
		}
	}

	currYear := ""
	if len(currentFiling.FilingDate) >= 4 {
		currYear = currentFiling.FilingDate[:4]
	}
	priorYear := ""
	if len(priorFiling.FilingDate) >= 4 {
		priorYear = priorFiling.FilingDate[:4]
	}

	result := fundamentals.FilingDiffResult{
		Ticker:    ticker,
		Item:      itemType,
		ItemTitle: itemTitle,
		CurrentFiling: fundamentals.FilingVersion{
			Accession:       currentFiling.Accession,
			FilingDate:      currentFiling.FilingDate,
			ReportDate:      currentFiling.ReportDate,
			Form:            currentFiling.Form,
			PrimaryDocument: currentFiling.PrimaryDocument,
			Year:            currYear,
		},
		PriorFiling: fundamentals.FilingVersion{
			Accession:       priorFiling.Accession,
			FilingDate:      priorFiling.FilingDate,
			ReportDate:      priorFiling.ReportDate,
			Form:            priorFiling.Form,
			PrimaryDocument: priorFiling.PrimaryDocument,
			Year:            priorYear,
		},
		Available10KFilings: available,
		Stats:               stats,
		DiffItems:           diffItems,
	}

	s.diffMu.Lock()
	s.diffCache[cacheKey] = cachedDiff{
		result: result,
		asOf:   time.Now(),
	}
	s.diffMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}

func templateEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (s *Server) invalidateScreenerCaches() {
	s.rankMu.Lock()
	s.cachedRank = nil
	s.rankTime = time.Time{}
	s.rankMu.Unlock()

	s.gaapArbMu.Lock()
	s.cachedArb = nil
	s.arbTime = time.Time{}
	s.gaapArbMu.Unlock()

	s.qaadMu.Lock()
	s.cachedQAAD = nil
	s.qaadTime = time.Time{}
	s.qaadMu.Unlock()
}

func (s *Server) handleScreenerRank(w http.ResponseWriter, r *http.Request) {
	dbEpoch, _ := s.store.CacheEpoch(r.Context())
	s.rankMu.RLock()
	if s.cachedRank != nil && time.Since(s.rankTime) < 15*time.Minute && s.rankEpoch == dbEpoch {
		cached := s.cachedRank
		s.rankMu.RUnlock()
		writeJSON(w, http.StatusOK, cached)
		return
	}
	s.rankMu.RUnlock()

	s.rankMu.Lock()
	defer s.rankMu.Unlock()
	dbEpoch, _ = s.store.CacheEpoch(r.Context())
	// Double check
	if s.cachedRank != nil && time.Since(s.rankTime) < 15*time.Minute && s.rankEpoch == dbEpoch {
		writeJSON(w, http.StatusOK, s.cachedRank)
		return
	}

	// Calculate ranking
	result, err := screener.RankCompanies(r.Context(), s.store, 5, 0, nil, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("screener rank error: %v", err))
		return
	}
	epochAfter, _ := s.store.CacheEpoch(r.Context())
	if epochAfter > dbEpoch {
		dbEpoch = epochAfter
	}

	s.cachedRank = &result
	s.rankTime = time.Now()
	s.rankEpoch = dbEpoch
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleScreenerOwnerYield(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uVal := q.Get("universe")
	sortVal := q.Get("sort")
	sortDir := q.Get("sort_dir")
	minYieldVal := q.Get("min_yield")
	minROICVal := q.Get("min_roic")
	minRevVal := q.Get("min_revenue")
	hurdleVal := q.Get("hurdle")
	finVal := q.Get("include_financials")

	var universeTickers []string
	if uVal != "" {
		if strings.Contains(uVal, ",") {
			for _, part := range strings.Split(uVal, ",") {
				if t := strings.TrimSpace(part); t != "" {
					universeTickers = append(universeTickers, strings.ToUpper(t))
				}
			}
		} else {
			uDir := findUniversesDir()
			path := filepath.Join(uDir, uVal)
			if !strings.HasSuffix(path, ".txt") && !strings.Contains(uVal, ".") {
				path += ".txt"
			}
			if data, err := os.ReadFile(path); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						universeTickers = append(universeTickers, strings.ToUpper(line))
					}
				}
			} else {
				universeTickers = append(universeTickers, strings.ToUpper(uVal))
			}
		}
	}

	isDefaultQuery := len(universeTickers) == 0 &&
		minYieldVal == "" &&
		minROICVal == "" &&
		minRevVal == "" &&
		hurdleVal == "" &&
		finVal == "" &&
		(sortVal == "" || sortVal == "yield") &&
		(sortDir == "" || sortDir == "desc")

	dbEpoch, _ := s.store.CacheEpoch(r.Context())
	if isDefaultQuery {
		s.ownerYieldMu.RLock()
		if s.cachedYield != nil && time.Since(s.yieldTime) < 15*time.Minute && s.yieldEpoch == dbEpoch {
			cached := s.cachedYield
			s.ownerYieldMu.RUnlock()
			writeJSON(w, http.StatusOK, cached)
			return
		}
		s.ownerYieldMu.RUnlock()
	}

	minYield := 0.0
	if minYieldVal != "" {
		if v, err := strconv.ParseFloat(minYieldVal, 64); err == nil && v >= 0 {
			minYield = v / 100.0
		}
	}
	minROIC := 0.0
	if minROICVal != "" {
		if v, err := strconv.ParseFloat(minROICVal, 64); err == nil {
			minROIC = v / 100.0
		}
	}
	minRev := 50_000_000.0
	if minRevVal != "" {
		if v, err := strconv.ParseFloat(minRevVal, 64); err == nil && v >= 0 {
			minRev = v
		}
	}
	requireHurdle := strings.EqualFold(hurdleVal, "true") || hurdleVal == "1"
	includeFin := strings.EqualFold(finVal, "true") || finVal == "1"

	pricesPath := filepath.Join(filepath.Dir(s.storePath), "prices.csv")
	marketCaps, prices, _ := market.LoadValuationInputs(pricesPath)
	if prices == nil {
		prices = make(map[string]float64)
	}
	if marketCaps == nil {
		marketCaps = make(map[string]float64)
	}

	s.quoteMu.RLock()
	for t, qEntry := range s.quoteCache {
		if qEntry.price > 0 {
			prices[t] = qEntry.price
		}
	}
	s.quoteMu.RUnlock()

	filter := screener.OwnerYieldFilter{
		MinYield:          minYield,
		MinAdjustedROIC:   minROIC,
		MinRevenue:        minRev,
		Years:             5,
		RequireHurdleBeat: requireHurdle,
		IncludeFinancials: includeFin,
		SortBy:            sortVal,
		SortAsc:           strings.EqualFold(sortDir, "asc"),
	}

	result, err := screener.ScreenOwnerYield(r.Context(), s.store, universeTickers, marketCaps, prices, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("owner yield screen: %v", err))
		return
	}

	if isDefaultQuery {
		epochAfter, _ := s.store.CacheEpoch(r.Context())
		if epochAfter > dbEpoch {
			dbEpoch = epochAfter
		}
		s.ownerYieldMu.Lock()
		s.cachedYield = &result
		s.yieldTime = time.Now()
		s.yieldEpoch = dbEpoch
		s.ownerYieldMu.Unlock()
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleScreenerGAAPArbitrage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortBy := "spread"
	if sVal := q.Get("sort"); sVal != "" {
		sortBy = sVal
	}

	isDefaultQuery := sortBy == "spread"
	dbEpoch, _ := s.store.CacheEpoch(r.Context())
	if isDefaultQuery {
		s.gaapArbMu.RLock()
		if s.cachedArb != nil && time.Since(s.arbTime) < 15*time.Minute && s.arbEpoch == dbEpoch {
			cached := s.cachedArb
			s.gaapArbMu.RUnlock()
			writeJSON(w, http.StatusOK, map[string]any{"candidates": cached, "total": len(cached)})
			return
		}
		s.gaapArbMu.RUnlock()
	}

	pricesPath := filepath.Join(filepath.Dir(s.storePath), "prices.csv")
	marketCaps, prices, _ := market.LoadValuationInputs(pricesPath)
	if prices == nil {
		prices = make(map[string]float64)
	}
	if marketCaps == nil {
		marketCaps = make(map[string]float64)
	}

	s.quoteMu.RLock()
	for t, qEntry := range s.quoteCache {
		if qEntry.price > 0 {
			prices[t] = qEntry.price
		}
	}
	s.quoteMu.RUnlock()

	filter := screener.ModernGAAPFilter{
		MinAdjustedROIC:      0.10,
		MinRevenue:           100_000_000,
		Years:                5,
		RequireSurging:       false,
		RequireGAAPExpensive: false,
		SortBy:               sortBy,
	}

	candidates, err := screener.ScreenModernGAAPArbitrage(r.Context(), s.store, nil, marketCaps, prices, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("gaap arbitrage screen: %v", err))
		return
	}

	if isDefaultQuery {
		epochAfter, _ := s.store.CacheEpoch(r.Context())
		if epochAfter > dbEpoch {
			dbEpoch = epochAfter
		}
		s.gaapArbMu.Lock()
		s.cachedArb = candidates
		s.arbTime = time.Now()
		s.arbEpoch = dbEpoch
		s.gaapArbMu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates, "total": len(candidates)})
}

func (s *Server) handleScreenerQualityDiscount(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uVal := q.Get("universe")

	var universeTickers []string
	if uVal != "" {
		if strings.Contains(uVal, ",") {
			for _, part := range strings.Split(uVal, ",") {
				if t := strings.TrimSpace(part); t != "" {
					universeTickers = append(universeTickers, strings.ToUpper(t))
				}
			}
		} else {
			uDir := findUniversesDir()
			path := filepath.Join(uDir, uVal)
			if !strings.HasSuffix(path, ".txt") && !strings.Contains(uVal, ".") {
				path += ".txt"
			}
			if data, err := os.ReadFile(path); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						universeTickers = append(universeTickers, strings.ToUpper(line))
					}
				}
			} else {
				universeTickers = append(universeTickers, strings.ToUpper(uVal))
			}
		}
	}

	// For global screening (no universe filter), return cached QAAD if fresh
	dbEpoch, _ := s.store.CacheEpoch(r.Context())
	if len(universeTickers) == 0 {
		s.qaadMu.RLock()
		if s.cachedQAAD != nil && time.Since(s.qaadTime) < 15*time.Minute && s.qaadEpoch == dbEpoch {
			cached := s.cachedQAAD
			s.qaadMu.RUnlock()
			writeJSON(w, http.StatusOK, map[string]any{"candidates": cached, "total": len(cached)})
			return
		}
		s.qaadMu.RUnlock()
	}

	var companiesToScreen []screener.RankedCompany
	if len(universeTickers) > 0 {
		s.rankMu.RLock()
		cached := s.cachedRank
		rankEpoch := s.rankEpoch
		s.rankMu.RUnlock()
		if cached != nil && rankEpoch == dbEpoch {
			tickerMap := make(map[string]bool, len(universeTickers))
			for _, t := range universeTickers {
				tickerMap[t] = true
			}
			for _, c := range cached.Companies {
				if tickerMap[c.Ticker] {
					companiesToScreen = append(companiesToScreen, c)
				}
			}
		} else {
			ranked, err := screener.RankCompanies(r.Context(), s.store, 5, 0, universeTickers, false)
			if err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("screener rank error: %v", err))
				return
			}
			companiesToScreen = ranked.Companies
		}
	} else {
		s.qaadMu.Lock()
		defer s.qaadMu.Unlock()
		dbEpoch, _ = s.store.CacheEpoch(r.Context())
		if s.cachedQAAD != nil && time.Since(s.qaadTime) < 15*time.Minute && s.qaadEpoch == dbEpoch {
			writeJSON(w, http.StatusOK, map[string]any{"candidates": s.cachedQAAD, "total": len(s.cachedQAAD)})
			return
		}

		s.rankMu.RLock()
		rankResult := s.cachedRank
		rankEpoch := s.rankEpoch
		s.rankMu.RUnlock()
		if rankResult == nil || rankEpoch != dbEpoch {
			ranked, err := screener.RankCompanies(r.Context(), s.store, 5, 0, nil, false)
			if err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("screener rank error: %v", err))
				return
			}
			epochAfter, _ := s.store.CacheEpoch(r.Context())
			if epochAfter > dbEpoch {
				dbEpoch = epochAfter
			}
			s.rankMu.Lock()
			s.cachedRank = &ranked
			s.rankTime = time.Now()
			s.rankEpoch = dbEpoch
			s.rankMu.Unlock()
			rankResult = &ranked
		}
		companiesToScreen = rankResult.Companies
	}

	pricesPath := filepath.Join(filepath.Dir(s.storePath), "prices.csv")
	marketCaps, prices, _ := market.LoadValuationInputs(pricesPath)
	if prices == nil {
		prices = make(map[string]float64)
	}
	if marketCaps == nil {
		marketCaps = make(map[string]float64)
	}

	s.quoteMu.RLock()
	for t, q := range s.quoteCache {
		if q.price > 0 {
			prices[t] = q.price
		}
	}
	s.quoteMu.RUnlock()

	minScore := 65.0
	if sVal := q.Get("min_score"); sVal != "" {
		if v, err := strconv.ParseFloat(sVal, 64); err == nil && v >= 0 {
			minScore = v
		}
	}
	minROIC := 0.10
	if rVal := q.Get("min_roic"); rVal != "" {
		if v, err := strconv.ParseFloat(rVal, 64); err == nil {
			minROIC = v / 100.0
		}
	}
	minDiscount := 0.05
	if dVal := q.Get("min_discount"); dVal != "" {
		if v, err := strconv.ParseFloat(dVal, 64); err == nil {
			minDiscount = v / 100.0
		}
	}
	sortBy := "qaad"
	if sVal := q.Get("sort"); sVal != "" {
		sortBy = sVal
	}

	filter := screener.QualityDiscountFilter{
		MinScore:          minScore,
		MinROIC:           minROIC,
		MinDiscount:       minDiscount,
		SortBy:            sortBy,
		IncludeFinancials: false,
		DCFConfig: valuation.DCFConfig{
			Mode:         valuation.DCFModeFCF,
			TerminalRate: 0.025,
			Years:        5,
		},
	}

	candidates, err := screener.ScreenQualityDiscount(r.Context(), s.store, companiesToScreen, marketCaps, prices, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("quality discount screen error: %v", err))
		return
	}

	if len(universeTickers) == 0 {
		epochAfter, _ := s.store.CacheEpoch(r.Context())
		s.cachedQAAD = candidates
		s.qaadTime = time.Now()
		s.qaadEpoch = epochAfter
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates, "total": len(candidates)})
}

type QuoteResponse struct {
	Ticker   string  `json:"ticker"`
	Price    float64 `json:"price"`
	High52   float64 `json:"high_52,omitempty"`
	Low52    float64 `json:"low_52,omitempty"`
	Currency string  `json:"currency"`
	AsOf     string  `json:"as_of"`
}

func (s *Server) handleCompanyQuote(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	s.quoteMu.RLock()
	entry, found := s.quoteCache[ticker]
	s.quoteMu.RUnlock()

	if found && time.Since(entry.asOf) < 15*time.Minute {
		writeJSON(w, http.StatusOK, QuoteResponse{
			Ticker:   ticker,
			Price:    entry.price,
			High52:   entry.high52,
			Low52:    entry.low52,
			Currency: "USD",
			AsOf:     entry.asOf.Format(time.RFC3339),
		})
		return
	}

	quote, err := market.FetchLiveQuote(r.Context(), ticker)
	price := quote.Price
	if err != nil || price <= 0 {
		prices := market.FetchLivePrices(r.Context(), []string{ticker}, nil)
		price = prices[ticker]
		quote.Price = price
	}

	s.quoteMu.Lock()
	if price > 0 {
		s.quoteCache[ticker] = quoteEntry{
			price:  price,
			high52: quote.High52,
			low52:  quote.Low52,
			asOf:   time.Now(),
		}
	}
	s.quoteMu.Unlock()

	if price <= 0 && found {
		price = entry.price
		quote.High52 = entry.high52
		quote.Low52 = entry.low52
	}

	writeJSON(w, http.StatusOK, QuoteResponse{
		Ticker:   ticker,
		Price:    price,
		High52:   quote.High52,
		Low52:    quote.Low52,
		Currency: "USD",
		AsOf:     time.Now().Format(time.RFC3339),
	})
}

type RawFactItem struct {
	Taxonomy    string  `json:"taxonomy"`
	Concept     string  `json:"concept"`
	Label       string  `json:"label"`
	PeriodEnd   string  `json:"period_end"`
	Unit        string  `json:"unit"`
	Value       float64 `json:"value"`
	Form        string  `json:"form"`
	FilingDate  string  `json:"filing_date"`
	Accession   string  `json:"accession"`
}

type AuditResponse struct {
	CIK      string        `json:"cik"`
	Ticker   string        `json:"ticker"`
	Company  string        `json:"company"`
	RawFacts []RawFactItem `json:"raw_facts"`
}

func (s *Server) handleCompanyAudit(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.PathValue("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}

	cik, name, savedTicker, err := s.store.CompanyForTicker(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	if savedTicker != "" {
		ticker = savedTicker
	}

	rows, err := s.store.DB().QueryContext(r.Context(), `
		SELECT taxonomy, concept, label, period_end, unit, value, form, filing_date, accession_number
		FROM sec_facts
		WHERE cik = ? AND (form LIKE '10-K%' OR form LIKE '20-F%')
		ORDER BY period_end DESC, taxonomy, concept
		LIMIT 200`, cik)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("query facts: %v", err))
		return
	}
	defer rows.Close()

	var facts []RawFactItem
	for rows.Next() {
		var f RawFactItem
		if err := rows.Scan(&f.Taxonomy, &f.Concept, &f.Label, &f.PeriodEnd, &f.Unit, &f.Value, &f.Form, &f.FilingDate, &f.Accession); err == nil {
			facts = append(facts, f)
		}
	}

	writeJSON(w, http.StatusOK, AuditResponse{
		CIK:      cik,
		Ticker:   ticker,
		Company:  name,
		RawFacts: facts,
	})
}

type StatsResponse struct {
	TotalCompanies int    `json:"total_companies"`
	TotalFilings   int    `json:"total_filings"`
	TotalFacts     int    `json:"total_facts"`
	DatabasePath   string `json:"database_path"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.statsMu.RLock()
	stats := s.cachedStats
	statsTime := s.statsTime
	s.statsMu.RUnlock()

	// Short 1-second throttle to avoid hammering SQLite during high-frequency polling,
	// while ensuring numbers automatically update as new companies/facts are synced.
	if stats != nil && time.Since(statsTime) < 1*time.Second {
		writeJSON(w, http.StatusOK, stats)
		return
	}

	var totalCompanies, totalFilings, totalFacts int
	_ = s.store.DB().QueryRowContext(r.Context(), "SELECT count(*) FROM sec_tickers").Scan(&totalCompanies)
	_ = s.store.DB().QueryRowContext(r.Context(), "SELECT coalesce(max(rowid), 0) FROM sec_filings").Scan(&totalFilings)
	_ = s.store.DB().QueryRowContext(r.Context(), "SELECT coalesce(max(rowid), 0) FROM sec_facts").Scan(&totalFacts)

	newStats := &StatsResponse{
		TotalCompanies: totalCompanies,
		TotalFilings:   totalFilings,
		TotalFacts:     totalFacts,
		DatabasePath:   s.storePath,
	}

	s.statsMu.Lock()
	s.cachedStats = newStats
	s.statsTime = time.Now()
	s.statsMu.Unlock()

	writeJSON(w, http.StatusOK, newStats)
}

type HealthResponse struct {
	Status   string `json:"status"`
	Uptime   string `json:"uptime"`
	Database string `json:"database"`
	Stats    struct {
		Companies int `json:"companies"`
		Filings   int `json:"filings"`
		Facts     int `json:"facts"`
	} `json:"stats"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbStatus := "connected"
	if err := s.store.DB().PingContext(r.Context()); err != nil {
		dbStatus = "error: " + err.Error()
	}

	res := HealthResponse{
		Status:   "ok",
		Uptime:   time.Since(s.startTime).Truncate(time.Second).String(),
		Database: dbStatus,
	}
	s.statsMu.RLock()
	if s.cachedStats != nil {
		res.Stats.Companies = s.cachedStats.TotalCompanies
		res.Stats.Filings = s.cachedStats.TotalFilings
		res.Stats.Facts = s.cachedStats.TotalFacts
	}
	s.statsMu.RUnlock()

	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if s.staticFS != nil {
		f, err := s.staticFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			_ = f.Close()
			http.FileServer(http.FS(s.staticFS)).ServeHTTP(w, r)
			return
		}
		// Fallback to index.html for SPA client routes
		indexData, err := fs.ReadFile(s.staticFS, "index.html")
		if err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexData)
			return
		}
	}

	if s.staticDir != "" {
		filePath := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
		if stat, err := os.Stat(filePath); err == nil && !stat.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}
		indexPath := filepath.Join(s.staticDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	// Default fallback message if no frontend is built yet
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `<!doctype html><html><body style="background:#09090b;color:#e4e4e7;font-family:sans-serif;padding:40px;">
<h2>SEC EDGAR Backend Running</h2>
<p>API endpoints available under <code>/api/...</code></p>
<ul>
  <li><a style="color:#38bdf8" href="/api/companies">/api/companies</a></li>
  <li><a style="color:#38bdf8" href="/api/company/AAPL/summary">/api/company/AAPL/summary</a></li>
  <li><a style="color:#38bdf8" href="/api/company/AAPL/dcf">/api/company/AAPL/dcf</a></li>
  <li><a style="color:#38bdf8" href="/api/company/AAPL/filings">/api/company/AAPL/filings</a></li>
  <li><a style="color:#38bdf8" href="/api/stats">/api/stats</a></li>
</ul>
</body></html>`)
}
