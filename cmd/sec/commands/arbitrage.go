package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"time"

	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
	"sec/internal/valuation"
)

func RunArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("arbitrage", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec arbitrage [--prices FILE] [--fetch-prices] [--min-score 75] [--min-roic 12] [--require-buybacks] [--universe FILE] [--data-dir DIR]")
	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", false, "fetch live Yahoo Finance quotes (Stooq fallback) for candidates missing from --prices")
	minScore := flags.Float64("min-score", 70, "minimum sec rank composite quality score (0-100)")
	minROIC := flags.Float64("min-roic", 12, "minimum multi-year average ROIC in percent")
	minConsistency := flags.Float64("min-consistency", 80, "minimum operating margin consistency in percent")
	requireBuybacks := flags.Bool("require-buybacks", true, "require non-dilutive or shrinking share count")
	maxImpliedGrowth := flags.Float64("max-implied-growth", 0, "optional maximum implied growth rate in percent; 0 disables the filter")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent")
	profileFlag := flags.String("profile", "", "macro profile for regional risk-free rate: us, eu, uk, ch")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth rate in percent")
	universeFile := flags.String("universe", "", "optional file restricting screening to these tickers")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "prices", "min-score", "min-roic", "min-consistency", "max-implied-growth", "risk-free", "profile", "erp", "terminal", "universe", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	riskFreeProvided := false
	flags.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "risk-free" {
			riskFreeProvided = true
		}
	})
	if *profileFlag != "" && !riskFreeProvided {
		profile := market.ResolveMacroProfile(*profileFlag)
		*riskFree = profile.RiskFreeRate
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
		tickers, err = LoadTickerUniverse(nil, *universeFile)
		if err != nil {
			return err
		}
	}
	marketCaps, prices, err := market.LoadValuationInputs(*pricesFile)
	if err != nil {
		return err
	}
	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStoreTimeout(parent, databasePath, "", 30*time.Minute, func(ctx context.Context, s *store.Store) error {
		rankResult, err := screener.RankCompanies(ctx, s, 6, 100_000_000, tickers, true)
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
				if ValuationInput(marketCaps, company.Ticker) <= 0 && ValuationInput(prices, company.Ticker) <= 0 {
					missing = append(missing, company.Ticker)
				}
			}
			if len(missing) > 0 {
				if _, err := fmt.Fprintf(errorOutput, "Fetching live quotes for %d quality-qualified candidates…\n", len(missing)); err != nil {
					return err
				}
				for ticker, price := range market.FetchLivePrices(ctx, missing, errorOutput) {
					prices[ticker] = price
				}
			}
		}
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
		candidates, err := screener.ScreenExpectationsArbitrage(ctx, s, rankResult.Companies, marketCaps, prices, filter)
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
		return screener.WriteArbitrageTable(output, candidates)
	})
}
