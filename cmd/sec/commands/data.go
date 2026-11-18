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
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/screener"
	"sec/internal/store"
)

const dataUsage = `usage: sec data <subcommand> [options]

Subcommands:
  sync [TICKER...|@universe]   Fetch & precompute company facts/filings (with batch ETA)
  status [--list]               Show SQLite database, cache coverage, and freshness
  prune [--older-than 30d]      Clean expired quotes, FX rates, HTML filings, and vacuum DB
  export TICKER [--format csv]  Export company facts to CSV or JSON
  rebuild                       Recompute derived summaries and screening variables

Note: Normal commands (inspect, value, screen) hydrate and precompute automatically;
use "sec data" for explicit bulk management, inspection, or cleanup.

Examples:
  sec data status --list
  sec data sync @defense
  sec data prune --older-than 30d --dry-run
  sec data export AAPL --format json
`

func RunData(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(output, dataUsage)
		return err
	}

	subcmd, subArgs := strings.ToLower(strings.TrimSpace(args[0])), args[1:]
	switch subcmd {
	case "sync":
		return runDataSync(parent, subArgs, output, errorOutput)
	case "status", "list":
		if subcmd == "list" {
			subArgs = append([]string{"--list"}, subArgs...)
		}
		return runDataStatus(parent, subArgs, output, errorOutput)
	case "prune", "clean":
		return runDataPrune(parent, subArgs, output, errorOutput)
	case "export":
		return runDataExport(parent, subArgs, output, errorOutput)
	case "rebuild", "precompute":
		return RunPrecompute(parent, subArgs, output, errorOutput)
	default:
		return fmt.Errorf("unknown data subcommand %q; choose sync, status, prune, export, or rebuild\n\n%s", args[0], dataUsage)
	}
}

