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
	"time"

	"sec/internal/store"
)

type universeDefinition struct {
	Alias       string
	Filename    string
	Description string
}

var canonicalUniverses = []universeDefinition{
	{Alias: "@sp500", Filename: "sp500.txt", Description: "S&P 500 Large-Cap US Benchmark"},
	{Alias: "@nasdaq100", Filename: "nasdaq100.txt", Description: "Nasdaq 100 Tech & Growth Leaders"},
	{Alias: "@midcap400", Filename: "midcap400.txt", Description: "S&P MidCap 400 Core Mid-Caps"},
	{Alias: "@russell2000", Filename: "russell2000.txt", Description: "Russell 2000 US Small-Cap Universe"},
	{Alias: "@russell3000", Filename: "russell3000.txt", Description: "Russell 3000 Comprehensive US Investable Universe"},
	{Alias: "@broad", Filename: "broad_market_3000.txt", Description: "Broad Market 3000+ (S&P 500, MidCap 400, Russell 2000, Foreign ADRs)"},
	{Alias: "@adr", Filename: "adr_foreign.txt", Description: "Global Foreign ADRs (Form 20-F / 40-F Cross-Border Filers)"},
	{Alias: "@euro-uk", Filename: "euro_uk_compounders_20f.txt", Description: "European & UK Compounders (Form 20-F Filers)"},
	{Alias: "@defense", Filename: "top10_defense_space.txt", Description: "Top 10 Defense & Space Compounders"},
	{Alias: "@space-defense", Filename: "space_defense_tech.txt", Description: "Space & Defense Technology Universe"},
	{Alias: "@all", Filename: "all_tickers.txt", Description: "Full SEC EDGAR Master Ticker Directory (~10,000+ filers)"},
}

var universeAliasShortcuts = map[string]string{
	"sp500":                     "sp500.txt",
	"sp5":                       "sp500.txt",
	"spx":                       "sp500.txt",
	"nasdaq100":                 "nasdaq100.txt",
	"ndx":                       "nasdaq100.txt",
	"qqq":                       "nasdaq100.txt",
	"midcap400":                 "midcap400.txt",
	"midcap":                    "midcap400.txt",
	"russell2000":               "russell2000.txt",
	"r2000":                     "russell2000.txt",
	"smallcap":                  "russell2000.txt",
	"russell3000":               "russell3000.txt",
	"r3000":                     "russell3000.txt",
	"broad":                     "broad_market_3000.txt",
	"broad-market":              "broad_market_3000.txt",
	"broad_market":              "broad_market_3000.txt",
	"broad_market_3000":         "broad_market_3000.txt",
	"broad-market-3000":         "broad_market_3000.txt",
	"adr":                       "adr_foreign.txt",
	"adr-foreign":               "adr_foreign.txt",
	"adr_foreign":               "adr_foreign.txt",
	"foreign":                   "adr_foreign.txt",
	"euro-uk":                   "euro_uk_compounders_20f.txt",
	"euro_uk":                   "euro_uk_compounders_20f.txt",
	"eu":                        "euro_uk_compounders_20f.txt",
	"euro-uk-compounders":       "euro_uk_compounders_20f.txt",
	"euro_uk_compounders_20f":   "euro_uk_compounders_20f.txt",
	"defense":                   "top10_defense_space.txt",
	"top10-defense":             "top10_defense_space.txt",
	"top10_defense_space":       "top10_defense_space.txt",
	"space-defense":             "space_defense_tech.txt",
	"space_defense":             "space_defense_tech.txt",
	"space_defense_tech":        "space_defense_tech.txt",
	"space":                     "space_defense_tech.txt",
	"all":                       "all_tickers.txt",
	"all-tickers":               "all_tickers.txt",
	"all_tickers":               "all_tickers.txt",
}

