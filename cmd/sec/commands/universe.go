package commands

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"sec/internal/store"
)

type UniverseInfo struct {
	Name        string
	TotalCount  int
	SyncedCount int
	Description string
}

func RunUniverse(parent context.Context, args []string, output, errorOutput io.Writer) error {
	mode := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	if mode != "list" && mode != "status" {
		return fmt.Errorf("unknown universe command %q; choose list or status", mode)
	}

	flags := flag.NewFlagSet("universe", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec universe [list|status] [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("universe accepts only the list or status subcommand")
	}

	universes, err := loadUniverses()
	if err != nil {
		return err
	}
	if mode == "list" {
		return writeUniverseList(output, universes)
	}

	databasePath := SharedDatabasePath(*dataDir)
	s, err := store.Open(parent, databasePath)
	if err != nil {
		return err
	}
	defer s.Close()

	for i := range universes {
		for _, ticker := range universes[i].tickers {
			if _, _, _, err := s.CompanyForTicker(parent, ticker); err == nil {
				universes[i].SyncedCount++
			}
		}
	}
	return writeUniverseStatus(output, universes)
}

type universeRoster struct {
	UniverseInfo
	tickers []string
}

func loadUniverses() ([]universeRoster, error) {
	const universesDir = "universes"
	entries, err := os.ReadDir(universesDir)
	if err != nil {
		return nil, fmt.Errorf("read universes directory: %w", err)
	}

	descriptions := map[string]string{
		"sp500.txt":                   "S&P 500 Large-Cap US Benchmark",
		"nasdaq100.txt":               "Nasdaq 100 Tech & Growth Leaders",
		"midcap400.txt":               "S&P MidCap 400 Core Mid-Caps",
		"russell2000.txt":             "Russell 2000 US Small-Cap Universe",
		"adr_foreign.txt":             "Global Foreign ADRs (Form 20-F / 40-F Cross-Border Filers)",
		"broad_market_3000.txt":       "Broad Market 3000+ (S&P 500, MidCap 400, Russell 2000, Foreign ADRs)",
		"russell3000.txt":             "Russell 3000 Comprehensive US Investable Universe",
		"euro_uk_compounders_20f.txt": "European & UK Compounders (Form 20-F Filers)",
		"all-tickers.txt":             "Full SEC EDGAR Master Ticker Directory (~10,000+ filers)",
	}

	var results []universeRoster
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		path := filepath.Join(universesDir, entry.Name())
		tickers, err := readTickersFromFile(path)
		if err != nil || len(tickers) == 0 {
			continue
		}

		desc := descriptions[entry.Name()]
		if desc == "" {
			desc = "Custom Universe"
		}

		name := strings.TrimSuffix(entry.Name(), ".txt")
		results = append(results, universeRoster{
			UniverseInfo: UniverseInfo{
				Name:        name,
				TotalCount:  len(tickers),
				Description: desc,
			},
			tickers: tickers,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func writeUniverseList(output io.Writer, universes []universeRoster) error {
	tw := tabwriter.NewWriter(output, 0, 4, 3, ' ', 0)
	if _, err := fmt.Fprintln(tw, "UNIVERSE\tTICKERS\tDESCRIPTION"); err != nil {
		return err
	}
	for _, u := range universes {
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%s\n", u.Name, u.TotalCount, u.Description); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeUniverseStatus(output io.Writer, universes []universeRoster) error {
	tw := tabwriter.NewWriter(output, 0, 4, 3, ' ', 0)
	if _, err := fmt.Fprintln(tw, "UNIVERSE\tTICKERS\tSYNCED\tCOVERAGE"); err != nil {
		return err
	}
	for _, u := range universes {
		pct := 0.0
		if u.TotalCount > 0 {
			pct = float64(u.SyncedCount) / float64(u.TotalCount) * 100
		}
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\n", u.Name, u.TotalCount, u.SyncedCount, pct); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func readTickersFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var tickers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, ",\t") {
			fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' })
			if len(fields) > 0 {
				line = strings.TrimSpace(fields[0])
			}
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if !strings.EqualFold(f, "ticker") && !strings.EqualFold(f, "symbol") {
				tickers = append(tickers, strings.ToUpper(f))
			}
		}
	}
	return tickers, scanner.Err()
}