func runDataSync(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("data sync", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec data sync [TICKER...|@universe] [--workers 1-4] [--skip-existing] [--refresh] [--data-dir DIR] [--ua STRING]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	universeFile := flags.String("universe", "", "universe alias (@sp500, ...) or file")
	workers := flags.Int("workers", 4, "concurrent sync workers (1-4)")
	skipExisting := flags.Bool("skip-existing", false, "skip companies already present in the database")
	refresh := flags.Bool("refresh", false, "re-fetch companies even if already cached")
	force := flags.Bool("force", false, "alias for --refresh")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := validateScreenWorkers(*workers); err != nil {
		return err
	}

	tickers, err := LoadTickerUniverse(flags.Args(), *universeFile)
	if err != nil {
		return err
	}

	isExplicitTickerList := *universeFile == "" && (len(flags.Args()) > 0 && !strings.HasPrefix(strings.TrimSpace(flags.Args()[0]), "@"))
	forceRefresh := *refresh || *force || (isExplicitTickerList && !*skipExisting)
	if *skipExisting {
		forceRefresh = false
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, 60*time.Minute)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	res, err := HydrateTickersBatch(ctx, s, tickers, HydrateOptions{
		Refresh:     forceRefresh,
		UserAgent:   *userAgent,
		Workers:     *workers,
		ErrorOutput: errorOutput,
	})
	if err != nil {
		return err
	}

	if res.Fetched > 0 {
		for _, y := range screener.DefaultPrecomputeRankYears {
			if _, rErr := screener.RefreshFullUniverseRankScores(ctx, s, y); rErr != nil {
				return fmt.Errorf("refresh %dy rank scores: %w", y, rErr)
			}
		}
		_ = s.BumpCacheEpoch(ctx)
	}

	_, err = fmt.Fprintf(output, "sync complete: total=%d cache_hits=%d fetched=%d failed=%d\n",
		res.Total, res.CacheHits, res.Fetched, res.Failed)
	return err
}

func runDataStatus(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("data status", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec data status [--list] [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	listCompanies := flags.Bool("list", false, "list synced companies")
	companiesFlag := flags.Bool("companies", false, "alias for --list")
	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("data status does not take positional arguments")
	}

	dbPath := SharedDatabasePath(*dataDir)
	dbSize := int64(0)
	dbExists := false
	if info, err := os.Stat(dbPath); err == nil {
		dbSize = info.Size()
		dbExists = true
	}

	filingsCount, filingsBytes := dirFileStats(filepath.Join(*dataDir, "filings"))

	if !dbExists {
		_, err := fmt.Fprintf(output, "Database: %s (not initialized yet)\nRun any command like \"sec inspect AAPL\" to hydrate automatically.\n", dbPath)
		return err
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, 30*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	stats, err := s.DatabaseOverviewStats(ctx, fundamentals.SummarySchemaVersion, screener.RankSchemaVersion, screener.GAAPArbitrageSchemaVersion)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Database Path:\t%s (%s)\n", dbPath, formatByteSize(dbSize))
	_, _ = fmt.Fprintf(tw, "Companies:\t%d hydrated (%d registered)\n", stats.HydratedCount, stats.Companies)
	_, _ = fmt.Fprintf(tw, "Facts / Filings:\t%d facts · %d filing records\n", stats.Facts, stats.Filings)
	_, _ = fmt.Fprintf(tw, "Precompute:\t%d clean summaries · %d dirty · %d rank(5y) · %d rank(6y) · %d gaap-arb(5y)\n",
		stats.CleanSummaries, stats.DirtySummaries, stats.Rank5Count, stats.Rank6Count, stats.Arb5Count)
	_, _ = fmt.Fprintf(tw, "Live Cache:\t%d quotes · %d FX rates · epoch %d\n", stats.Quotes, stats.FXRates, stats.CacheEpoch)
	_, _ = fmt.Fprintf(tw, "HTML Filings Cache:\t%d files (%s)\n", filingsCount, formatByteSize(filingsBytes))
	if !stats.NewestSync.IsZero() {
		_, _ = fmt.Fprintf(tw, "Sync Window:\tnewest %s · oldest %s\n", FormatRelativeAge(stats.NewestSync), FormatRelativeAge(stats.OldestSync))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if *listCompanies || *companiesFlag {
		companies, err := s.Companies(ctx)
		if err != nil {
			return err
		}
		if len(companies) == 0 {
			_, _ = fmt.Fprintln(output, "\nNo companies cached yet.")
			return nil
		}
		_, _ = fmt.Fprintln(output)
		ctw := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(ctw, "TICKER\tCOMPANY\tCIK")
		for _, c := range companies {
			_, _ = fmt.Fprintf(ctw, "%s\t%s\t%s\n", c.Ticker, c.Name, c.CIK)
		}
		return ctw.Flush()
	}
	return nil
}

func runDataPrune(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("data prune", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec data prune [--older-than 30d] [--quotes] [--fx] [--filings] [--orphans] [--all] [--no-vacuum] [--dry-run] [--data-dir DIR]")
	olderThanStr := flags.String("older-than", "30d", "prune items older than duration (e.g. 24h, 7d, 30d)")
	quotesFlag := flags.Bool("quotes", false, "prune expired/stale cached quotes")
	fxFlag := flags.Bool("fx", false, "prune expired/stale cached FX rates")
	filingsFlag := flags.Bool("filings", false, "prune cached filing HTML files")
	orphansFlag := flags.Bool("orphans", false, "prune company rows with zero facts and filings")
	allFlag := flags.Bool("all", false, "prune all cached quotes, FX rates, and HTML filings regardless of age")
	noVacuum := flags.Bool("no-vacuum", false, "skip SQLite VACUUM after pruning")
	dryRun := flags.Bool("dry-run", false, "preview what would be removed without modifying files")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("data prune does not take positional arguments")
	}

	olderDur, err := parseFlexibleDuration(*olderThanStr)
	if err != nil {
		return fmt.Errorf("invalid --older-than %q: %w", *olderThanStr, err)
	}
	olderSet := registerFlagCheck(flags, "older-than")["older-than"]
	anySpecific := *quotesFlag || *fxFlag || *filingsFlag || *orphansFlag
	if !anySpecific {
		*quotesFlag = true
		*fxFlag = true
		*filingsFlag = true
		*orphansFlag = true
	}

	dbPath := SharedDatabasePath(*dataDir)
	if _, statErr := os.Stat(dbPath); errors.Is(statErr, os.ErrNotExist) {
		_, err := fmt.Fprintln(output, "Nothing to prune (database does not exist yet).")
		return err
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, 2*time.Minute)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	now := time.Now().UTC()
	quoteCutoff := now.Add(-24 * time.Hour)
	fxCutoff := now.Add(-7 * 24 * time.Hour)
	filingCutoff := now.Add(-olderDur)
	if olderSet {
		quoteCutoff = now.Add(-olderDur)
		fxCutoff = now.Add(-olderDur)
	}
	if *allFlag {
		quoteCutoff = time.Time{}
		fxCutoff = time.Time{}
		filingCutoff = time.Time{}
	}

	var quotesRemoved, fxRemoved, orphansRemoved int64
	var filingsRemoved int
	var filingsBytesReclaimed int64

	if *dryRun {
		if *quotesFlag {
			if qMap, qErr := s.LoadQuotes(ctx, nil); qErr == nil {
				for _, q := range qMap {
					if quoteCutoff.IsZero() || q.FetchedAt.IsZero() || q.FetchedAt.Before(quoteCutoff) {
						quotesRemoved++
					}
				}
			}
		}
		if *fxFlag {
			if fxList, fErr := s.LoadAllFXRates(ctx); fErr == nil {
				for _, fx := range fxList {
					if fxCutoff.IsZero() || fx.FetchedAt.IsZero() || fx.FetchedAt.Before(fxCutoff) {
						fxRemoved++
					}
				}
			}
		}
		if *orphansFlag {
			_ = s.DB().QueryRowContext(ctx, `
SELECT COUNT(*) FROM sec_companies c
WHERE NOT EXISTS (SELECT 1 FROM sec_facts f WHERE f.cik = c.cik)
  AND NOT EXISTS (SELECT 1 FROM sec_filings fl WHERE fl.cik = c.cik)`).Scan(&orphansRemoved)
		}
		if *filingsFlag {
			filingsRemoved, filingsBytesReclaimed = pruneFilingHTMLDir(filepath.Join(*dataDir, "filings"), filingCutoff, true)
		}
		_, err = fmt.Fprintf(output, "[dry-run] would prune: quotes=%d fx_rates=%d orphan_companies=%d filing_html=%d (%s)\n",
			quotesRemoved, fxRemoved, orphansRemoved, filingsRemoved, formatByteSize(filingsBytesReclaimed))
		return err
	}

	var sizeBefore int64
	if info, statErr := os.Stat(dbPath); statErr == nil {
		sizeBefore = info.Size()
	}

	if *quotesFlag {
		quotesRemoved, err = s.PruneQuotes(ctx, quoteCutoff)
		if err != nil {
			return err
		}
	}
	if *fxFlag {
		fxRemoved, err = s.PruneFXRates(ctx, fxCutoff)
		if err != nil {
			return err
		}
	}
	if *orphansFlag {
		orphansRemoved, err = s.PruneEmptyCompanies(ctx)
		if err != nil {
			return err
		}
	}
	if *filingsFlag {
		filingsRemoved, filingsBytesReclaimed = pruneFilingHTMLDir(filepath.Join(*dataDir, "filings"), filingCutoff, false)
	}
	if !*noVacuum && (quotesRemoved > 0 || fxRemoved > 0 || orphansRemoved > 0) {
		if err := s.Vacuum(ctx); err != nil {
			return err
		}
	}

	var sizeAfter int64
	if info, statErr := os.Stat(dbPath); statErr == nil {
		sizeAfter = info.Size()
	}
	dbSaved := sizeBefore - sizeAfter
	if dbSaved < 0 {
		dbSaved = 0
	}

	_, err = fmt.Fprintf(output, "pruned: quotes=%d fx_rates=%d orphan_companies=%d filing_html=%d reclaimed=%s\n",
		quotesRemoved, fxRemoved, orphansRemoved, filingsRemoved, formatByteSize(filingsBytesReclaimed+dbSaved))
	return err
}

func runDataExport(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("data export", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec data export TICKER [--format csv|json] [--refresh] [--offline] [--data-dir DIR]")
	format := flags.String("format", "csv", "export format: csv or json")
	refresh := flags.Bool("refresh", false, "force fresh EDGAR pull before exporting")
	offline := flags.Bool("offline", false, "use local SQLite cache only")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := CommandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *format != "csv" && *format != "json" {
		return errors.New("--format must be csv or json")
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, 2*time.Minute)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	if _, err := EnsureCompanyHydrated(ctx, s, ticker, HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline,
		UserAgent:   *userAgent,
		ErrorOutput: errorOutput,
	}); err != nil {
		return err
	}

	facts, err := s.LoadFacts(ctx, ticker)
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
}

func parseFlexibleDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return 0, errors.New("empty duration")
	}
	if strings.HasSuffix(trimmed, "d") {
		numStr := strings.TrimSuffix(trimmed, "d")
		days, err := strconv.ParseFloat(numStr, 64)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("invalid day duration %q", raw)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", raw)
	}
	return d, nil
}

func dirFileStats(dir string) (count int, bytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			count++
			bytes += info.Size()
		}
	}
	return count, bytes
}

func pruneFilingHTMLDir(dir string, cutoff time.Time, dryRun bool) (removed int, bytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if cutoff.IsZero() || info.ModTime().UTC().Before(cutoff) {
			if !dryRun {
				if rmErr := os.Remove(filepath.Join(dir, e.Name())); rmErr != nil {
					continue
				}
			}
			removed++
			bytes += info.Size()
		}
	}
	return removed, bytes
}

func formatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
