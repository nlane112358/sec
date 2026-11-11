package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

func RunRank(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("rank", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec rank [--universe FILE] [--years 5|6] [--min-revenue AMOUNT] [--currency USD|EUR|GBP] [--exclude-financials] [--data-dir DIR]")
	universeFile := flags.String("universe", "", "optional file restricting ranking to these tickers")
	years := flags.Int("years", 6, "annual periods to use (6 periods provide five-year trends)")
	minimumRevenue := flags.Float64("min-revenue", 100_000_000, "minimum latest annual revenue in USD (or --currency)")
	currencyFlag := flags.String("currency", "USD", "currency for --min-revenue threshold (default USD)")
	excludeFinancials := flags.Bool("exclude-financials", false, "exclude SEC financial-sector SIC codes 6000-6799")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "universe", "years", "min-revenue", "currency", "data-dir"); err != nil {
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
		tickers, err = LoadTickerUniverse(nil, *universeFile)
		if err != nil {
			return err
		}
	}
	minRevUSD := *minimumRevenue
	if *currencyFlag != "" && !strings.EqualFold(*currencyFlag, "USD") {
		minRevUSD = market.ToUSD(*minimumRevenue, *currencyFlag)
	}
	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStoreTimeout(parent, databasePath, "", 30*time.Minute, func(ctx context.Context, s *store.Store) error {
		result, err := screener.RankCompanies(ctx, s, *years, minRevUSD, tickers, *excludeFinancials)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Fundamental ranks: %d eligible of %d synced companies\n", len(result.Companies), result.Considered); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Filters: latest revenue ≥ %s%.0f and positive net income in the latest three years. Nonfinancial companies also need positive latest operating cash flow; solvency is checked when cash and debt facts are present.\n", market.CurrencySymbol(*currencyFlag), *minimumRevenue); err != nil {
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
		nonfinancial := make([]screener.RankedCompany, 0, len(result.Companies))
		financial := make([]screener.RankedCompany, 0, result.FinancialIncluded)
		unclassified := make([]screener.RankedCompany, 0, result.UnclassifiedIncluded)
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
			if err := screener.WriteNonfinancialRankTable(output, nonfinancial); err != nil {
				return err
			}
		}
		if len(financial) > 0 {
			if _, err := fmt.Fprintln(output, "Financial companies — separate preliminary ranking"); err != nil {
				return err
			}
			if err := screener.WriteFinancialRankTable(output, financial); err != nil {
				return err
			}
		}
		if len(unclassified) > 0 {
			if _, err := fmt.Fprintln(output, "Unclassified companies — provisional nonfinancial model"); err != nil {
				return err
			}
			if err := screener.WriteNonfinancialRankTable(output, unclassified); err != nil {
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
