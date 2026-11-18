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

func RunModernGAAPArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("gaap-arbitrage", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen gaap-arbitrage [@universe|TICKER...] [--prices FILE] [--min-adj-roic 15] [--min-yield 3.5] [--surging] [--screen-expensive] [--max-gaap-roic PCT] [--sort yield|roic|spread|score] [--top N] [--include-financials] [--years N] [--where EXPR] [--refresh] [--offline] [--workers 1-4] [--data-dir DIR]")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", true, "automatically fetch live quotes for candidates")
	noLivePrice := flags.Bool("no-live-prices", false, "skip live quote fetches and use cached/file quotes only")
	minAdjROIC := flags.Float64("min-adj-roic", 15.0, "minimum Intangible-Adjusted ROIC in percent")
	minYield := flags.Float64("min-yield", 3.5, "minimum Intangible-Adjusted Owner Earnings Yield in percent; 0 disables yield filter")
	surging := flags.Bool("surging", false, "require surging/expanding Intangible-Adjusted ROIC")
	screenExpensive := flags.Bool("screen-expensive", false, "require company to appear expensive or unprofitable under GAAP (P/E >= 25, loss, or ROIC spread >= 5%)")
	maxGAAPROIC := flags.Float64("max-gaap-roic", 0.0, "optional ceiling on standard GAAP ROIC in percent to isolate distorted firms")
	sortBy := flags.String("sort", "yield", "sort table by: yield, roic, spread, or score")
	top := flags.Int("top", 0, "limit output to top N candidates")
	includeFinancials := flags.Bool("include-financials", false, "include financial-sector SIC codes 6000-6799 (excluded by default)")
	universeFile := flags.String("universe", "", "optional universe alias (@sp500, @euro-uk, ...) or file")
	whereExpr := flags.String("where", "", "additional formula filter expression")
	years := flags.Int("years", 5, "years of history to analyze")
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
	for name, value := range map[string]float64{
		"--min-adj-roic":  *minAdjROIC,
		"--min-yield":     *minYield,
		"--max-gaap-roic": *maxGAAPROIC,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s must be finite", name)
		}
	}
	if *minAdjROIC < 0 || *minAdjROIC > 500 {
		return errors.New("--min-adj-roic must be between 0 and 500 percent")
	}
	if *minYield < 0 || *minYield > 100 {
		return errors.New("--min-yield must be between 0 and 100 percent")
	}
	if *years < 1 || *years > 10 {
		return errors.New("--years must be between 1 and 10")
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

	edgarOpts := HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline,
		UserAgent:   *userAgent,
		Workers:     *workers,
		ErrorOutput: errorOutput,
	}
	quoteOpts := edgarOpts
	quoteOpts.Offline = *offline || !*fetchPrices || *noLivePrice

	tickers, universeLabel, batch, err := prepareScreenUniverse(ctx, s, flags.Args(), *universeFile, edgarOpts)
	if err != nil {
		return err
	}

	// Pre-filter eligible candidates before fetching live quotes.
	arbRows, err := s.LoadGAAPArbitrageVariables(ctx, *years, screener.GAAPArbitrageSchemaVersion, screener.NormalizeTickers(tickers))
	if err != nil {
		return err
	}
	var quoteCandidates []string
	for _, r := range arbRows {
		if !r.Eligible {
			continue
		}
		if !*includeFinancials && r.Sector == "Financials" {
			continue
		}
		if r.GAAPRevenue < 100_000_000 {
			continue
		}
		if *minAdjROIC > 0 && (!r.HasAdjustedROIC || r.AdjustedROIC < *minAdjROIC/100.0) {
			continue
		}
		if *surging && !r.SurgingROIC {
			continue
		}
		if *maxGAAPROIC > 0 && r.HasGAAPROIC && r.GAAPROIC > *maxGAAPROIC/100.0 {
			continue
		}
		quoteCandidates = append(quoteCandidates, r.Ticker)
	}

	resolvedPrices, qSum := EnsureUniverseQuotes(ctx, s, quoteCandidates, prices, marketCaps, quoteOpts)

	filter := screener.ModernGAAPFilter{
		MinAdjustedROIC:      *minAdjROIC / 100.0,
		MinOwnerYield:        *minYield / 100.0,
		RequireSurging:       *surging,
		RequireGAAPExpensive: *screenExpensive,
		MaxGAAPROIC:          *maxGAAPROIC / 100.0,
		Years:                *years,
		SortBy:               *sortBy,
		IncludeFinancials:    *includeFinancials,
	}

	candidates, err := screener.ScreenModernGAAPArbitrage(ctx, s, tickers, marketCaps, resolvedPrices, filter)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*whereExpr) != "" && len(candidates) > 0 {
		candTickers := make([]string, len(candidates))
		for i, c := range candidates {
			candTickers[i] = c.Ticker
		}
		allowed, wErr := filterTickersByWhere(ctx, s, *whereExpr, candTickers, resolvedPrices, marketCaps, quoteOpts)
		if wErr != nil {
			return wErr
		}
		var filtered []screener.ModernGAAPCandidate
		for _, c := range candidates {
			if allowed[c.Ticker] {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}

	if *top > 0 && *top < len(candidates) {
		candidates = candidates[:*top]
	}

	WriteBatchFreshnessHeader(output, universeLabel, len(arbRows), batch, &qSum)
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(output, "No companies matched the Intangible-Capitalized screening criteria. Try adjusting --min-adj-roic or --min-yield.")
		return err
	}
	if _, err := fmt.Fprintf(output, "Modern GAAP Arbitrage — %d Intangible-Capitalized compounders identified\n", len(candidates)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Screen: Adj ROIC ≥ %.1f%% | Owner Yield ≥ %.1f%% | Surging required: %v | GAAP expensive required: %v\n", *minAdjROIC, *minYield, *surging, *screenExpensive); err != nil {
		return err
	}
	return screener.WriteModernGAAPTable(output, candidates)
}
