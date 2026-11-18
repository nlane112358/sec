package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

func RunRank(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("rank", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen rank [@universe|TICKER...] [--universe FILE] [--top N] [--years 5|6] [--min-revenue AMOUNT] [--currency USD|EUR|GBP] [--exclude-financials] [--where EXPR] [--refresh] [--offline] [--workers 1-4] [--data-dir DIR]")
	universeFile := flags.String("universe", "", "optional universe alias (@sp500, @euro-uk, ...) or file")
	whereExpr := flags.String("where", "", "additional formula filter expression")
	top := flags.Int("top", 0, "limit output to the top N companies by composite score (0 shows all)")
	years := flags.Int("years", 6, "annual periods to use (6 periods provide five-year trends)")
	minimumRevenue := flags.Float64("min-revenue", 100_000_000, "minimum latest annual revenue in USD (or --currency)")
	currencyFlag := flags.String("currency", "USD", "currency for --min-revenue threshold (default USD)")
	excludeFinancials := flags.Bool("exclude-financials", false, "exclude SEC financial-sector SIC codes 6000-6799")
	refresh := flags.Bool("refresh", false, "force fresh EDGAR and FX pulls")
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
	if *years < 5 || *years > 6 {
		return errors.New("--years must be 5 or 6 annual periods; use 6 for five-year trends")
	}
	if *top < 0 {
		return errors.New("--top must be zero or a positive number")
	}
	if err := validateScreenWorkers(*workers); err != nil {
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
		Offline:     *offline,
		UserAgent:   *userAgent,
		Workers:     *workers,
		ErrorOutput: errorOutput,
	}
	tickers, universeLabel, batch, err := prepareScreenUniverse(ctx, s, flags.Args(), *universeFile, hydrateOpts)
	if err != nil {
		return err
	}

	minRevUSD := *minimumRevenue
	if *currencyFlag != "" && !strings.EqualFold(*currencyFlag, "USD") {
		ensureCurrencyFX(ctx, s, *currencyFlag, hydrateOpts, nil)
		minRevUSD = market.ToUSD(*minimumRevenue, *currencyFlag)
	}

	result, err := screener.RankCompanies(ctx, s, *years, minRevUSD, tickers, *excludeFinancials)
	if err != nil {
		return err
	}
	rankedCompanies := result.Companies

	if strings.TrimSpace(*whereExpr) != "" && len(rankedCompanies) > 0 {
		candTickers := make([]string, len(rankedCompanies))
		for i, c := range rankedCompanies {
			candTickers[i] = c.Ticker
		}
		allowed, wErr := filterTickersByWhere(ctx, s, *whereExpr, candTickers, nil, nil, hydrateOpts)
		if wErr != nil {
			return wErr
		}
		var filtered []screener.RankedCompany
		for _, c := range rankedCompanies {
			if allowed[c.Ticker] {
				filtered = append(filtered, c)
			}
		}
		rankedCompanies = filtered
	}

	if *top > 0 && *top < len(rankedCompanies) {
		rankedCompanies = append([]screener.RankedCompany(nil), rankedCompanies...)
		sort.Slice(rankedCompanies, func(i, j int) bool {
			if rankedCompanies[i].Score != rankedCompanies[j].Score {
				return rankedCompanies[i].Score > rankedCompanies[j].Score
			}
			return rankedCompanies[i].Ticker < rankedCompanies[j].Ticker
		})
		rankedCompanies = rankedCompanies[:*top]
	}

	WriteBatchFreshnessHeader(output, universeLabel, result.Considered, batch, nil)
	if _, err := fmt.Fprintf(output, "Fundamental ranks: %d eligible of %d evaluated companies\n", len(rankedCompanies), result.Considered); err != nil {
		return err
	}
	if *top > 0 {
		if _, err := fmt.Fprintf(output, "Showing up to the top %d eligible companies by composite score.\n", *top); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(output, "Filters: latest revenue ≥ %s%.0f and positive net income in the latest three years. Nonfinancial companies also need positive latest operating cash flow; solvency is checked when cash and debt facts are present.\n", market.CurrencySymbol(*currencyFlag), *minimumRevenue); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Factor percentiles use SEC SIC major groups; groups with fewer than five companies broaden to the SIC division. Financials and nonfinancials use separate score models and tables."); err != nil {
		return err
	}
	if *excludeFinancials {
		if _, err := fmt.Fprintf(output, "Financial-sector companies excluded by SIC code: %d (6000-6799).\n", result.FinancialExcluded); err != nil {
			return err
		}
	}
	if result.SICUnavailable > 0 {
		if _, err := fmt.Fprintf(errorOutput, "Sector classification is unavailable for %d saved companies; run with --refresh to fetch SIC metadata.\n", result.SICUnavailable); err != nil {
			return err
		}
	}

	nonfinancial := make([]screener.RankedCompany, 0, len(rankedCompanies))
	financial := make([]screener.RankedCompany, 0, len(rankedCompanies))
	unclassified := make([]screener.RankedCompany, 0, len(rankedCompanies))
	for _, company := range rankedCompanies {
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
	if len(rankedCompanies) == 0 {
		fmt.Fprintln(output, "No companies passed the available-data and financial filters.")
	}
	if result.SBCAdjusted > 0 || result.SBCFallback > 0 {
		fmt.Fprintf(output, "Nonfinancial owner-earnings margin uses FCF less stock-based compensation where available (%d companies); %d use reported FCF.\n", result.SBCAdjusted, result.SBCFallback)
	}
	if result.MissingSolvencyData > 0 {
		fmt.Fprintf(output, "Solvency data is incomplete for %d nonfinancial ranked companies; missing debt or cash was not treated as zero.\n", result.MissingSolvencyData)
	}
	trendYears := *years - 1
	_, err = fmt.Fprintf(output, "Nonfinancial weights: 20%% %d-year average ROIC, 15%% revenue CAGR, 15%% operating margin and positive-year consistency, 20%% owner-earnings margin proxy, 10%% net cash/assets, 10%% EPS/share growth, 10%% split-adjusted diluted-share reduction. Financial weights: 30%% %d-year ROE, 20%% revenue CAGR, 20%% book value/share growth, 20%% EPS/share growth, 10%% split-adjusted diluted-share reduction.\n", trendYears, trendYears)
	return err
}
