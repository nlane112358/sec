package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
	"sec/internal/valuation"
)

func RunQualityDiscount(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("discount", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen discount [--prices FILE] [--fetch-prices] [--min-score 70] [--min-roic 12] [--min-discount 15] [--min-fcf-yield 0] [--require-buybacks] [--sort qaad|discount|quality|yield|upside] [--top N] [--universe FILE] [--data-dir DIR]")

	pricesFile := flags.String("prices", "", "CSV/TSV or JSON file with ticker prices or market caps")
	fetchPrices := flags.Bool("fetch-prices", false, "fetch live Yahoo Finance quotes (Stooq fallback) for candidates missing from --prices")
	minScore := flags.Float64("min-score", 70.0, "minimum composite quality score (0-100)")
	minROIC := flags.Float64("min-roic", 12.0, "minimum 5Y average ROIC in percent")
	minDiscount := flags.Float64("min-discount", 15.0, "minimum DCF discount / margin of safety in percent")
	minFCFYield := flags.Float64("min-fcf-yield", 0.0, "minimum FCF yield in percent (0 disables)")
	minConsistency := flags.Float64("min-consistency", 75.0, "minimum operating margin consistency in percent")
	requireBuybacks := flags.Bool("require-buybacks", false, "require non-dilutive or shrinking share count")
	includeFinancials := flags.Bool("include-financials", false, "include financial-sector SIC codes 6000-6799 (excluded by default)")
	growth := flags.Float64("growth", 0.0, "optional manual DCF growth rate in percent; 0 defaults to historical CAGR")
	discountRate := flags.Float64("discount", 0.0, "manual discount rate in percent; defaults to calculated WACC")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth rate in percent (default 2.5)")
	sortBy := flags.String("sort", "qaad", "sort table by: qaad, discount, quality, yield, upside")
	top := flags.Int("top", 0, "limit output to top N candidates (0 shows all)")
	universeFile := flags.String("universe", "", "optional file restricting screening to these tickers")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args, "prices", "min-score", "min-roic", "min-discount", "min-fcf-yield", "min-consistency", "growth", "discount", "terminal", "sort", "top", "universe", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if len(flags.Args()) != 0 {
		return errors.New("discount screen accepts tickers through --universe FILE")
	}

	for name, value := range map[string]float64{
		"--min-score":     *minScore,
		"--min-roic":      *minROIC,
		"--min-discount":  *minDiscount,
		"--min-fcf-yield": *minFCFYield,
		"--growth":        *growth,
		"--discount":      *discountRate,
		"--terminal":      *terminal,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s must be finite", name)
		}
	}

	if *minScore < 0 || *minScore > 100 {
		return errors.New("--min-score must be between 0 and 100")
	}
	if *minROIC < 0 || *minROIC > 500 {
		return errors.New("--min-roic must be between 0 and 500 percent")
	}
	if *top < 0 {
		return errors.New("--top must be non-negative")
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
		rankResult, err := screener.RankCompanies(ctx, s, 6, 100_000_000, tickers, !*includeFinancials)
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
			RiskFreeRate: 0.0484, EquityRiskPremium: 0.055,
			Beta: 1.0, CostOfDebt: 0.05, TaxRate: 0.21,
			UseEffectiveTaxRate: true, UseEstimatedCostDebt: true,
		}

		dcfConfig := valuation.DCFConfig{
			Mode:                valuation.DCFModeFCF,
			TerminalRate:        *terminal / 100.0,
			Years:               5,
			WACC:                waccConfig,
			UseHistoricalGrowth: *growth == 0,
		}
		if *growth != 0 {
			dcfConfig.GrowthRate = *growth / 100.0
		}
		if *discountRate > 0 {
			dcfConfig.DiscountRate = *discountRate / 100.0
		}

		filter := screener.QualityDiscountFilter{
			MinScore:            *minScore,
			MinROIC:             *minROIC / 100.0,
			MinDiscount:         *minDiscount / 100.0,
			MinFCFYield:         *minFCFYield / 100.0,
			RequireBuybacks:     *requireBuybacks,
			MinOperatingConsist: *minConsistency / 100.0,
			IncludeFinancials:   *includeFinancials,
			SortBy:              *sortBy,
			DCFConfig:           dcfConfig,
		}

		candidates, err := screener.ScreenQualityDiscount(ctx, s, rankResult.Companies, marketCaps, prices, filter)
		if err != nil {
			return err
		}

		if len(candidates) == 0 {
			_, err := fmt.Fprintln(output, "No companies matched the Quality-at-a-Discount criteria. Provide --prices FILE, pass --fetch-prices, or adjust --min-discount / --min-score thresholds.")
			return err
		}

		if *top > 0 && *top < len(candidates) {
			candidates = candidates[:*top]
		}

		if _, err := fmt.Fprintf(output, "Quality-at-a-Discount (QaaD) Screener — %d qualifying compounders\n", len(candidates)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Hurdles: Quality Score ≥ %.0f | 5Y ROIC ≥ %.1f%% | DCF Margin of Safety ≥ %.0f%% | Sort: %s\n",
			*minScore, *minROIC, *minDiscount, strings.ToUpper(*sortBy)); err != nil {
			return err
		}

		return screener.WriteQualityDiscountTable(output, candidates)
	})
}
