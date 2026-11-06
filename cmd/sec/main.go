package main

import (
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
  sync TICKER                 Download or refresh facts and filings
  summary TICKER [--years N]  Show annual financial metrics (3-5 years)
  read TICKER                 Browse and read saved filings
  list                        List locally cached companies
  export TICKER --format FMT  Export saved facts as csv or json

Options:
  --data-dir DIR  directory for per-company SQLite databases (default SEC_DATABASE_DIR or data)

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
	case "read":
		return runRead(parent, args, output, errorOutput)
	case "list":
		return runList(parent, args, output, errorOutput)
	case "export":
		return runExport(parent, args, output, errorOutput)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}

func runSync(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec sync TICKER [--data-dir DIR] [--ua STRING]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	if err := parseCommandFlags(flags, args, "data-dir", "ua"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := commandTicker(flags.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(*userAgent) == "" {
		return errors.New("provide --ua 'Research App contact@example.com' or set SEC_USER_AGENT")
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	databasePath := filepath.Join(*dataDir, ticker+".db")
	client := sec.NewClient(*userAgent)
	if _, err := fmt.Fprintf(errorOutput, "Resolving %s…\n", ticker); err != nil {
		return err
	}
	cik, err := client.ResolveTicker(ctx, ticker)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(errorOutput, "Fetching filing history…"); err != nil {
		return err
	}
	submissions, err := client.Submissions(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC filing submissions: %w", err)
	}
	if _, err := fmt.Fprintln(errorOutput, "Fetching company facts…"); err != nil {
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
	if _, err := fmt.Fprintf(errorOutput, "Saving %d facts and %d filings…\n", len(facts), len(filings)); err != nil {
		return err
	}
	store, err := sec.OpenStore(ctx, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()
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
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
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
	databasePath := filepath.Join(*dataDir, ticker+".db")
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		summary, err := store.FinancialSummary(ctx, *years)
		if err != nil {
			return err
		}
		return sec.WriteFinancialSummary(output, summary)
	})
}

func runRead(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	setFlagUsage(flags, errorOutput, "usage: sec read TICKER [--data-dir DIR]")
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
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
	databasePath := filepath.Join(*dataDir, ticker+".db")
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
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
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
	companies, err := sec.ListCompanies(ctx, *dataDir)
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
	dataDir := flags.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
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
	databasePath := filepath.Join(*dataDir, ticker+".db")
	return withExistingStore(parent, databasePath, ticker, func(ctx context.Context, store *sec.Store) error {
		facts, err := store.LoadFacts(ctx)
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

func setFlagUsage(flags *flag.FlagSet, errorOutput io.Writer, usageLine string) {
	flags.SetOutput(errorOutput)
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, usageLine)
		flags.PrintDefaults()
	}
}

func withExistingStore(parent context.Context, path, ticker string, action func(context.Context, *sec.Store) error) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no saved data for %s; run sec sync %s first", ticker, ticker)
		}
		return fmt.Errorf("inspect company database: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	store, err := sec.OpenStore(ctx, path)
	if err != nil {
		return err
	}
	defer store.Close()
	return action(ctx, store)
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