func universesDir() string {
	if envDir := strings.TrimSpace(os.Getenv("SEC_UNIVERSES_DIR")); envDir != "" {
		return envDir
	}
	for _, c := range []string{"universes", "../universes", "../../universes"} {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "universes"
}

func canonicalAliasForFilename(filename string) string {
	for _, u := range canonicalUniverses {
		if u.Filename == filename {
			return u.Alias
		}
	}
	base := strings.TrimSuffix(filename, ".txt")
	return "@" + strings.ReplaceAll(base, "_", "-")
}

func resolveUniverseSpec(spec string) (filePath, alias string, ok bool) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return "", "", false
	}
	isAt := strings.HasPrefix(trimmed, "@")
	clean := strings.TrimPrefix(trimmed, "@")
	lower := strings.ToLower(clean)
	uDir := universesDir()

	if targetFile, found := universeAliasShortcuts[lower]; found {
		candidate := filepath.Join(uDir, targetFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, canonicalAliasForFilename(targetFile), true
		}
	}

	if isAt {
		names := []string{
			clean,
			strings.ReplaceAll(clean, "-", "_"),
			strings.ReplaceAll(clean, "_", "-"),
			lower,
			strings.ReplaceAll(lower, "-", "_"),
			strings.ReplaceAll(lower, "_", "-"),
		}
		for _, n := range names {
			if n == "" {
				continue
			}
			fname := n
			if !strings.HasSuffix(fname, ".txt") {
				fname += ".txt"
			}
			candidate := filepath.Join(uDir, fname)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, canonicalAliasForFilename(filepath.Base(candidate)), true
			}
		}
		return "", "", false
	}

	// Non-@ path or bare universe name
	for _, candidate := range []string{
		trimmed,
		trimmed + ".txt",
		filepath.Join(uDir, trimmed),
		filepath.Join(uDir, trimmed+".txt"),
		filepath.Join(uDir, strings.ReplaceAll(lower, "-", "_")+".txt"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, canonicalAliasForFilename(filepath.Base(candidate)), true
		}
	}
	return "", "", false
}

// AllUniverseAliases returns sorted canonical @-prefixed universe aliases for completion and discovery.
func AllUniverseAliases() []string {
	rosters, err := loadUniverses()
	if err != nil {
		out := make([]string, len(canonicalUniverses))
		for i, u := range canonicalUniverses {
			out[i] = u.Alias
		}
		sort.Strings(out)
		return out
	}
	seen := make(map[string]struct{}, len(rosters))
	out := make([]string, 0, len(rosters))
	for _, r := range rosters {
		if _, ok := seen[r.Alias]; !ok {
			seen[r.Alias] = struct{}{}
			out = append(out, r.Alias)
		}
	}
	sort.Strings(out)
	return out
}

type UniverseInfo struct {
	Alias       string
	Name        string
	Filename    string
	TotalCount  int
	SyncedCount int
	Description string
}

const universeUsage = `usage: sec universe <subcommand> [options]

Subcommands:
  list                  List available @universe aliases and local sync coverage
  show @UNIVERSE        Display tickers and cache status for a universe
  create @NAME TICKERS  Create a custom @universe roster

Examples:
  sec universe list
  sec universe show @euro-uk
  sec universe create @compounders AAPL MSFT ASML COST
`

func RunUniverse(parent context.Context, args []string, output, errorOutput io.Writer) error {
	mode := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}
	if mode == "--help" || mode == "-h" || mode == "help" {
		_, err := io.WriteString(output, universeUsage)
		return err
	}
	switch mode {
	case "list", "status":
		return runUniverseList(parent, args, output, errorOutput)
	case "show", "inspect":
		return runUniverseShow(parent, args, output, errorOutput)
	case "create", "new":
		return runUniverseCreate(parent, args, output, errorOutput)
	default:
		if strings.HasPrefix(mode, "@") {
			return runUniverseShow(parent, append([]string{mode}, args...), output, errorOutput)
		}
		return fmt.Errorf("unknown universe subcommand %q; choose list, show, or create\n\n%s", mode, universeUsage)
	}
}

