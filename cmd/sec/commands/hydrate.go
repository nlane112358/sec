package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sec/internal/edgar"
	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

const (
	quoteCacheTTL = 15 * time.Minute
	fxCacheTTL    = 60 * time.Minute
)

var (
	edgarFactoryMu     sync.RWMutex
	edgarClientFactory = func(userAgent string) *edgar.Client {
		return edgar.NewClient(userAgent)
	}
)

// SetEDGARClientFactoryForTesting overrides EDGAR client creation in tests.
func SetEDGARClientFactoryForTesting(fn func(userAgent string) *edgar.Client) func() {
	edgarFactoryMu.Lock()
	prev := edgarClientFactory
	if fn != nil {
		edgarClientFactory = fn
	}
	edgarFactoryMu.Unlock()
	return func() {
		edgarFactoryMu.Lock()
		edgarClientFactory = prev
		edgarFactoryMu.Unlock()
	}
}

func newEDGARClient(flagUA string) *edgar.Client {
	ua := resolveUserAgent(flagUA)
	edgarFactoryMu.RLock()
	fn := edgarClientFactory
	edgarFactoryMu.RUnlock()
	return fn(ua)
}

func resolveUserAgent(flagUA string) string {
	if ua := strings.TrimSpace(flagUA); ua != "" {
		return ua
	}
	if ua := strings.TrimSpace(os.Getenv("SEC_USER_AGENT")); ua != "" {
		return ua
	}
	return "SECAnalysisApp research@secanalysis.local"
}

type HydrateOptions struct {
	Refresh     bool
	Offline     bool
	UserAgent   string
	NeedQuote   bool
	NeedFX      bool
	Workers     int
	ErrorOutput io.Writer
}

type HydrationMetadata struct {
	Ticker            string
	CompanyName       string
	CIK               string
	Currency          string
	LatestPeriodLabel string
	SyncedAt          time.Time
	SyncJustNow       bool
	QuotePrice        float64
	QuoteFetchedAt    time.Time
	QuoteSource       string // "live", "cached", "stale", ""
	FXRate            float64
	FXFetchedAt       time.Time
	FXSource          string // "live", "cached", "fallback", ""
	ADR               market.ADRInfo
	IsADR             bool
}

type BatchHydrationResult struct {
	Total       int
	CacheHits   int
	Fetched     int
	Failed      int
	FailedList  []string
	Currencies  map[string]FXHydrationInfo
	FXLiveCount int
}

type FXHydrationInfo struct {
	Currency  string
	RateToUSD float64
	FetchedAt time.Time
	Source    string
}

type QuoteHydrationSummary struct {
	LiveCount   int
	CachedCount int
	StaleCount  int
	FailedCount int
	OldestAt    time.Time
	NewestAt    time.Time
}

