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
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
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
  read TICKER                 Browse and read saved filings
  list                        List companies in the shared database
  export TICKER --format FMT  Export saved facts as csv or json
  rank [--universe FILE]      Screen and rank the saved universe
  migrate                     Merge old per-ticker databases into sec.db

Options:
  --data-dir DIR  directory containing sec.db (default SEC_DATABASE_DIR or data)

Sync option:
  --ua STRING     SEC User-Agent including contact email (default SEC_USER_AGENT)

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
	case "migrate":
		return runMigrate(parent, args, output, errorOutput)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}

func runSync(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec sync TICKER... [--data-dir DIR] [--ua STRING]\n       sec sync --universe FILE [--data-dir DIR] [--ua STRING]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	universeFile := flags.String("universe", "", "file containing one ticker per line")
	if err := parseCommandFlags(flags, args, "data-dir", "ua", "universe"); err != nil {
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
	databasePath := sharedDatabasePath(*dataDir)
	store, err := sec.OpenStore(parent, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	client := sec.NewClient(*userAgent)
	var failures []error
	for i, ticker := range tickers {
		if err := parent.Err(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(parent, 90*time.Second)
		err := syncCompany(ctx, client, store, ticker, i+1, len(tickers), databasePath, output, errorOutput)
		cancel()
		if err != nil {
			fmt.Fprintf(errorOutput, "Sync failed for %s: %v\n", ticker, err)
			failures = append(failures, fmt.Errorf("%s: %w", ticker, err))
		}
	}
	return errors.Join(failures...)
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
	if err := store.SaveCompany(ctx, cik, companyName, ticker); err != nil {
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
	setFlagUsage(flags, errorOutput, "usage: sec dcf TICKER [--growth PCT] [--discount PCT] [--terminal PCT] [--years N] [--data-dir DIR]")
	growth := flags.Float64("growth", 0, "annual forecast growth in percent; default uses historical three-year FCF CAGR")
	discount := flags.Float64("discount", 9, "discount rate in percent")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth in percent")
	years := flags.Int("years", 5, "explicit forecast period in years (1-20)")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := parseCommandFlags(flags, args, "growth", "discount", "terminal", "years", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	growthProvided := false
	flags.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "growth" {
			growthProvided = true
		}
	})
	config := sec.DCFConfig{
		GrowthRate:          *growth / 100,
		UseHistoricalGrowth: !growthProvided,
		DiscountRate:        *discount / 100,
		TerminalRate:        *terminal / 100,
		Years:               *years,
	}
	databasePath := sharedDatabasePath(*dataDir)
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		summary, err := store.FinancialSummary(ctx, 5, ticker)
		if err != nil {
			return err
		}
		result, err := sec.CalculateDCF(summary, config)
		if err != nil {
			return err
		}
		return sec.WriteDCFResult(output, result)
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
	setFlagUsage(flags, errorOutput, "usage: sec rank [--universe FILE] [--years 4|5] [--min-revenue USD] [--data-dir DIR]")
	universeFile := flags.String("universe", "", "optional file restricting ranking to these tickers")
	years := flags.Int("years", 5, "annual history to use (4-5 years)")
	minimumRevenue := flags.Float64("min-revenue", 100_000_000, "minimum latest annual revenue in USD")
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
	if *years < 4 || *years > 5 {
		return errors.New("--years must be 4 or 5 for a three-year growth rate")
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
		result, err := store.RankCompanies(ctx, *years, *minimumRevenue, tickers)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Quality and growth rank: %d eligible of %d synced companies\n", len(result.Companies), result.Considered); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "Filters: latest revenue ≥ $%.0f, positive operating cash flow and net income, solvency check; score uses percentile ranks.\n", *minimumRevenue); err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "RANK\tTICKER\tCOMPANY\tSCORE\tREVENUE\tOP MARGIN\tFCF MARGIN\t3Y REV CAGR\tCASH CONV\t3Y SHARE RED."); err != nil {
			return err
		}
		for i, company := range result.Companies {
			if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%.1f\t$%.1fM\t%.1f%%\t%.1f%%\t%.1f%%\t%.2fx\t%.1f%%\n",
				i+1, company.Ticker, company.Company, company.Score, company.Revenue/1e6,
				company.OperatingMargin*100, company.FCFMargin*100, company.RevenueCAGR*100,
				company.CashConversion, company.ShareReduction*100); err != nil {
				return err
			}
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		if len(result.Missing) > 0 {
			fmt.Fprintf(errorOutput, "Not synced: %s\n", strings.Join(result.Missing, ", "))
		}
		if len(result.Companies) == 0 {
			fmt.Fprintln(output, "No companies passed the available-data and financial filters.")
		}
		_, err = fmt.Fprintln(output, "Scores rank financial quality and growth; they do not include market valuation.")
		return err
	})
}

func runMigrate(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec migrate [--data-dir DIR]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory containing old and shared databases")
	if err := parseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("migrate does not take positional arguments")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	databasePath := sharedDatabasePath(*dataDir)
	report, err := sec.MigrateCompanyDatabases(ctx, *dataDir, databasePath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "merged_databases=%d companies=%d facts=%d filings=%d database=%s\n",
		report.Databases, report.Companies, report.Facts, report.Filings, databasePath)
	return err
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