func runUniverseList(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("universe list", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec universe list [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("universe list does not take positional arguments")
	}

	universes, err := loadUniverses()
	if err != nil {
		return err
	}

	databasePath := SharedDatabasePath(*dataDir)
	if _, statErr := os.Stat(databasePath); statErr == nil {
		ctx, cancel, s, _, openErr := openStore(parent, *dataDir, 15*time.Second)
		if openErr != nil {
			return openErr
		}
		defer cancel()
		defer s.Close()

		hydratedSet := loadHydratedTickerSet(ctx, s)
		for i := range universes {
			for _, tk := range universes[i].tickers {
				if hydratedSet[strings.ToUpper(tk)] {
					universes[i].SyncedCount++
				}
			}
		}
	}

	tw := tabwriter.NewWriter(output, 0, 4, 3, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ALIAS\tNAME\tTICKERS\tSYNCED\tCOVERAGE\tDESCRIPTION"); err != nil {
		return err
	}
	for _, u := range universes {
		pct := 0.0
		if u.TotalCount > 0 {
			pct = float64(u.SyncedCount) / float64(u.TotalCount) * 100
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%.1f%%\t%s\n",
			u.Alias, u.Name, u.TotalCount, u.SyncedCount, pct, u.Description); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func runUniverseShow(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("universe show", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec universe show @UNIVERSE [--synced-only] [--missing-only] [--data-dir DIR]")
	syncedOnly := flags.Bool("synced-only", false, "show only tickers already cached locally")
	missingOnly := flags.Bool("missing-only", false, "show only tickers not yet cached locally")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	pos := flags.Args()
	if len(pos) != 1 {
		return errors.New("provide one universe alias or file (e.g. sec universe show @euro-uk)")
	}
	if *syncedOnly && *missingOnly {
		return errors.New("use at most one of --synced-only or --missing-only")
	}

	spec := pos[0]
	resolvedPath, alias, ok := resolveUniverseSpec(spec)
	if !ok {
		return fmt.Errorf("unknown universe %q (run \"sec universe list\" to see available @aliases)", spec)
	}
	tickers, err := readTickersFromFile(resolvedPath)
	if err != nil {
		return err
	}
	desc := universeDescription(filepath.Base(resolvedPath), resolvedPath)

	statusMap := make(map[string]store.CompanyCacheStatus)
	databasePath := SharedDatabasePath(*dataDir)
	if _, statErr := os.Stat(databasePath); statErr == nil {
		ctx, cancel, s, _, openErr := openStore(parent, *dataDir, 15*time.Second)
		if openErr != nil {
			return openErr
		}
		defer cancel()
		defer s.Close()
		statusMap, _ = s.CompanyHydrationStatuses(ctx, tickers)
	}

	syncedCount := 0
	for _, tk := range tickers {
		if statusMap[strings.ToUpper(tk)].HasFacts {
			syncedCount++
		}
	}

	if _, err := fmt.Fprintf(output, "%s (%s) — %d tickers (%d cached, %d pending)\n",
		alias, filepath.Base(resolvedPath), len(tickers), syncedCount, len(tickers)-syncedCount); err != nil {
		return err
	}
	if desc != "" {
		if _, err := fmt.Fprintf(output, "%s\n\n", desc); err != nil {
			return err
		}
	}

	tw := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TICKER\tSTATUS\tCOMPANY\tLATEST PERIOD\tLAST SYNCED"); err != nil {
		return err
	}
	for _, tk := range tickers {
		st := statusMap[strings.ToUpper(tk)]
		if *syncedOnly && !st.HasFacts {
			continue
		}
		if *missingOnly && st.HasFacts {
			continue
		}
		stateStr := "pending (JIT)"
		company := "—"
		period := "—"
		synced := "—"
		if st.HasFacts {
			stateStr = "cached"
			if st.CompanyName != "" {
				company = st.CompanyName
			}
			if st.LatestPeriodEnd != "" {
				period = st.LatestPeriodEnd
			}
			if !st.UpdatedAt.IsZero() {
				synced = FormatRelativeAge(st.UpdatedAt)
			}
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", tk, stateStr, company, period, synced); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func runUniverseCreate(_ context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("universe create", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec universe create @NAME [TICKER...] [--tickers A,B,C] [--file PATH] [--description TEXT] [--force]")
	tickersFlag := flags.String("tickers", "", "comma-separated list of tickers")
	fromFile := flags.String("file", "", "read initial tickers from an existing file")
	description := flags.String("description", "", "optional description comment for the universe")
	force := flags.Bool("force", false, "overwrite an existing universe file")
	if err := ParseCommandFlags(flags, args, "tickers", "file", "description"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	pos := flags.Args()
	if len(pos) < 1 {
		return errors.New("provide a universe name, e.g. sec universe create @my-compounders AAPL MSFT ASML")
	}

	rawName := strings.TrimSpace(pos[0])
	cleanName := strings.TrimPrefix(rawName, "@")
	cleanName = strings.TrimSuffix(cleanName, ".txt")
	cleanName = strings.ToLower(strings.TrimSpace(cleanName))
	if cleanName == "" {
		return errors.New("universe name cannot be empty")
	}
	for _, r := range cleanName {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("invalid universe name %q (use letters, digits, '-' or '_')", rawName)
		}
	}
	fileStem := strings.ReplaceAll(cleanName, "-", "_")
	alias := "@" + strings.ReplaceAll(cleanName, "_", "-")

	var rawTickers []string
	rawTickers = append(rawTickers, pos[1:]...)
	if strings.TrimSpace(*tickersFlag) != "" {
		for _, part := range strings.Split(*tickersFlag, ",") {
			if p := strings.TrimSpace(part); p != "" {
				rawTickers = append(rawTickers, p)
			}
		}
	}
	if strings.TrimSpace(*fromFile) != "" {
		loaded, err := readTickersFromFile(*fromFile)
		if err != nil {
			return fmt.Errorf("read --file %s: %w", *fromFile, err)
		}
		rawTickers = append(rawTickers, loaded...)
	}
	if len(rawTickers) == 0 {
		return errors.New("provide at least one ticker via arguments, --tickers, or --file")
	}

	seen := make(map[string]struct{}, len(rawTickers))
	tickers := make([]string, 0, len(rawTickers))
	for _, raw := range rawTickers {
		tk, err := CommandTicker([]string{raw})
		if err != nil {
			return err
		}
		if _, ok := seen[tk]; ok {
			continue
		}
		seen[tk] = struct{}{}
		tickers = append(tickers, tk)
	}

	uDir := universesDir()
	if err := os.MkdirAll(uDir, 0755); err != nil {
		return fmt.Errorf("create universes directory: %w", err)
	}
	targetPath := filepath.Join(uDir, fileStem+".txt")
	if !*force {
		if _, err := os.Stat(targetPath); err == nil {
			return fmt.Errorf("universe %s already exists at %s (pass --force to overwrite)", alias, targetPath)
		}
	}

	var sb strings.Builder
	if strings.TrimSpace(*description) != "" {
		sb.WriteString("# ")
		sb.WriteString(strings.TrimSpace(*description))
		sb.WriteByte('\n')
	}
	for _, tk := range tickers {
		sb.WriteString(tk)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(targetPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write universe file: %w", err)
	}

	_, err := fmt.Fprintf(output, "Created universe %s (%d tickers) -> %s\nUse it anywhere with: sec screen %s ...\n",
		alias, len(tickers), targetPath, alias)
	return err
}

func loadHydratedTickerSet(ctx context.Context, s *store.Store) map[string]bool {
	set := make(map[string]bool)
	rows, err := s.DB().QueryContext(ctx, `
SELECT DISTINCT t.ticker
FROM sec_tickers t
WHERE EXISTS (SELECT 1 FROM sec_facts f WHERE f.cik = t.cik LIMIT 1)`)
	if err != nil {
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var tk string
		if err := rows.Scan(&tk); err == nil {
			set[strings.ToUpper(strings.TrimSpace(tk))] = true
		}
	}
	return set
}

type universeRoster struct {
	UniverseInfo
	tickers []string
}

func universeDescription(filename, fullPath string) string {
	for _, u := range canonicalUniverses {
		if u.Filename == filename {
			return u.Description
		}
	}
	if file, err := os.Open(fullPath); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		if scanner.Scan() {
			first := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(first, "#") {
				return strings.TrimSpace(strings.TrimPrefix(first, "#"))
			}
		}
	}
	return "Custom Universe"
}

func loadUniverses() ([]universeRoster, error) {
	uDir := universesDir()
	entries, err := os.ReadDir(uDir)
	if err != nil {
		return nil, fmt.Errorf("read universes directory: %w", err)
	}

	var results []universeRoster
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		path := filepath.Join(uDir, entry.Name())
		tickers, err := readTickersFromFile(path)
		if err != nil || len(tickers) == 0 {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".txt")
		results = append(results, universeRoster{
			UniverseInfo: UniverseInfo{
				Alias:       canonicalAliasForFilename(entry.Name()),
				Name:        name,
				Filename:    entry.Name(),
				TotalCount:  len(tickers),
				Description: universeDescription(entry.Name(), path),
			},
			tickers: tickers,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Alias < results[j].Alias
	})
	return results, nil
}

func readTickersFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var tickers []string
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, ",\t") {
			fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' })
			if len(fields) > 0 {
				line = strings.TrimSpace(fields[0])
				line = strings.Trim(line, "\"'")
			}
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if !strings.EqualFold(f, "ticker") && !strings.EqualFold(f, "symbol") {
				u := strings.ToUpper(f)
				if _, ok := seen[u]; !ok {
					seen[u] = struct{}{}
					tickers = append(tickers, u)
				}
			}
		}
	}
	return tickers, scanner.Err()
}