// EnsureCompanyHydrated ensures a single ticker is cached in SQLite with precomputed
// summaries, live/cached FX rates (when non-USD), ADR metadata, and optional quote.
func EnsureCompanyHydrated(ctx context.Context, s *store.Store, ticker string, opts HydrateOptions) (HydrationMetadata, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	meta := HydrationMetadata{Ticker: ticker}

	st, err := s.CompanyHydrationStatus(ctx, ticker)
	if err != nil {
		return meta, err
	}

	spinner := NewSpinner(opts.ErrorOutput)
	var opErr error
	defer func() {
		spinner.Finish(opErr)
	}()

	needEDGAR := !st.HasFacts || opts.Refresh
	if needEDGAR {
		if opts.Offline {
			if !st.HasFacts {
				opErr = fmt.Errorf("no cached data for %s (offline mode enabled)", ticker)
				return meta, opErr
			}
		} else {
			client := newEDGARClient(opts.UserAgent)
			spinner.Step(fmt.Sprintf("Fetching %s (EDGAR)...", ticker))
			fetchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			formFamily, fetchErr := fetchAndSaveCompanyQuiet(fetchCtx, client, s, ticker, func(family string) {
				if family != "" {
					spinner.ReplaceFirstStep(fmt.Sprintf("Fetching %s facts (EDGAR)...", family))
				}
			})
			cancel()
			if fetchErr != nil {
				if !st.HasFacts {
					opErr = fetchErr
					return meta, opErr
				}
				// If refresh failed on an already-cached company, warn but continue with cached data.
				if opts.ErrorOutput != nil {
					_, _ = fmt.Fprintf(opts.ErrorOutput, "warning: refresh failed for %s (%v); using cached data\n", ticker, fetchErr)
				}
			} else {
				_ = formFamily
				_ = s.BumpCacheEpoch(ctx)
				meta.SyncJustNow = true
				st, _ = s.CompanyHydrationStatus(ctx, ticker)
			}
		}
	}

	// Ensure precomputed summary/rank/arb tables are clean for this ticker.
	_, _ = screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{
		Tickers:   []string{ticker},
		RankYears: screener.DefaultPrecomputeRankYears,
		ArbYears:  screener.DefaultPrecomputeArbYears,
	})
	st, _ = s.CompanyHydrationStatus(ctx, ticker)

	meta.CIK = st.CIK
	meta.CompanyName = st.CompanyName
	meta.Currency = strings.ToUpper(strings.TrimSpace(st.Currency))
	if meta.Currency == "" {
		meta.Currency = "USD"
	}
	meta.SyncedAt = st.UpdatedAt
	meta.LatestPeriodLabel = formatLatestFilingPeriod(st)

	// Resolve ADR info if applicable.
	if adrInfo, ok := market.GetADRInfo(ticker); ok {
		meta.ADR = adrInfo
		meta.IsADR = true
	}

	// Ensure FX rate if non-USD (or explicitly requested).
	if (meta.Currency != "" && meta.Currency != "USD") || opts.NeedFX {
		if meta.Currency != "" && meta.Currency != "USD" {
			fxInfo := ensureCurrencyFX(ctx, s, meta.Currency, opts, spinner)
			meta.FXRate = fxInfo.RateToUSD
			meta.FXFetchedAt = fxInfo.FetchedAt
			meta.FXSource = fxInfo.Source
		}
	}

	// Ensure market quote if requested.
	if opts.NeedQuote {
		qRec := ensureSingleQuote(ctx, s, ticker, opts)
		meta.QuotePrice = qRec.Price
		meta.QuoteFetchedAt = qRec.FetchedAt
		meta.QuoteSource = qRec.Source
	}

	return meta, nil
}

func fetchAndSaveCompanyQuiet(
	ctx context.Context,
	client *edgar.Client,
	s *store.Store,
	ticker string,
	onFormFamily func(string),
) (string, error) {
	cik, err := client.ResolveTicker(ctx, ticker)
	if err != nil {
		return "", err
	}
	submissions, err := client.Submissions(ctx, cik)
	if err != nil {
		return "", fmt.Errorf("fetch SEC filing submissions for %s: %w", ticker, err)
	}
	filings := submissions.AllFilings(cik)
	family := detectFilingFamily(filings)
	if onFormFamily != nil {
		onFormFamily(family)
	}

	factsData, err := client.Facts(ctx, cik)
	if err != nil {
		return family, fmt.Errorf("fetch SEC company facts for %s: %w", ticker, err)
	}
	facts := edgar.ExtractFacts(factsData, cik)
	companyName := submissions.Name
	if strings.TrimSpace(companyName) == "" {
		companyName = factsData.Entity
	}
	if err := s.SaveCompany(ctx, cik, companyName, ticker, string(submissions.SIC), submissions.SICDescription); err != nil {
		return family, err
	}
	if err := s.SaveFacts(ctx, ticker, facts); err != nil {
		return family, err
	}
	if err := s.SaveFilings(ctx, filings); err != nil {
		return family, err
	}
	if err := screener.PrecomputeCompany(ctx, s, store.LocalCompany{
		CIK:            cik,
		Name:           companyName,
		Ticker:         ticker,
		SIC:            string(submissions.SIC),
		SICDescription: submissions.SICDescription,
	}); err != nil {
		return family, fmt.Errorf("precompute variables for %s: %w", ticker, err)
	}
	return family, nil
}

func detectFilingFamily(filings []store.Filing) string {
	for _, f := range filings {
		form := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(f.Form), "/A"))
		switch form {
		case "20-F":
			return "20-F"
		case "40-F":
			return "40-F"
		case "10-K":
			return "10-K/10-Q"
		}
	}
	return "10-K/10-Q"
}

