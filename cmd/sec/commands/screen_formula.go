package commands

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"sec/internal/api"
	"sec/internal/market"
	"sec/internal/store"
)

func RunFormulaScreen(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("screen", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec screen [@universe|TICKER...] --where \"<formula>\" [--sort EXPR] [--asc] [--top N] [--columns A,B,C] [--format table|csv|json] [--prices FILE] [--refresh] [--offline] [--workers 1-4] [--data-dir DIR]")

	whereExpr := flags.String("where", "", "boolean/arithmetic filter formula (e.g. \"roic > 0.15 && fcf_yield > 0.05\")")
	universeFile := flags.String("universe", "", "optional universe alias (@sp500, @euro-uk, ...) or file")
	sortExpr := flags.String("sort", "", "sort metric or formula expression")
	sortAsc := flags.Bool("asc", false, "sort in ascending order")
	top := flags.Int("top", 0, "limit output to top N matching companies")
	columnsFlag := flags.String("columns", "", "comma-separated metrics to display")
	formatFlag := flags.String("format", "table", "output format: table, csv, or json")
	pricesFile := flags.String("prices", "", "optional CSV/TSV/JSON file with ticker prices or market caps")
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
	if strings.TrimSpace(*whereExpr) == "" {
		return errors.New("--where \"<formula>\" is required")
	}
	if *top < 0 {
		return errors.New("--top must be non-negative")
	}
	if err := validateScreenWorkers(*workers); err != nil {
		return err
	}
	format := strings.ToLower(strings.TrimSpace(*formatFlag))
	if format != "table" && format != "csv" && format != "json" {
		return errors.New("--format must be table, csv, or json")
	}

	var cols []string
	if strings.TrimSpace(*columnsFlag) != "" {
		for _, c := range strings.Split(*columnsFlag, ",") {
			if trimmed := strings.TrimSpace(c); trimmed != "" {
				cols = append(cols, trimmed)
			}
		}
	}

	marketCaps, prices, err := market.LoadValuationInputs(*pricesFile)
	if err != nil {
		return err
	}

	// Validate formula syntax upfront before hydrating large universes.
	if _, err := api.ParseFormula(*whereExpr); err != nil {
		return err
	}
	if strings.TrimSpace(*sortExpr) != "" {
		if _, err := api.ParseFormula(*sortExpr); err != nil {
			return fmt.Errorf("invalid --sort expression: %w", err)
		}
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

	var quoteSummary *QuoteHydrationSummary
	res, err := api.ScreenByFormula(ctx, s, api.FormulaScreenOptions{
		Tickers:       tickers,
		Where:         *whereExpr,
		SortExpr:      *sortExpr,
		SortAsc:       *sortAsc,
		Top:           *top,
		Columns:       cols,
		MarketCaps:    marketCaps,
		Prices:        prices,
		Offline:       *offline,
		RefreshQuotes: *refresh,
		OnQuoteHydrate: func(qCtx context.Context, missing []string) map[string]float64 {
			hydrated, qSum := EnsureUniverseQuotes(qCtx, s, missing, prices, marketCaps, hydrateOpts)
			quoteSummary = &qSum
			return hydrated
		},
	})
	if err != nil {
		return err
	}

	switch format {
	case "json":
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case "csv":
		w := csv.NewWriter(output)
		header := append([]string{"ticker", "company", "sector", "currency"}, res.Columns...)
		if err := w.Write(header); err != nil {
			return err
		}
		for _, c := range res.Candidates {
			row := []string{c.Ticker, c.Company, c.Sector, c.Currency}
			for _, col := range res.Columns {
				if val, ok := c.Metrics[col]; ok && val != nil {
					row = append(row, fmt.Sprintf("%v", val))
				} else {
					row = append(row, "")
				}
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	default:
		WriteBatchFreshnessHeader(output, universeLabel, res.Considered, batch, quoteSummary)
		return api.WriteFormulaScreenTable(output, res)
	}
}
