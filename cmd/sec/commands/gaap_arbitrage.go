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
)

func RunModernGAAPArbitrage(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("gaap-arbitrage", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec gaap-arbitrage [--prices FILE] [--fetch-prices] [--min-adj-roic 15] [--min-yield 3.5] [--surging] [--screen-expensive] [--max-gaap-roic PCT] [--sort yield|roic|spread|score] [--include-financials] [--universe FILE] [--years N] [--data-dir DIR]")
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
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args, "prices", "min-adj-roic", "min-yield", "max-gaap-roic", "sort", "universe", "years", "data-dir"); err != nil {
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
		companies, err := s.Companies(ctx)
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
				if ValuationInput(marketCaps, company.Ticker) <= 0 && ValuationInput(prices, company.Ticker) <= 0 {
					missing = append(missing, company.Ticker)
				}
			}
			if len(missing) > 0 {
				if _, err := fmt.Fprintf(errorOutput, "Fetching live quotes for %d candidates…\n", len(missing)); err != nil {
					return err
				}
				for ticker, price := range market.FetchLivePrices(ctx, missing, errorOutput) {
					prices[ticker] = price
				}
			}
		}

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

		candidates, err := screener.ScreenModernGAAPArbitrage(ctx, s, tickers, marketCaps, prices, filter)
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
		return screener.WriteModernGAAPTable(output, candidates)
	})
}