func ensureCurrencyFX(ctx context.Context, s *store.Store, currency string, opts HydrateOptions, spinner *Spinner) FXHydrationInfo {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" {
		return FXHydrationInfo{Currency: "USD", RateToUSD: 1.0, Source: "live"}
	}

	cached, found, _ := s.LoadFXRate(ctx, cur)
	fresh := found && cached.RateToUSD > 0 && !cached.FetchedAt.IsZero() && time.Since(cached.FetchedAt) < fxCacheTTL

	if fresh && !opts.Refresh {
		market.SetFXRate(cur, cached.RateToUSD)
		return FXHydrationInfo{
			Currency:  cur,
			RateToUSD: cached.RateToUSD,
			FetchedAt: cached.FetchedAt,
			Source:    "cached",
		}
	}

	if !opts.Offline {
		if spinner != nil {
			spinner.Step(fmt.Sprintf("Applying %s/USD FX...", cur))
		}
		fxCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rate, err := market.FetchLiveFXRate(fxCtx, cur)
		cancel()
		if err == nil && rate > 0 {
			now := time.Now().UTC()
			market.SetFXRate(cur, rate)
			_ = s.SaveFXRate(ctx, store.FXRateRecord{
				Currency:  cur,
				RateToUSD: rate,
				Source:    "live",
				FetchedAt: now,
			})
			return FXHydrationInfo{
				Currency:  cur,
				RateToUSD: rate,
				FetchedAt: now,
				Source:    "live",
			}
		}
	}

	if found && cached.RateToUSD > 0 {
		market.SetFXRate(cur, cached.RateToUSD)
		return FXHydrationInfo{
			Currency:  cur,
			RateToUSD: cached.RateToUSD,
			FetchedAt: cached.FetchedAt,
			Source:    "cached",
		}
	}
	return FXHydrationInfo{
		Currency:  cur,
		RateToUSD: market.GetFXRateToUSD(cur),
		Source:    "fallback",
	}
}

func ensureSingleQuote(ctx context.Context, s *store.Store, ticker string, opts HydrateOptions) store.QuoteRecord {
	cached, found, _ := s.LoadQuote(ctx, ticker)
	fresh := found && cached.Price > 0 && !cached.FetchedAt.IsZero() && time.Since(cached.FetchedAt) < quoteCacheTTL

	if fresh && !opts.Refresh {
		cached.Source = "cached"
		return cached
	}

	if !opts.Offline {
		qCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		q, err := market.FetchLiveQuote(qCtx, ticker)
		cancel()
		if err == nil && q.Price > 0 {
			rec := store.QuoteRecord{
				Ticker:    ticker,
				Price:     q.Price,
				High52:    q.High52,
				Low52:     q.Low52,
				Currency:  "USD",
				Source:    "live",
				FetchedAt: time.Now().UTC(),
			}
			_ = s.SaveQuote(ctx, rec)
			return rec
		}
	}

	if found && cached.Price > 0 {
		cached.Source = "stale"
		return cached
	}
	return store.QuoteRecord{}
}

