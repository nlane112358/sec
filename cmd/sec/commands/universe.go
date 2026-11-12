package commands

import (
	"bufio"
	"context"
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
	Path        string
	TotalCount  int
	SyncedCount int
	Description string
}

func RunUniverse(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("universe", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec universe [list] [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		return err
	}

	databasePath := SharedDatabasePath(*dataDir)
	s, err := store.Open(parent, databasePath)
	if err != nil {
		return err
	}
	defer s.Close()

	// Find universe files in universes/
	universesDir := "universes"
	entries, err := os.ReadDir(universesDir)
	if err != nil {
		return fmt.Errorf("read universes dir: %w", err)
	}

	descriptions := map[string]string{
		"sp500.txt":                  "S&P 500 Large-Cap US Benchmark",
		"nasdaq100.txt":              "Nasdaq 100 Tech & Growth Leaders",
		"midcap400.txt":              "S&P MidCap 400 Core Mid-Caps",
		"russell2000.txt":            "Russell 2000 US Small-Cap Universe",
		"adr_foreign.txt":            "Global Foreign ADRs (Form 20-F / 40-F Cross-Border Filers)",
		"broad_market_3000.txt":      "Broad Market 3000+ (S&P 500, MidCap 400, Russell 2000, Foreign ADRs)",
		"russell3000.txt":            "Russell 3000 Comprehensive US Investable Universe",
		"euro_uk_compounders_20f.txt": "European & UK Compounders (Form 20-F Filers)",
		"all-tickers.txt":            "Full SEC EDGAR Master Ticker Directory (~10,000+ filers)",
	}

	var results []UniverseInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		path := filepath.Join(universesDir, entry.Name())
		tickers, err := readTickersFromFile(path)
		if err != nil || len(tickers) == 0 {
			continue
		}

		syncedCount := 0
		for _, ticker := range tickers {
			if _, _, _, err := s.CompanyForTicker(parent, ticker); err == nil {
				syncedCount++
			}
		}

		desc := descriptions[entry.Name()]
		if desc == "" {
			desc = "Custom Universe"
		}

		name := strings.TrimSuffix(entry.Name(), ".txt")
		results = append(results, UniverseInfo{
			Name:        name,
			Path:        path,
			TotalCount:  len(tickers),
			SyncedCount: syncedCount,
			Description: desc,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalCount < results[j].TotalCount
	})

	tw := tabwriter.NewWriter(output, 0, 4, 3, ' ', 0)
	fmt.Fprintln(tw, "UNIVERSE\tTICKERS\tSYNCED IN DB\tDESCRIPTION")
	for _, u := range results {
		pct := 0.0
		if u.TotalCount > 0 {
			pct = float64(u.SyncedCount) / float64(u.TotalCount) * 100
		}
		fmt.Fprintf(tw, "%s\t%d\t%d (%.1f%%)\t%s\n", u.Name, u.TotalCount, u.SyncedCount, pct, u.Description)
	}
	tw.Flush()

	fmt.Fprintln(output, "\nSync any universe:")
	fmt.Fprintln(output, "  ./sec sync --universe russell2000 --skip-existing")
	fmt.Fprintln(output, "  ./sec sync --universe midcap400 --skip-existing")
	fmt.Fprintln(output, "  ./sec sync --universe adr_foreign --skip-existing")
	fmt.Fprintln(output, "  ./sec sync --universe broad_market_3000 --skip-existing")
	return nil
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
