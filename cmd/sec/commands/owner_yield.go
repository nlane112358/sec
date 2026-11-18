package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

func RunOwnerYield(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("owner-yield", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen owner-yield [@universe|TICKER...] [--min-yield 0] [--min-roic 0] [--min-revenue 50000000] [--hurdle] [--sort yield|standard_yield|fcf_yield|roic|market_cap|price|owner_earnings|pe|spread] [--asc] [--top N] [--include-financials] [--where EXPR] [--prices FILE] [--refresh] [--offline] [--workers 1-4] [--data-dir DIR]")

	universeFile := flags.String("universe", "", "optional universe alias (@sp500, @euro-uk, ...) or file")
	whereExpr := flags.String("where", "", "additional formula filter expression")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", true, "automatically fetch live quotes for candidates missing cached quotes")
	noLivePrice := flags.Bool("no-live-prices", false, "skip live quote fetches and use cached/file quotes only")
	minYield := flags.Float64("min-yield", 0, "minimum Intangible-Adjusted Owner Earnings Yield in percent")
	minROIC := flags.Float64("min-roic", 0, "minimum Intangible-Adjusted ROIC in percent")
	minRevenue := flags.Float64("min-revenue", 50_000_000, "minimum annual revenue")
	hurdle := flags.Bool("hurdle", false, "require Owner Yield >= 6.84% institutional hurdle rate")
	includeFinancials := flags.Bool("include-financials", false, "include financial-sector SIC codes 6000-6799")
	sortBy := flags.String("sort", "yield", "sort by: yield, standard_yield, fcf_yield, roic, market_cap, price, owner_earnings, pe, spread")
	sortAsc := flags.Bool("asc", false, "sort in ascending order")
	top := flags.Int("top", 0, "limit output to top N candidates")
	refresh := flags.Bool("refresh", false, "force fresh EDGAR, quote, and FX pulls")
	offline := flags.Bool("offline", false, "use local SQLite cache only")
	workers := flags.Int("workers", 4, "concurrent EDGAR hydration workers (1-4)")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := validateScreenWorkers(*workers); err != nil {
		return err
	}
	if *top < 0 {
		return errors.New("--top must be non-negative")
	}
	for name, v := range map[string]float64{
		"--min-yield":   *minYield,
		"--min-roic":    *minROIC,
		"--min-revenue": *minRevenue,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("%s must be finite and non-negative", name)
		}
	}

	marketCaps, prices, err := market.LoadValuationInputs(*pricesFile)
	if err != nil {
		return err
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, defaultScreenTimeout())
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	hydrateOpts := HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline || !*fetchPrices || *noLivePrice,
		UserAgent:   *userAgent,
		Workers:     *workers,
		ErrorOutput: errorOutput,
	}

	tickers, universeLabel, batch, err := prepareScreenUniverse(ctx, s, flags.Args(), *universeFile, HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline,
		UserAgent:   *userAgent,
		Workers:     *workers,
		ErrorOutput: errorOutput,
	})
	if err != nil {
		return err
	}

	// Determine pre-eligible tickers from precomputed arbitrage variables to avoid fetching unnecessary quotes.
	arbRows, err := s.LoadGAAPArbitrageVariables(ctx, 5, screener.GAAPArbitrageSchemaVersion, screener.NormalizeTickers(tickers))
	if err != nil {
		return err
	}
	var quoteCandidates []string
	for _, r := range arbRows {
		if !r.Eligible || !r.HasOwnerEarnings {
			continue
		}
		if !*includeFinancials && r.Sector == "Financials" {
			continue
		}
		if *minRevenue > 0 && r.GAAPRevenue < *minRevenue {
			continue
		}
		if *minROIC > 0 && (!r.HasAdjustedROIC || r.AdjustedROIC < *minROIC/100) {
			continue
		}
		quoteCandidates = append(quoteCandidates, r.Ticker)
	}

	resolvedPrices, qSum := EnsureUniverseQuotes(ctx, s, quoteCandidates, prices, marketCaps, hydrateOpts)

	result, err := screener.ScreenOwnerYield(ctx, s, tickers, marketCaps, resolvedPrices, screener.OwnerYieldFilter{
		MinYield:          *minYield / 100.0,
		MinAdjustedROIC:   *minROIC / 100.0,
		MinRevenue:        *minRevenue,
		Years:             5,
		RequireHurdleBeat: *hurdle,
		IncludeFinancials: *includeFinancials,
		SortBy:            *sortBy,
		SortAsc:           *sortAsc,
	})
	if err != nil {
		return err
	}

	if strings.TrimSpace(*whereExpr) != "" && len(result.Candidates) > 0 {
		candTickers := make([]string, len(result.Candidates))
		for i, c := range result.Candidates {
			candTickers[i] = c.Ticker
		}
		allowed, wErr := filterTickersByWhere(ctx, s, *whereExpr, candTickers, resolvedPrices, marketCaps, hydrateOpts)
		if wErr != nil {
			return wErr
		}
		var filtered []screener.OwnerYieldCandidate
		for _, c := range result.Candidates {
			if allowed[c.Ticker] {
				c.Rank = len(filtered) + 1
				filtered = append(filtered, c)
			}
		}
		result.Candidates = filtered
		result.Total = len(filtered)
	}

	if *top > 0 && *top < len(result.Candidates) {
		result.Candidates = result.Candidates[:*top]
	}

	WriteBatchFreshnessHeader(output, universeLabel, len(arbRows), batch, &qSum)
	if len(result.Candidates) == 0 {
		_, err := fmt.Fprintln(output, "No companies matched the Owner Yield screening criteria.")
		return err
	}
	if _, err := fmt.Fprintf(output, "Owner Earnings Yield Screener — %d candidates (%d beating %.2f%% hurdle · median yield %.2f%%)\n",
		result.Total, result.BeatsCount, result.HurdleRate*100, result.MedianYield*100); err != nil {
		return err
	}
	return screener.WriteOwnerYieldTable(output, result)
}