// HydrateTickersBatch ensures all specified tickers are hydrated in SQLite,
// showing an inline progress bar for missing EDGAR fetches with SEC rate-limit ETA
// without dumping per-ticker logs or failing mid-run on individual ticker errors.
func HydrateTickersBatch(ctx context.Context, s *store.Store, tickers []string, opts HydrateOptions) (BatchHydrationResult, error) {
	var res BatchHydrationResult
	res.Total = len(tickers)
	res.Currencies = make(map[string]FXHydrationInfo)
	if len(tickers) == 0 {
		return res, nil
	}

	statuses, err := s.CompanyHydrationStatuses(ctx, tickers)
	if err != nil {
		return res, err
	}

	var toFetch []string
	for _, tk := range tickers {
		upper := strings.ToUpper(strings.TrimSpace(tk))
		if upper == "" {
			continue
		}
		st := statuses[upper]
		if st.HasFacts && !opts.Refresh {
			res.CacheHits++
		} else {
			toFetch = append(toFetch, upper)
		}
	}

	if len(toFetch) > 0 && !opts.Offline {
		client := newEDGARClient(opts.UserAgent)
		workers := opts.Workers
		if workers < 1 {
			workers = 4
		}
		if workers > 4 {
			workers = 4
		}
		if workers > len(toFetch) {
			workers = len(toFetch)
		}

		// Warm ticker mapping once so concurrent workers share local mapping cleanly.
		_ = client.WarmTickerCache(ctx)

		bar := NewBatchProgressBar(opts.ErrorOutput, "Hydrating", len(tickers), res.CacheHits, len(toFetch), 4.25)

		jobs := make(chan string, len(toFetch))
		for _, tk := range toFetch {
			jobs <- tk
		}
		close(jobs)

		var fetchedCount, failedCount atomic.Int64
		var failMu sync.Mutex
		var failedSample []string

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for tk := range jobs {
					if ctx.Err() != nil {
						return
					}
					fetchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
					_, fetchErr := fetchAndSaveCompanyQuiet(fetchCtx, client, s, tk, nil)
					cancel()
					if fetchErr != nil {
						failedCount.Add(1)
						failMu.Lock()
						if len(failedSample) < 8 {
							failedSample = append(failedSample, tk)
						}
						failMu.Unlock()
					} else {
						fetchedCount.Add(1)
					}
					bar.Update(int(fetchedCount.Load()), int(failedCount.Load()), tk)
				}
			}()
		}
		wg.Wait()

		res.Fetched = int(fetchedCount.Load())
		res.Failed = int(failedCount.Load())
		res.FailedList = failedSample
		bar.Finish(res.Fetched, res.Failed)

		if res.Failed > 0 && opts.ErrorOutput != nil {
			sample := strings.Join(res.FailedList, ", ")
			if res.Failed > len(res.FailedList) {
				sample += ", …"
			}
			_, _ = fmt.Fprintf(opts.ErrorOutput, "Note: %d ticker(s) unavailable on EDGAR (%s); continuing with %d available companies.\n",
				res.Failed, sample, res.CacheHits+res.Fetched)
		}
		if res.Fetched > 0 {
			_ = s.BumpCacheEpoch(ctx)
		}
	}

	if ctx.Err() != nil {
		return res, ctx.Err()
	}

	// Ensure precomputed summaries & rank/arbitrage variables are clean.
	_, _ = screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{
		Tickers:           tickers,
		RankYears:         screener.DefaultPrecomputeRankYears,
		ArbYears:          screener.DefaultPrecomputeArbYears,
		RefreshRankScores: false,
	})

	// Hydrate FX rates for any non-USD currencies present in the universe.
	if fxRates, fxErr := s.LoadAllFXRates(ctx); fxErr == nil {
		for _, fx := range fxRates {
			if fx.RateToUSD > 0 {
				market.SetFXRate(fx.Currency, fx.RateToUSD)
			}
		}
	}
	statusesAfter, err := s.CompanyHydrationStatuses(ctx, tickers)
	if err == nil {
		seenCur := make(map[string]bool)
		for _, st := range statusesAfter {
			cur := strings.ToUpper(strings.TrimSpace(st.Currency))
			if cur != "" && cur != "USD" && !seenCur[cur] {
				seenCur[cur] = true
				fxInfo := ensureCurrencyFX(ctx, s, cur, opts, nil)
				res.Currencies[cur] = fxInfo
				if fxInfo.Source == "live" {
					res.FXLiveCount++
				}
			}
		}
	}

	return res, nil
}

