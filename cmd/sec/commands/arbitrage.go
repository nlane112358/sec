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
	"sec/internal/valuation"
)

func RunArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("arbitrage", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen arbitrage [@universe|TICKER...] [--prices FILE] [--min-score 70] [--min-roic 12] [--require-buybacks] [--top N] [--where EXPR] [--refresh] [--offline] [--workers 1-4] [--data-dir DIR]")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", true, "automatically fetch live quotes for quality-qualified candidates")
	noLivePrice := flags.Bool("no-live-prices", false, "skip live quote fetches and use cached/file quotes only")
	minScore := flags.Float64("min-score", 70, "minimum rank composite quality score (0-100)")
	minROIC := flags.Float64("min-roic", 12, "minimum multi-year average ROIC in percent")
	minConsistency := flags.Float64("min-consistency", 80, "minimum operating margin consistency in percent")
	requireBuybacks := flags.Bool("require-buybacks", true, "require non-dilutive or shrinking share count")
	maxImpliedGrowth := flags.Float64("max-implied-growth", 0, "optional maximum implied growth rate in percent; 0 disables the filter")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent")
	profileFlag := flags.String("profile", "", "macro profile for regional risk-free rate: us, eu, uk, ch")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth rate in percent")
	top := flags.Int("top", 0, "limit output to top N candidates")
	universeFile := flags.String("universe", "", "optional universe alias (@sp500, @euro-uk, ...) or file")
	whereExpr := flags.String("where", "", "additional formula filter expression")
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

	riskFreeProvided := registerFlagCheck(flags, "risk-free")["risk-free"]
	if *profileFlag != "" && !riskFreeProvided {
		profile := market.ResolveMacroProfile(*profileFlag)
		*riskFree = profile.RiskFreeRate
	}
	for name, value := range map[string]float64{
		"--min-score": *minScore, "--min-roic": *minROIC,
		"--min-consistency": *minConsistency, "--risk-free": *riskFree,
		"--erp": *equityRiskPremium, "--terminal": *terminal,
		"--max-implied-growth": *maxImpliedGrowth,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s must be finite", name)
		}
	}
	if *minScore < 0 || *minScore > 100 || *minROIC < 0 || *minROIC > 100 || *minConsistency < 0 || *minConsistency > 100 {
		return errors.New("--min-score, --min-roic, and --min-consistency must be between 0 and 100")
	}
	if *riskFree < 0 || *riskFree >= 100 || *equityRiskPremium < 0 || *equityRiskPremium >= 100 || *terminal < 0 || *terminal >= 100 || *maxImpliedGrowth < 0 {
		return errors.New("--risk-free, --erp, and --terminal must be between 0 and 100 percent; --max-implied-growth must be non-negative")
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

	rankResult, err := screener.RankCompanies(ctx, s, 6, 100_000_000, tickers, true)
	if err != nil {
		return err
	}

	var quoteCandidates []string
	for _, company := range rankResult.Companies {
		if company.Score < *minScore || (*minROIC > 0 && (!company.HasROIC || company.ROIC < *minROIC/100)) ||
			(*minConsistency > 0 && (!company.HasOperatingMargin || company.OperatingConsistency < *minConsistency/100)) ||
			(*requireBuybacks && (!company.HasShareTrend || company.ShareReduction < 0)) {
			continue
		}
		quoteCandidates = append(quoteCandidates, company.Ticker)
	}

	resolvedPrices, qSum := EnsureUniverseQuotes(ctx, s, quoteCandidates, prices, marketCaps, quoteOpts)

	waccConfig := &valuation.WACCConfig{
		RiskFreeRate: *riskFree / 100, EquityRiskPremium: *equityRiskPremium / 100,
		Beta: 1, CostOfDebt: 0.05, TaxRate: 0.21,
		UseEffectiveTaxRate: true, UseEstimatedCostDebt: true,
	}
	filter := screener.ArbitrageFilter{
		MinScore: *minScore, MinROIC: *minROIC / 100,
		MinOperatingConsist: *minConsistency / 100,
		RequireBuybacks:     *requireBuybacks, MaxImpliedGrowth: *maxImpliedGrowth / 100,
		DCFConfig: valuation.DCFConfig{
			Mode: valuation.DCFModeOwnerEarnings, TerminalRate: *terminal / 100,
			Years: 5, WACC: waccConfig,
		},
	}
	candidates, err := screener.ScreenExpectationsArbitrage(ctx, s, rankResult.Companies, marketCaps, resolvedPrices, filter)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*whereExpr) != "" && len(candidates) > 0 {
		candTickers := make([]string, len(candidates))
		for i, c := range candidates {
			candTickers[i] = c.Ranked.Ticker
		}
		allowed, wErr := filterTickersByWhere(ctx, s, *whereExpr, candTickers, resolvedPrices, marketCaps, quoteOpts)
		if wErr != nil {
			return wErr
		}
		var filtered []screener.ArbitrageCandidate
		for _, c := range candidates {
			if allowed[c.Ranked.Ticker] {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}

	if *top > 0 && *top < len(candidates) {
		candidates = candidates[:*top]
	}

	WriteBatchFreshnessHeader(output, universeLabel, rankResult.Considered, batch, &qSum)
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(output, "No companies matched the quality and valuation inputs.")
		return err
	}
	if _, err := fmt.Fprintf(output, "Quality vs. Implied Expectations Arbitrage — %d compounders evaluated\n", len(candidates)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Quality gate: Score ≥ %.0f | 5Y ROIC ≥ %.1f%% | Operating consistency ≥ %.0f%% | Buybacks required: %v\n", *minScore, *minROIC, *minConsistency, *requireBuybacks); err != nil {
		return err
	}
	return screener.WriteArbitrageTable(output, candidates)
}
