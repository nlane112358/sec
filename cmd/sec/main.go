package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sec/internal/sec"
	"sec/internal/sqlite"
)

const usage = `usage: sec -ticker SYMBOL [options]

Fetch SEC company facts and filings, or read saved filings in the terminal.

options:
  -ticker string    company ticker symbol, e.g. AAPL (required)
  -data-dir string  directory for per-company SQLite databases (default SEC_DATABASE_DIR or data)
  -ua string        SEC User-Agent including contact email (default SEC_USER_AGENT)
  -read             browse and read saved filings in the terminal
`

var tickerPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,14}$`)

func run() error {
	tickerFlag := flag.String("ticker", "", "company ticker symbol, e.g. AAPL")
	dataDir := flag.String("data-dir", sqlite.DefaultDirectory(), "directory for per-company SQLite databases")
	userAgent := flag.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	read := flag.Bool("read", false, "browse and read saved filings in the terminal")
	flag.Usage = func() { fmt.Fprint(flag.CommandLine.Output(), usage) }
	flag.CommandLine.SetOutput(os.Stderr)
	flag.Parse()

	ticker := strings.ToUpper(strings.TrimSpace(*tickerFlag))
	if !tickerPattern.MatchString(ticker) || ticker == "." || ticker == ".." {
		return errors.New("provide a valid -ticker symbol, for example AAPL")
	}
	if *userAgent == "" {
		return errors.New("provide -ua 'Research App contact@example.com' or set SEC_USER_AGENT")
	}
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	databasePath := filepath.Join(*dataDir, ticker+".db")

	if *read {
		return sec.ReadFilings(context.Background(), databasePath, ticker, *userAgent, os.Stdin, os.Stdout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := sec.NewClient(*userAgent)
	cik, err := client.ResolveTicker(ctx, ticker)
	if err != nil {
		return err
	}
	submissions, err := client.Submissions(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC filing submissions: %w", err)
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
	fmt.Printf("saved_facts=%d saved_filings=%d ticker=%s cik=%s database=%s\n", len(facts), len(filings), ticker, cik, databasePath)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
