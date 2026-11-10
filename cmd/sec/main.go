package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"sec/internal/sec"
	"sec/internal/sqlite"
)

const usage = `usage: sec <command> [arguments] [options]

Commands:
  sync TICKER...              Download or refresh facts and filings
  sync --universe FILE        Sync newline-separated tickers from a file
  summary TICKER [--years N]  Show annual financial metrics (3-5 years)
  dcf TICKER [options]        Estimate intrinsic value from SEC facts
  arbitrage [options]         Rank quality compounders against implied growth
  gaap-arbitrage [options]    Screen modern GAAP arbitrage (intangible-capitalized value)
  audit [--gaps-only]         Scan the database and report missing facts
  audit TICKER                Deep-dive diagnostic on a company to discover raw tags
  read TICKER                 Browse and read saved filings
  list                        List companies in the shared database
  export TICKER --format FMT  Export saved facts as csv or json
  rank [--universe FILE]      Screen and rank the saved universe

Options:
  --data-dir DIR  directory containing sec.db (default SEC_DATABASE_DIR or data)

Sync option:
  --ua STRING     SEC User-Agent including contact email (default SEC_USER_AGENT)
  --workers N     concurrent sync workers (1-4, default 4)

Run "sec <command> --help" for command-specific options.
`

var tickerPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,14}$`)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func run(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, usage)
		return err
	}
	command, args := args[0], args[1:]
	switch command {
	case "sync":
		return runSync(parent, args, output, errorOutput)
	case "summary":
		return runSummary(parent, args, output, errorOutput)
	case "dcf":
		return runDCF(parent, args, output, errorOutput)
	case "read":
		return runRead(parent, args, output, errorOutput)
	case "list":
		return runList(parent, args, output, errorOutput)
	case "export":
		return runExport(parent, args, output, errorOutput)
	case "rank":
		return runRank(parent, args, output, errorOutput)
	case "arbitrage":
		return runArbitrage(parent, args, output, errorOutput)
	case "gaap-arbitrage", "modern-value", "modern", "intangible":
		return runModernGAAPArbitrage(parent, args, output, errorOutput)
	case "audit":
		return runAudit(parent, args, output, errorOutput)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}

func runSync(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec sync TICKER... [--workers 1-4] [--data-dir DIR] [--ua STRING]\n       sec sync --universe FILE [--workers 1-4] [--data-dir DIR] [--ua STRING]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	universeFile := flags.String("universe", "", "file containing one ticker per line")
	workers := flags.Int("workers", 4, "concurrent sync workers (1-4)")
	if err := parseCommandFlags(flags, args, "data-dir", "ua", "universe", "workers"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	tickers, err := loadTickerUniverse(flags.Args(), *universeFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*userAgent) == "" {
		return errors.New("provide --ua 'Research App contact@example.com' or set SEC_USER_AGENT")
	}
	if *workers < 1 || *workers > 4 {
		return errors.New("--workers must be between 1 and 4")
	}
	databasePath := sharedDatabasePath(*dataDir)
	store, err := sec.OpenStore(parent, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	client := sec.NewClient(*userAgent)
	if len(tickers) == 0 {
		return nil
	}
	workerCount := min(*workers, len(tickers))
	var writeMu sync.Mutex
	safeOutput := lockedWriter{mu: &writeMu, writer: output}
	safeErrorOutput := lockedWriter{mu: &writeMu, writer: errorOutput}
	_, _ = fmt.Fprintf(safeErrorOutput, "Syncing %d companies with %d workers; SEC requests share the 8.5 requests/second limiter.\n", len(tickers), workerCount)
	type syncJob struct {
		index  int
		ticker string
	}
	jobs := make(chan syncJob, len(tickers))
	failures := make([]error, len(tickers))
	for index, ticker := range tickers {
		jobs <- syncJob{index: index, ticker: ticker}
	}
	close(jobs)
	var workersDone sync.WaitGroup
	workersDone.Add(workerCount)
	for range workerCount {
		go func() {
			defer workersDone.Done()
			for job := range jobs {
				if err := parent.Err(); err != nil {
					failures[job.index] = err
					continue
				}
				ctx, cancel := context.WithTimeout(parent, 90*time.Second)
				err := syncCompany(ctx, client, store, job.ticker, job.index+1, len(tickers), databasePath, safeOutput, safeErrorOutput)
				cancel()
				if err != nil {
					fmt.Fprintf(safeErrorOutput, "Sync failed for %s: %v\n", job.ticker, err)
					failures[job.index] = fmt.Errorf("%s: %w", job.ticker, err)
				}
			}
		}()
	}
	workersDone.Wait()
	if err := parent.Err(); err != nil {
		return err
	}
	return errors.Join(failures...)
}

type lockedWriter struct {
	mu     *sync.Mutex
	writer io.Writer
}

func (w lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

func syncCompany(ctx context.Context, client *sec.Client, store *sec.Store, ticker string, index, count int, databasePath string, output, errorOutput io.Writer) error {
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Resolving %s…\n", index, count, ticker); err != nil {
		return err
	}
	cik, err := client.ResolveTicker(ctx, ticker)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Fetching filing history for %s…\n", index, count, ticker); err != nil {
		return err
	}
	submissions, err := client.Submissions(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC filing submissions: %w", err)
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Fetching company facts for %s…\n", index, count, ticker); err != nil {
		return err
	}
	factsData, err := client.Facts(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC company facts: %w", err)
	}
	facts := sec.ExtractFacts(factsData, cik)
	filings := submissions.AllFilings(cik)
	companyName := submissions.Name
	if strings.TrimSpace(companyName) == "" {
		companyName = factsData.Entity
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Saving %d facts and %d filings for %s…\n", index, count, len(facts), len(filings), ticker); err != nil {
		return err
	}
	if err := store.SaveCompany(ctx, cik, companyName, ticker, string(submissions.SIC), submissions.SICDescription); err != nil {
		return err
	}
	if err := store.SaveFacts(ctx, ticker, facts); err != nil {
		return err
	}
	if err := store.SaveFilings(ctx, filings); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "saved_facts=%d saved_filings=%d ticker=%s cik=%s database=%s\n", len(facts), len(filings), ticker, cik, databasePath)
	return err
}

func runSummary(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("summary", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec summary TICKER [--years 3|4|5] [--data-dir DIR]")
	years := flags.Int("years", 5, "fiscal years to show (3-5)")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "years", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *years < 3 || *years > 5 {
		return errors.New("--years must be between 3 and 5")
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		summary, err := store.FinancialSummary(ctx, *years, ticker)
		if err != nil {
			return err
		}
		return sec.WriteFinancialSummary(output, summary)
	})
}

func runDCF(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("dcf", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec dcf TICKER [--reverse] [--mode owner|fcf|revenue] [--market-cap AMOUNT] [--growth PCT] [--discount PCT] [--debt AMOUNT] [--capex-mode raw|normalized] [--years N] [WACC options] [--data-dir DIR]")
	mode := flags.String("mode", "fcf", "DCF model: fcf or revenue-driven FCFF; owner is available with --reverse")
	reverse := flags.Bool("reverse", false, "solve for market-implied growth from --market-cap")
	growth := flags.Float64("growth", 0, "annual forecast growth in percent; defaults to historical three-year CAGR for the selected mode")
	discount := flags.Float64("discount", 0, "manual discount rate in percent; defaults to calculated WACC")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth in percent")
	years := flags.Int("years", 5, "explicit forecast period in years (1-20)")
	capexMode := flags.String("capex-mode", "raw", "CapEx forecast: raw with a terminal growth-CapEx fade, or normalized to D&A")
	normalizeCapEx := flags.Bool("normalize-capex", false, "normalize CapEx to depreciation and amortization")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent (default is the Nov. 5, 2026 US 10-year Treasury yield)")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	beta := flags.Float64("beta", 1.0, "company beta; EDGAR does not provide beta")
	costOfDebt := flags.Float64("cost-debt", 5.0, "fallback pre-tax cost of debt in percent; historical interest/debt is used when available")
	taxRate := flags.Float64("tax", 21.0, "fallback tax rate in percent; historical effective tax rate is used by default")
	marketCap := flags.Float64("market-cap", 0, "market capitalization in reported currency units; otherwise book equity is used for WACC weights")
	debt := flags.Float64("debt", 0, "gross debt override in reported currency units; use 0 only when filings confirm no debt")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "mode", "growth", "discount", "terminal", "years", "capex-mode", "normalize-capex", "risk-free", "erp", "beta", "cost-debt", "tax", "market-cap", "debt", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *capexMode != "raw" && *capexMode != "normalized" {
		return errors.New("--capex-mode must be raw or normalized")
	}
	if *riskFree < 0 || *riskFree >= 100 || *equityRiskPremium < 0 || *equityRiskPremium >= 100 ||
		*costOfDebt < 0 || *costOfDebt >= 100 || *taxRate < 0 || *taxRate >= 100 || *beta < 0 || *marketCap < 0 {
		return errors.New("WACC rates must be between 0 and 100 percent, and beta and market capitalization must be non-negative")
	}
	growthProvided, discountProvided, taxProvided, costDebtProvided, debtProvided, modeProvided := false, false, false, false, false, false
	flags.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "mode":
			modeProvided = true
		case "growth":
			growthProvided = true
		case "discount":
			discountProvided = true
		case "tax":
			taxProvided = true
		case "cost-debt":
			costDebtProvided = true
		case "debt":
			debtProvided = true
		}
	})
	if *reverse {
		if !modeProvided {
			*mode = string(sec.DCFModeOwnerEarnings)
		}
		if *mode != string(sec.DCFModeOwnerEarnings) && *mode != string(sec.DCFModeFCF) && *mode != string(sec.DCFModeRevenue) {
			return errors.New("reverse DCF --mode must be owner, fcf, or revenue")
		}
		if growthProvided {
			return errors.New("--growth cannot be combined with --reverse because growth is solved from market capitalization")
		}
	} else if *mode != string(sec.DCFModeFCF) && *mode != string(sec.DCFModeRevenue) {
		return errors.New("--mode must be fcf or revenue; owner is available with --reverse")
	}
	if discountProvided && *discount <= 0 {
		return errors.New("--discount must be greater than 0 when provided")
	}
	if discountProvided && *discount >= 100 {
		return errors.New("--discount must be less than 100 percent")
	}
	if *reverse && *marketCap <= 0 {
		return errors.New("--reverse requires a positive --market-cap")
	}
	useNormalizedCapEx := *normalizeCapEx || *capexMode == "normalized"
	var debtOverride *float64
	if debtProvided {
		debtOverride = debt
	}
	waccConfig := &sec.WACCConfig{
		RiskFreeRate:         *riskFree / 100,
		EquityRiskPremium:    *equityRiskPremium / 100,
		Beta:                 *beta,
		CostOfDebt:           *costOfDebt / 100,
		TaxRate:              *taxRate / 100,
		MarketCap:            *marketCap,
		DebtOverride:         debtOverride,
		UseEffectiveTaxRate:  !taxProvided,
		UseEstimatedCostDebt: !costDebtProvided,
	}
	config := sec.DCFConfig{
		Mode:                sec.DCFMode(*mode),
		GrowthRate:          *growth / 100,
		UseHistoricalGrowth: !growthProvided,
		DiscountRate:        *discount / 100,
		TerminalRate:        *terminal / 100,
		Years:               *years,
		NormalizeCapEx:      useNormalizedCapEx,
		DebtOverride:        debtOverride,
		TaxRate:             *taxRate / 100,
		UseEffectiveTaxRate: !taxProvided,
		WACC:                waccConfig,
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		yearsToLoad := 5
		if *reverse {
			yearsToLoad = 6
		}
		summary, err := store.FinancialSummary(ctx, yearsToLoad, ticker)
		if err != nil {
			return err
		}
		if *reverse {
			result, err := sec.SolveReverseDCF(summary, config)
			if err != nil {
				return err
			}
			return sec.WriteReverseDCFResult(output, result)
		}
		result, err := sec.CalculateDCF(summary, config)
		if err != nil {
			return err
		}
		return sec.WriteDCFResult(output, result)
	})
}

func runArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("arbitrage", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec arbitrage [--prices FILE] [--fetch-prices] [--min-score 75] [--min-roic 12] [--require-buybacks] [--universe FILE] [--data-dir DIR]")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", false, "fetch live Yahoo Finance quotes (Stooq fallback) for candidates missing from --prices")
	minScore := flags.Float64("min-score", 70, "minimum sec rank composite quality score (0-100)")
	minROIC := flags.Float64("min-roic", 12, "minimum multi-year average ROIC in percent")
	minConsistency := flags.Float64("min-consistency", 80, "minimum operating margin consistency in percent")
	requireBuybacks := flags.Bool("require-buybacks", true, "require non-dilutive or shrinking share count")
	maxImpliedGrowth := flags.Float64("max-implied-growth", 0, "optional maximum implied growth rate in percent; 0 disables the filter")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth rate in percent")
	universeFile := flags.String("universe", "", "optional file restricting screening to these tickers")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "prices", "min-score", "min-roic", "min-consistency", "max-implied-growth", "risk-free", "erp", "terminal", "universe", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("arbitrage accepts tickers through --universe FILE")
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
	var tickers []string
	var err error
	if *universeFile != "" {
		tickers, err = loadTickerUniverse(nil, *universeFile)
		if err != nil {
			return err
		}
	}
	marketCaps, prices, err := sec.LoadValuationInputs(*pricesFile)
	if err != nil {
		return err
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStoreTimeout(parent, databasePath, "", 30*time.Minute, func(ctx context.Context, store *sec.Store) error {
		rankResult, err := store.RankCompanies(ctx, 6, 100_000_000, tickers, true)
		if err != nil {
			return err
		}
		if *fetchPrices {
			missing := make([]string, 0)
			for _, company := range rankResult.Companies {
				if company.Score < *minScore || (*minROIC > 0 && (!company.HasROIC || company.ROIC < *minROIC/100)) ||
					(*minConsistency > 0 && (!company.HasOperatingMargin || company.OperatingConsistency < *minConsistency/100)) ||
					(*requireBuybacks && (!company.HasShareTrend || company.ShareReduction < 0)) {
					continue
				}
				if valuationInput(marketCaps, company.Ticker) <= 0 && valuationInput(prices, company.Ticker) <= 0 {
					missing = append(missing, company.Ticker)
				}
			}
			if len(missing) > 0 {
				if _, err := fmt.Fprintf(errorOutput, "Fetching live quotes for %d quality-qualified candidates…\n", len(missing)); err != nil {
					return err
				}
				for ticker, price := range sec.FetchLivePrices(ctx, missing, errorOutput) {
					prices[ticker] = price
				}
			}
		}
		waccConfig := &sec.WACCConfig{
			RiskFreeRate: *riskFree / 100, EquityRiskPremium: *equityRiskPremium / 100,
			Beta: 1, CostOfDebt: 0.05, TaxRate: 0.21,
			UseEffectiveTaxRate: true, UseEstimatedCostDebt: true,
		}
		filter := sec.ArbitrageFilter{
			MinScore: *minScore, MinROIC: *minROIC / 100,
			MinOperatingConsist: *minConsistency / 100,
			RequireBuybacks:     *requireBuybacks, MaxImpliedGrowth: *maxImpliedGrowth / 100,
			DCFConfig: sec.DCFConfig{
				Mode: sec.DCFModeOwnerEarnings, TerminalRate: *terminal / 100,
				Years: 5, WACC: waccConfig,
			},
		}
		candidates, err := store.ScreenExpectationsArbitrage(ctx, rankResult.Companies, marketCaps, prices, filter)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			_, err := fmt.Fprintln(output, "No companies matched the quality and valuation inputs. Provide --prices FILE or pass --fetch-prices.")
			return err
		}
		if _, err := fmt.Fprintf(output, "Quality vs. Implied Expectations Arbitrage — %d compounders evaluated\n", len(candidates)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Quality gate: Score ≥ %.0f | 5Y ROIC ≥ %.1f%% | Operating consistency ≥ %.0f%% | Buybacks required: %v\n", *minScore, *minROIC, *minConsistency, *requireBuybacks); err != nil {
			return err
		}
		return sec.WriteArbitrageTable(output, candidates)
	})
}

func valuationInput(values map[string]float64, ticker string) float64 {
	if value, ok := values[strings.ToUpper(strings.TrimSpace(ticker))]; ok {
		return value
	}
	for key, value := range values {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(ticker)) {
			return value
		}
	}
	return 0
}

func runModernGAAPArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("gaap-arbitrage", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec gaap-arbitrage [--prices FILE] [--fetch-prices] [--min-adj-roic 15] [--min-yield 3.5] [--surging] [--screen-expensive] [--max-gaap-roic PCT] [--sort yield|roic|spread|score] [--include-financials] [--universe FILE] [--years N] [--data-dir DIR]")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", false, "fetch live Yahoo Finance quotes (Stooq fallback) for candidates missing from --prices")
	minAdjROIC := flags.Float64("min-adj-roic", 15.0, "minimum Intangible-Adjusted ROIC in percent")
	minYield := flags.Float64("min-yield", 3.5, "minimum Intangible-Adjusted Owner Earnings Yield in percent; 0 disables yield filter")
	surging := flags.Bool("surging", false, "require surging/expanding Intangible-Adjusted ROIC")
	screenExpensive := flags.Bool("screen-expensive", false, "require company to appear expensive or unprofitable under GAAP (P/E >= 25, loss, or ROIC spread >= 5%)")
	maxGAAPROIC := flags.Float64("max-gaap-roic", 0.0, "optional ceiling on standard GAAP ROIC in percent to isolate distorted firms")
	sortBy := flags.String("sort", "yield", "sort table by: yield, roic, spread, or score")
	includeFinancials := flags.Bool("include-financials", false, "include financial-sector SIC codes 6000-6799 (excluded by default)")
	universeFile := flags.String("universe", "", "optional file restricting screening to these tickers")
	years := flags.Int("years", 5, "years of history to analyze")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := parseCommandFlags(flags, args, "prices", "min-adj-roic", "min-yield", "max-gaap-roic", "sort", "include-financials", "universe", "years", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("gaap-arbitrage accepts tickers through --universe FILE")
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

	var tickers []string
	var err error
	if *universeFile != "" {
		tickers, err = loadTickerUniverse(nil, *universeFile)
		if err != nil {
			return err
		}
	}

	marketCaps, prices, err := sec.LoadValuationInputs(*pricesFile)
	if err != nil {
		return err
	}

	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStoreTimeout(parent, databasePath, "", 30*time.Minute, func(ctx context.Context, store *sec.Store) error {
		companies, err := store.Companies(ctx)
		if err != nil {
			return err
		}
		if *fetchPrices {
			tickerSet := make(map[string]bool)
			for _, t := range tickers {
				tickerSet[strings.ToUpper(strings.TrimSpace(t))] = true
			}
			missing := make([]string, 0)
			for _, company := range companies {
				if len(tickers) > 0 && !tickerSet[strings.ToUpper(company.Ticker)] {
					continue
				}
				if valuationInput(marketCaps, company.Ticker) <= 0 && valuationInput(prices, company.Ticker) <= 0 {
					missing = append(missing, company.Ticker)
				}
			}
			if len(missing) > 0 {
				if _, err := fmt.Fprintf(errorOutput, "Fetching live quotes for %d candidates…\n", len(missing)); err != nil {
					return err
				}
				for ticker, price := range sec.FetchLivePrices(ctx, missing, errorOutput) {
					prices[ticker] = price
				}
			}
		}

		filter := sec.ModernGAAPFilter{
			MinAdjustedROIC:      *minAdjROIC / 100.0,
			MinOwnerYield:        *minYield / 100.0,
			RequireSurging:       *surging,
			RequireGAAPExpensive: *screenExpensive,
			MaxGAAPROIC:          *maxGAAPROIC / 100.0,
			Years:                *years,
			SortBy:               *sortBy,
			IncludeFinancials:    *includeFinancials,
		}

		candidates, err := store.ScreenModernGAAPArbitrage(ctx, tickers, marketCaps, prices, filter)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			if *pricesFile == "" && !*fetchPrices && *minYield > 0 {
				_, err := fmt.Fprintln(output, "No companies matched. Pass --fetch-prices to fetch live quotes, supply --prices FILE, or set --min-yield 0.")
				return err
			}
			_, err := fmt.Fprintln(output, "No companies matched the Intangible-Capitalized screening criteria. Try adjusting --min-adj-roic or --min-yield.")
			return err
		}
		if _, err := fmt.Fprintf(output, "Modern GAAP Arbitrage — %d Intangible-Capitalized compounders identified\n", len(candidates)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Screen: Adj ROIC ≥ %.1f%% | Owner Yield ≥ %.1f%% | Surging required: %v | GAAP expensive required: %v\n", *minAdjROIC, *minYield, *surging, *screenExpensive); err != nil {
			return err
		}
		return sec.WriteModernGAAPTable(output, candidates)
	})
}

func runAudit(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec audit [TICKER] [--gaps-only] [--data-dir DIR]")
	gapsOnly := flags.Bool("gaps-only", false, "only list companies that have missing data")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := parseCommandFlags(flags, args, "gaps-only", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	databasePath := sharedDatabasePath(*dataDir)
	positionals := flags.Args()

	// If a specific ticker is passed: run deep diagnostic
	if len(positionals) == 1 {
		ticker, err := commandTicker(positionals)
		if err != nil {
			return err
		}
		return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
			return store.DiagnoseTicker(ctx, ticker, output)
		})
	}
	if len(positionals) > 1 {
		return errors.New("audit accepts at most one ticker")
	}

	// Full universe scan
	return withExistingStoreTimeout(parent, databasePath, "", 5*time.Minute, func(ctx context.Context, store *sec.Store) error {
		results, err := store.AuditDatabase(ctx)
		if err != nil {
			return err
		}
		return sec.WriteAuditSummary(output, results, *gapsOnly)
	})
}

func runRead(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec read TICKER [--data-dir DIR]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	databasePath := sharedDatabasePath(*dataDir)
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no saved data for %s; run sec sync %s first", ticker, ticker)
		}
		return fmt.Errorf("inspect company database: %w", err)
	}
	return sec.ReadFilings(parent, databasePath, ticker, os.Stdin, output)
}

func runList(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec list [--data-dir DIR]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("list does not take positional arguments")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	companies, err := sec.ListCompanies(ctx, sharedDatabasePath(*dataDir))
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "TICKER\tCOMPANY\tCIK"); err != nil {
		return err
	}
	for _, company := range companies {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\n", company.Ticker, company.Name, company.CIK); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func runExport(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec export TICKER [--format csv|json] [--data-dir DIR]")
	format := flags.String("format", "csv", "export format: csv or json")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "format", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *format != "csv" && *format != "json" {
		return errors.New("--format must be csv or json")
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		facts, err := store.LoadFacts(ctx, ticker)
		if err != nil {
			return err
		}
		if *format == "json" {
			encoder := json.NewEncoder(output)
			encoder.SetIndent("", "  ")
			return encoder.Encode(facts)
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"company", "cik", "ticker", "taxonomy", "concept", "label", "unit", "value", "period_start", "period_end", "filing_date", "form", "accession_number", "frame"}); err != nil {
			return err
		}
		for _, fact := range facts {
			if err := writer.Write([]string{
				fact.Company, fact.CIK, fact.Ticker, fact.Taxonomy, fact.Concept, fact.Label,
				fact.Unit, fmt.Sprintf("%g", fact.Value), fact.Start, fact.End, fact.FilingDate,
				fact.Form, fact.Accession, fact.Frame,
			}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	})
}

func runRank(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("rank", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec rank [--universe FILE] [--years 5|6] [--min-revenue USD] [--exclude-financials] [--data-dir DIR]")
	universeFile := flags.String("universe", "", "optional file restricting ranking to these tickers")
	years := flags.Int("years", 6, "annual periods to use (6 periods provide five-year trends)")
	minimumRevenue := flags.Float64("min-revenue", 100_000_000, "minimum latest annual revenue in USD")
	excludeFinancials := flags.Bool("exclude-financials", false, "exclude SEC financial-sector SIC codes 6000-6799")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "universe", "years", "min-revenue", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("rank accepts tickers through --universe FILE")
	}
	if *years < 5 || *years > 6 {
		return errors.New("--years must be 5 or 6 annual periods; use 6 for five-year trends")
	}
	var tickers []string
	var err error
	if *universeFile != "" {
		tickers, err = loadTickerUniverse(nil, *universeFile)
		if err != nil {
			return err
		}
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStoreTimeout(parent, databasePath, "", 30*time.Minute, func(ctx context.Context, store *sec.Store) error {
		result, err := store.RankCompanies(ctx, *years, *minimumRevenue, tickers, *excludeFinancials)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Fundamental ranks: %d eligible of %d synced companies\n", len(result.Companies), result.Considered); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Filters: latest revenue ≥ $%.0f and positive net income in the latest three years. Nonfinancial companies also need positive latest operating cash flow; solvency is checked when cash and debt facts are present.\n", *minimumRevenue); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, "Factor percentiles use SEC SIC major groups; groups with fewer than five companies broaden to the SIC division. Financials and nonfinancials use separate score models and tables."); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, "SIC peer groups are a broad industry proxy, not GICS. Financial-company scores are preliminary; insurer combined ratios and investment-return metrics are not yet included."); err != nil {
			return err
		}
		if *excludeFinancials {
			if _, err := fmt.Fprintf(output, "Financial-sector companies excluded by SIC code: %d (6000-6799).\n", result.FinancialExcluded); err != nil {
				return err
			}
		}
		if result.SICUnavailable > 0 {
			if _, err := fmt.Fprintf(errorOutput, "Sector classification is unavailable for %d saved companies; sync them again to fetch SIC metadata. Eligible companies remain in the Unclassified peer tier.\n", result.SICUnavailable); err != nil {
				return err
			}
		}
		nonfinancial := make([]sec.RankedCompany, 0, len(result.Companies))
		financial := make([]sec.RankedCompany, 0, result.FinancialIncluded)
		unclassified := make([]sec.RankedCompany, 0, result.UnclassifiedIncluded)
		for _, company := range result.Companies {
			if company.Sector == "Financials" {
				financial = append(financial, company)
			} else if company.Sector == "Unclassified" {
				unclassified = append(unclassified, company)
			} else {
				nonfinancial = append(nonfinancial, company)
			}
		}
		if len(nonfinancial) > 0 {
			if _, err := fmt.Fprintln(output, "Nonfinancial companies — SIC peer ranking"); err != nil {
				return err
			}
			if err := writeNonfinancialRankTable(output, nonfinancial); err != nil {
				return err
			}
		}
		if len(financial) > 0 {
			if _, err := fmt.Fprintln(output, "Financial companies — separate preliminary ranking"); err != nil {
				return err
			}
			if err := writeFinancialRankTable(output, financial); err != nil {
				return err
			}
		}
		if len(unclassified) > 0 {
			if _, err := fmt.Fprintln(output, "Unclassified companies — provisional nonfinancial model"); err != nil {
				return err
			}
			if err := writeNonfinancialRankTable(output, unclassified); err != nil {
				return err
			}
		}
		if len(result.Missing) > 0 {
			fmt.Fprintf(errorOutput, "Not synced: %s\n", strings.Join(result.Missing, ", "))
		}
		if len(result.Companies) == 0 {
			fmt.Fprintln(output, "No companies passed the available-data and financial filters.")
		}
		if result.SBCAdjusted > 0 || result.SBCFallback > 0 {
			fmt.Fprintf(output, "Nonfinancial owner-earnings margin uses FCF less stock-based compensation where available (%d companies); %d use reported FCF. This is a proxy, not a GAAP measure or market FCF yield.\n", result.SBCAdjusted, result.SBCFallback)
		}
		if result.MissingSolvencyData > 0 {
			fmt.Fprintf(output, "Solvency data is incomplete for %d nonfinancial ranked companies; missing debt or cash was not treated as zero.\n", result.MissingSolvencyData)
		}
		trendYears := *years - 1
		_, err = fmt.Fprintf(output, "Nonfinancial weights: 20%% %d-year average ROIC, 15%% revenue CAGR, 15%% operating margin and positive-year consistency, 20%% owner-earnings margin proxy, 10%% net cash/assets, 10%% EPS/share growth, 10%% split-adjusted diluted-share reduction. Financial weights: 30%% %d-year ROE, 20%% revenue CAGR, 20%% book value/share growth, 20%% EPS/share growth, 10%% split-adjusted diluted-share reduction. Missing factors are omitted and remaining weights are renormalized. No market valuation or FCF yield is included.\n", trendYears, trendYears)
		return err
	})
}

func writeNonfinancialRankTable(output io.Writer, companies []sec.RankedCompany) error {
	companies = orderRankedPeerGroups(companies)
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	years := companies[0].TrendYears
	if _, err := fmt.Fprintf(writer, "RANK\tTICKER\tCOMPANY\tSIC PEER\tSCORE\tREVENUE\t%dY ROIC\tOP MARGIN\tMARGIN CONS.\tOWNER MARGIN\t%dY REV CAGR\tNET CASH/ASSETS\t%dY EPS CAGR\t%dY SHARE RED.\n", years, years, years, years); err != nil {
		return err
	}
	peerRank := 0
	peerGroup := ""
	for _, company := range companies {
		if company.PeerGroup != peerGroup {
			peerGroup = company.PeerGroup
			peerRank = 0
		}
		peerRank++
		shareLabel := formatRankPercent(company.ShareReduction, company.HasShareTrend)
		if company.SplitAdjusted {
			shareLabel += "*"
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%.1f\t$%.1fM\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			peerRank, company.Ticker, company.Company, company.PeerGroup, company.Score, company.Revenue/1e6,
			formatRankPercent(company.ROIC, company.HasROIC),
			formatRankPercent(company.OperatingMargin, company.HasOperatingMargin),
			formatRankPercent(company.OperatingConsistency, company.HasOperatingMargin),
			formatRankPercent(company.OwnerEarningsMargin, company.HasOwnerEarnings),
			formatRankPercent(company.RevenueCAGR, true),
			formatRankPercent(company.BalanceSheetQuality, company.HasBalanceSheet),
			formatRankPercent(company.EPSGrowth, company.HasEPSGrowth), shareLabel); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeFinancialRankTable(output io.Writer, companies []sec.RankedCompany) error {
	companies = orderRankedPeerGroups(companies)
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	years := companies[0].TrendYears
	if _, err := fmt.Fprintf(writer, "RANK\tTICKER\tCOMPANY\tSIC PEER\tSCORE\tREVENUE\t%dY ROE\t%dY REV CAGR\t%dY BV/SHARE CAGR\t%dY EPS/SHARE CAGR\t%dY SHARE RED.\n", years, years, years, years, years); err != nil {
		return err
	}
	peerRank := 0
	peerGroup := ""
	for _, company := range companies {
		if company.PeerGroup != peerGroup {
			peerGroup = company.PeerGroup
			peerRank = 0
		}
		peerRank++
		shareLabel := formatRankPercent(company.ShareReduction, company.HasShareTrend)
		if company.SplitAdjusted {
			shareLabel += "*"
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%.1f\t$%.1fM\t%s\t%s\t%s\t%s\t%s\n",
			peerRank, company.Ticker, company.Company, company.PeerGroup, company.Score, company.Revenue/1e6,
			formatRankPercent(company.ROE, company.HasROE),
			formatRankPercent(company.RevenueCAGR, true),
			formatRankPercent(company.BookValueGrowth, company.HasBookValueGrowth),
			formatRankPercent(company.EPSGrowth, company.HasEPSGrowth), shareLabel); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func formatRankPercent(value float64, available bool) string {
	if !available {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", value*100)
}

func orderRankedPeerGroups(companies []sec.RankedCompany) []sec.RankedCompany {
	ordered := append([]sec.RankedCompany(nil), companies...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].PeerGroup != ordered[j].PeerGroup {
			return ordered[i].PeerGroup < ordered[j].PeerGroup
		}
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score > ordered[j].Score
		}
		return ordered[i].Ticker < ordered[j].Ticker
	})
	return ordered
}

func setFlagUsage(flags *flag.FlagSet, errorOutput io.Writer, usageLine string) {
	flags.SetOutput(errorOutput)
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, usageLine)
		flags.PrintDefaults()
	}
}

func withExistingStore(parent context.Context, path, ticker string, action func(context.Context, *sec.Store) error) error {
	return withExistingStoreTimeout(parent, path, ticker, 15*time.Second, action)
}

func withExistingStoreTimeout(parent context.Context, path, ticker string, timeout time.Duration, action func(context.Context, *sec.Store) error) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if ticker == "" {
				return errors.New("no shared SEC database yet; sync a ticker or ticker universe first")
			}
			return fmt.Errorf("no saved data for %s; run sec sync %s first", ticker, ticker)
		}
		return fmt.Errorf("inspect company database: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	store, err := sec.OpenStore(ctx, path)
	if err != nil {
		return err
	}
	defer store.Close()
	return action(ctx, store)
}

func sharedDatabasePath(dataDir string) string {
	return filepath.Join(dataDir, "sec.db")
}

func loadTickerUniverse(args []string, path string) ([]string, error) {
	if path != "" && len(args) != 0 {
		return nil, errors.New("provide tickers or --universe FILE, not both")
	}
	rawTickers := append([]string(nil), args...)
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open ticker universe: %w", err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.ContainsAny(line, ",\t") {
				fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' })
				if len(fields) == 0 {
					continue
				}
				line = strings.TrimSpace(fields[0])
				line = strings.Trim(line, "\"'")
			}
			if strings.EqualFold(line, "ticker") || strings.EqualFold(line, "symbol") {
				continue
			}
			rawTickers = append(rawTickers, strings.Fields(line)...)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read ticker universe: %w", err)
		}
	}
	if len(rawTickers) == 0 {
		return nil, errors.New("provide at least one ticker or a non-empty --universe FILE")
	}
	seen := make(map[string]struct{}, len(rawTickers))
	tickers := make([]string, 0, len(rawTickers))
	for _, raw := range rawTickers {
		ticker, err := commandTicker([]string{raw})
		if err != nil {
			return nil, err
		}
		if _, ok := seen[ticker]; ok {
			continue
		}
		seen[ticker] = struct{}{}
		tickers = append(tickers, ticker)
	}
	return tickers, nil
}

func commandTicker(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("provide exactly one ticker symbol, for example AAPL")
	}
	ticker := strings.ToUpper(strings.TrimSpace(args[0]))
	if !tickerPattern.MatchString(ticker) || ticker == "." || ticker == ".." {
		return "", fmt.Errorf("invalid ticker %q", args[0])
	}
	return ticker, nil
}

// parseCommandFlags accepts the documented `command TICKER --option value`
// order while retaining the standard library flag parser and its validation.
func parseCommandFlags(flags *flag.FlagSet, args []string, valueFlags ...string) error {
	valueFlagSet := make(map[string]bool, len(valueFlags))
	for _, name := range valueFlags {
		valueFlagSet[name] = true
	}
	var options, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(arg, "-")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		if valueFlagSet[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			options = append(options, args[i+1])
			i++
		}
	}
	return flags.Parse(append(options, positionals...))
}