// EnsureUniverseQuotes resolves prices from explicit inputs, SQLite sec_quotes cache,
// and batched live quote fetches with a clean inline progress bar.
func EnsureUniverseQuotes(
	ctx context.Context,
	s *store.Store,
	tickers []string,
	explicitPrices map[string]float64,
	explicitMarketCaps map[string]float64,
	opts HydrateOptions,
) (map[string]float64, QuoteHydrationSummary) {
	prices := make(map[string]float64, len(explicitPrices)+len(tickers))
	for k, v := range explicitPrices {
		prices[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	var summary QuoteHydrationSummary
	if len(tickers) == 0 {
		return prices, summary
	}

	cachedMap, _ := s.LoadQuotes(ctx, tickers)
	var missing []string
	now := time.Now().UTC()

	for _, raw := range tickers {
		tk := strings.ToUpper(strings.TrimSpace(raw))
		if tk == "" {
			continue
		}
		if ValuationInput(prices, tk) > 0 || ValuationInput(explicitMarketCaps, tk) > 0 {
			summary.CachedCount++
			continue
		}
		if rec, ok := cachedMap[tk]; ok && rec.Price > 0 {
			age := now.Sub(rec.FetchedAt)
			if !opts.Refresh && !rec.FetchedAt.IsZero() && age < quoteCacheTTL {
				prices[tk] = rec.Price
				summary.CachedCount++
				recordQuoteTime(&summary, rec.FetchedAt)
				continue
			}
			if opts.Offline {
				prices[tk] = rec.Price
				summary.StaleCount++
				recordQuoteTime(&summary, rec.FetchedAt)
				continue
			}
		}
		if !opts.Offline {
			missing = append(missing, tk)
		}
	}

	if len(missing) > 0 && !opts.Offline {
		bar := NewBatchProgressBar(opts.ErrorOutput, "Quotes", len(tickers), summary.CachedCount, len(missing), 10.0)
		var mu sync.Mutex
		var fetched, failed int
		liveQuotes := market.FetchLiveQuotesBatch(ctx, missing, 4, func(_ int, _ int, tk string, q market.LiveQuote, err error) {
			mu.Lock()
			if err == nil && q.Price > 0 {
				fetched++
			} else {
				failed++
			}
			f, fl := fetched, failed
			mu.Unlock()
			bar.Update(f, fl, tk)
		})
		bar.Finish(fetched, failed)

		var toSave []store.QuoteRecord
		fetchTime := time.Now().UTC()
		for tk, q := range liveQuotes {
			if q.Price > 0 {
				prices[tk] = q.Price
				summary.LiveCount++
				recordQuoteTime(&summary, fetchTime)
				toSave = append(toSave, store.QuoteRecord{
					Ticker:    tk,
					Price:     q.Price,
					High52:    q.High52,
					Low52:     q.Low52,
					Currency:  "USD",
					Source:    "live",
					FetchedAt: fetchTime,
				})
			}
		}
		if len(toSave) > 0 {
			_ = s.SaveQuotes(ctx, toSave)
		}
		// Fallback to stale cached quote for any ticker that failed live fetch.
		for _, tk := range missing {
			if prices[tk] > 0 {
				continue
			}
			if rec, ok := cachedMap[tk]; ok && rec.Price > 0 {
				prices[tk] = rec.Price
				summary.StaleCount++
				recordQuoteTime(&summary, rec.FetchedAt)
			} else {
				summary.FailedCount++
			}
		}
	}
	return prices, summary
}

func recordQuoteTime(s *QuoteHydrationSummary, t time.Time) {
	if t.IsZero() {
		return
	}
	if s.OldestAt.IsZero() || t.Before(s.OldestAt) {
		s.OldestAt = t
	}
	if s.NewestAt.IsZero() || t.After(s.NewestAt) {
		s.NewestAt = t
	}
}

func formatLatestFilingPeriod(st store.CompanyCacheStatus) string {
	form := strings.TrimSuffix(strings.TrimSpace(st.LatestForm), "/A")
	dateStr := strings.TrimSpace(st.LatestReportDate)
	if dateStr == "" {
		dateStr = strings.TrimSpace(st.LatestPeriodEnd)
	}
	if dateStr == "" {
		dateStr = strings.TrimSpace(st.LatestFilingDate)
	}
	if dateStr == "" && form == "" {
		return "local cache"
	}
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		if form == "10-Q" || form == "6-K" {
			q := (int(t.Month())-1)/3 + 1
			return fmt.Sprintf("Q%d %d %s", q, t.Year(), form)
		}
		if form != "" {
			return fmt.Sprintf("FY %d %s", t.Year(), form)
		}
		return fmt.Sprintf("FY %d", t.Year())
	}
	if form != "" && len(dateStr) >= 4 {
		return dateStr[:4] + " " + form
	}
	if form != "" {
		return form
	}
	return dateStr
}

// FormatRelativeAge formats a past timestamp as a concise human-readable age ("4m ago", "2h ago").
func FormatRelativeAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < 30*time.Second {
		return "just now"
	}
	if d < time.Minute {
		return "<1m ago"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
}

// WriteFreshnessHeader prints a subtle metadata header line on every single-company output.
func WriteFreshnessHeader(output io.Writer, meta HydrationMetadata) {
	if output == nil {
		return
	}
	parts := make([]string, 0, 4)

	dataPart := meta.LatestPeriodLabel
	if dataPart == "" {
		dataPart = "EDGAR"
	}
	if meta.SyncJustNow {
		dataPart += " (synced just now)"
	} else if !meta.SyncedAt.IsZero() {
		dataPart += " (synced " + FormatRelativeAge(meta.SyncedAt) + ")"
	}
	parts = append(parts, "Data: "+dataPart)

	if meta.QuotePrice > 0 {
		qStatus := "synced " + FormatRelativeAge(meta.QuoteFetchedAt)
		if meta.QuoteSource == "live" {
			qStatus = "live"
		} else if meta.QuoteSource == "stale" {
			qStatus = "stale · " + FormatRelativeAge(meta.QuoteFetchedAt)
		}
		parts = append(parts, fmt.Sprintf("Price $%.2f (%s)", meta.QuotePrice, qStatus))
	}

	if meta.Currency != "" && meta.Currency != "USD" && meta.FXRate > 0 {
		fxStatus := meta.FXSource
		if fxStatus == "cached" && !meta.FXFetchedAt.IsZero() {
			fxStatus = FormatRelativeAge(meta.FXFetchedAt)
		}
		if fxStatus != "" {
			parts = append(parts, fmt.Sprintf("%s→USD %.4f (%s)", meta.Currency, meta.FXRate, fxStatus))
		} else {
			parts = append(parts, fmt.Sprintf("%s→USD %.4f", meta.Currency, meta.FXRate))
		}
	}

	if meta.IsADR {
		ratioStr := strconv.FormatFloat(meta.ADR.Ratio, 'g', -1, 64)
		if meta.ADR.OrdinaryTicker != "" {
			parts = append(parts, fmt.Sprintf("ADR %s:1 (%s)", ratioStr, meta.ADR.OrdinaryTicker))
		} else {
			parts = append(parts, fmt.Sprintf("ADR %s:1", ratioStr))
		}
	}

	line := strings.Join(parts, " · ")
	if isTerminalWriter(output) {
		_, _ = fmt.Fprintf(output, "\033[2m%s\033[0m\n", line)
		return
	}
	_, _ = fmt.Fprintln(output, line)
}

// WriteBatchFreshnessHeader prints a subtle freshness summary header for multi-company screens.
func WriteBatchFreshnessHeader(output io.Writer, universeLabel string, total int, batch BatchHydrationResult, quoteSummary *QuoteHydrationSummary) {
	if output == nil {
		return
	}
	parts := make([]string, 0, 4)
	if universeLabel != "" {
		parts = append(parts, fmt.Sprintf("Universe: %s (%d companies)", universeLabel, total))
	} else {
		parts = append(parts, fmt.Sprintf("Data: local SQLite (%d companies)", total))
	}
	if batch.Fetched > 0 {
		parts = append(parts, fmt.Sprintf("EDGAR: %d fetched, %d cached", batch.Fetched, batch.CacheHits))
	} else if batch.CacheHits > 0 {
		parts = append(parts, "EDGAR: cached")
	}
	if quoteSummary != nil {
		switch {
		case quoteSummary.LiveCount > 0 && quoteSummary.CachedCount > 0:
			parts = append(parts, fmt.Sprintf("Prices: %d live, %d cached", quoteSummary.LiveCount, quoteSummary.CachedCount))
		case quoteSummary.LiveCount > 0:
			parts = append(parts, "Prices: live")
		case quoteSummary.CachedCount > 0 && !quoteSummary.NewestAt.IsZero():
			parts = append(parts, "Prices: synced "+FormatRelativeAge(quoteSummary.NewestAt))
		case quoteSummary.StaleCount > 0:
			parts = append(parts, "Prices: cached (stale)")
		}
	}
	if len(batch.Currencies) > 0 {
		if batch.FXLiveCount > 0 {
			parts = append(parts, "FX: live")
		} else {
			parts = append(parts, "FX: cached")
		}
	}

	line := strings.Join(parts, " · ")
	if isTerminalWriter(output) {
		_, _ = fmt.Fprintf(output, "\033[2m%s\033[0m\n", line)
		return
	}
	_, _ = fmt.Fprintln(output, line)
}
