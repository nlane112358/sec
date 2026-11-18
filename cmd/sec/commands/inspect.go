package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"sec/internal/edgar"
	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/store"
)

var inspectDocNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.~%-]+$`)

const inspectUsage = `usage: sec inspect TICKER [view] [options]
       sec inspect audit [--gaps-only] [--data-dir DIR]

Views:
  summary (default)     Multi-year annual financial metrics (--years 3-5, --usd)
  quarterly             Discrete quarterly metrics from 10-Q (--quarters 1-16)
  ttm                   Trailing-Twelve-Month run-rate & quarterly earnings inflections
  diff                  Redline diff of Item 1A (Risk Factors) or Item 7 (MD&A) between 10-K/20-F filings
  audit                 Diagnostic XBRL tag & coverage audit (or full DB audit without ticker)
  filings               Browse or open SEC EDGAR filings (--form, --limit, --list)

Options:
  --refresh             Force fresh EDGAR, quote, and FX pulls
  --offline             Use local SQLite cache only (no network requests)
  --data-dir DIR        Directory containing the shared SQLite database

Examples:
  sec inspect ASML
  sec inspect AAPL quarterly --quarters 8
  sec inspect AAPL ttm
  sec inspect AAPL diff --item 1A
  sec inspect AAPL audit
  sec inspect AAPL filings --form 10-K --list
`

func isInspectView(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "summary", "annual", "quarterly", "q", "ttm", "diff", "audit", "filings", "read":
		return true
	default:
		return false
	}
}

func RunInspect(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(output, inspectUsage)
		return err
	}

	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, inspectUsage)
	years := flags.Int("years", 5, "fiscal years to show (3-5)")
	quarterly := flags.Bool("quarterly", false, "display quarterly financial metrics (10-Q)")
	qShort := flags.Bool("q", false, "display quarterly financial metrics (short alias)")
	quarters := flags.Int("quarters", 8, "number of quarters to show (1-16)")
	ttm := flags.Bool("ttm", false, "display Trailing-Twelve-Month (TTM) data and quarterly inflections")
	viewFlag := flags.String("view", "", "view mode: annual, quarterly, ttm, diff, audit, or filings")
	usdFlag := flags.Bool("usd", false, "convert non-USD monetary metrics to USD using live/cached FX")
	itemFlag := flags.String("item", "1A", "diff section: 1A (Risk Factors) or 7 (MD&A)")
	currentRef := flags.String("current", "", "diff current filing accession or year")
	priorRef := flags.String("prior", "", "diff prior filing accession or year")
	showAll := flags.Bool("all", false, "show unchanged paragraphs in diff output")
	jsonOut := flags.Bool("json", false, "output diff as JSON")
	gapsOnly := flags.Bool("gaps-only", false, "for full DB audit, list only companies with missing metrics")
	formFilter := flags.String("form", "", "filter filings by form type (e.g. 10-K, 10-Q, 8-K)")
	limitFlag := flags.Int("limit", 25, "maximum number of filings to display in list mode")
	listOnly := flags.Bool("list", false, "print filings list without interactive prompt")
	openIdx := flags.Int("open", 0, "open the N-th matching filing in your browser")
	refresh := flags.Bool("refresh", false, "force fresh EDGAR, quote, and FX pulls")
	offline := flags.Bool("offline", false, "use local SQLite cache only")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	pos := flags.Args()
	var ticker, view string
	switch len(pos) {
	case 0:
		return errors.New("provide a ticker symbol (e.g. sec inspect AAPL) or \"sec inspect audit\"")
	case 1:
		if strings.EqualFold(pos[0], "audit") {
			view = "audit"
		} else {
			var err error
			ticker, err = CommandTicker([]string{pos[0]})
			if err != nil {
				return err
			}
		}
	case 2:
		if isInspectView(pos[1]) {
			var err error
			ticker, err = CommandTicker([]string{pos[0]})
			if err != nil {
				return err
			}
			view = strings.ToLower(strings.TrimSpace(pos[1]))
		} else if isInspectView(pos[0]) {
			view = strings.ToLower(strings.TrimSpace(pos[0]))
			var err error
			ticker, err = CommandTicker([]string{pos[1]})
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unknown inspect view %q (choose summary, quarterly, ttm, diff, audit, or filings)", pos[1])
		}
	default:
		return errors.New("inspect accepts TICKER and an optional sub-view (quarterly, ttm, diff, audit, filings)")
	}

	if *viewFlag != "" {
		view = strings.ToLower(strings.TrimSpace(*viewFlag))
	}
	if view == "" {
		switch {
		case *ttm:
			view = "ttm"
		case *quarterly || *qShort:
			view = "quarterly"
		default:
			view = "summary"
		}
	}
	if view == "q" {
		view = "quarterly"
	} else if view == "annual" {
		view = "summary"
	} else if view == "read" {
		view = "filings"
	}

	if *years < 3 || *years > 5 {
		return errors.New("--years must be between 3 and 5")
	}
	if *quarters < 1 || *quarters > 16 {
		return errors.New("--quarters must be between 1 and 16")
	}

	ctx, cancel, s, dbPath, err := openStore(parent, *dataDir, 2*time.Minute)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	// Database-wide audit when no ticker is provided.
	if view == "audit" && ticker == "" {
		results, err := fundamentals.AuditDatabase(ctx, s)
		if err != nil {
			return err
		}
		return fundamentals.WriteAuditSummary(output, results, *gapsOnly)
	}

	meta, err := EnsureCompanyHydrated(ctx, s, ticker, HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline,
		UserAgent:   *userAgent,
		NeedQuote:   view == "summary" || view == "quarterly" || view == "ttm",
		NeedFX:      true,
		ErrorOutput: errorOutput,
	})
	if err != nil {
		return err
	}

	switch view {
	case "summary":
		summary, err := fundamentals.LoadFinancialSummary(ctx, s, *years, ticker)
		if err != nil {
			return err
		}
		if *usdFlag {
			summary = convertSummaryToUSD(summary)
		}
		WriteFreshnessHeader(output, meta)
		return fundamentals.WriteFinancialSummary(output, summary)

	case "quarterly":
		if *ttm {
			return writeInspectTTM(ctx, s, ticker, meta, *usdFlag, output)
		}
		summary, err := fundamentals.LoadQuarterlySummary(ctx, s, *quarters, ticker)
		if err != nil {
			return err
		}
		if *usdFlag {
			summary = convertSummaryToUSD(summary)
		}
		WriteFreshnessHeader(output, meta)
		return fundamentals.WriteFinancialSummary(output, summary)

	case "ttm":
		return writeInspectTTM(ctx, s, ticker, meta, *usdFlag, output)

	case "diff":
		return runInspectDiff(ctx, s, *dataDir, ticker, meta, *itemFlag, *currentRef, *priorRef, *showAll, *jsonOut, *offline, *userAgent, output, errorOutput)

	case "audit":
		WriteFreshnessHeader(output, meta)
		return fundamentals.DiagnoseTicker(ctx, s, ticker, output)

	case "filings":
		return runInspectFilings(ctx, s, dbPath, ticker, meta, *formFilter, *limitFlag, *listOnly, *openIdx, output)

	default:
		return fmt.Errorf("unknown inspect view %q (choose summary, quarterly, ttm, diff, audit, or filings)", view)
	}
}

func writeInspectTTM(ctx context.Context, s *store.Store, ticker string, meta HydrationMetadata, toUSD bool, output io.Writer) error {
	summary, err := fundamentals.LoadTTMFinancialSummary(ctx, s, ticker)
	if err != nil {
		return err
	}
	if toUSD {
		summary = convertSummaryToUSD(summary)
	}
	WriteFreshnessHeader(output, meta)
	if err := fundamentals.WriteFinancialSummary(output, summary); err != nil {
		return err
	}
	ttmDetails, err := fundamentals.LoadTTMSummary(ctx, s, ticker)
	if err == nil {
		_ = fundamentals.WriteTTMInflections(output, ttmDetails.Inflections)
	}
	return nil
}

func convertSummaryToUSD(summary fundamentals.FinancialSummary) fundamentals.FinancialSummary {
	out := fundamentals.FinancialSummary{
		Company: summary.Company,
		Ticker:  summary.Ticker,
		Periods: append([]fundamentals.SummaryPeriod(nil), summary.Periods...),
		Metrics: make([]fundamentals.SummaryMetric, len(summary.Metrics)),
	}
	for i, m := range summary.Metrics {
		vals := make([]fundamentals.SummaryValue, len(m.Values))
		for j, v := range m.Values {
			vals[j] = v
			if v.Present && v.Unit != "" && !strings.EqualFold(v.Unit, "USD") && !strings.EqualFold(v.Unit, "shares") && v.Unit != "%" {
				vals[j].Value = market.ToUSD(v.Value, v.Unit)
				vals[j].Unit = "USD"
			}
		}
		out.Metrics[i] = fundamentals.SummaryMetric{Name: m.Name, Values: vals}
	}
	return out
}

func runInspectDiff(
	ctx context.Context,
	s *store.Store,
	dataDir, ticker string,
	meta HydrationMetadata,
	itemRaw, currentRef, priorRef string,
	showAll, jsonOut, offline bool,
	userAgent string,
	output, errorOutput io.Writer,
) error {
	itemType := strings.ToUpper(strings.TrimSpace(itemRaw))
	if itemType != "7" {
		itemType = "1A"
	}
	itemTitle := "Item 1A. Risk Factors"
	if itemType == "7" {
		itemTitle = "Item 7. Management's Discussion and Analysis"
	}

	filings, err := s.LoadFilings(ctx, ticker)
	if err != nil {
		return err
	}
	currentFiling, priorFiling, available, err := fundamentals.SelectDiffFilings(filings, currentRef, priorRef)
	if err != nil {
		return err
	}

	filingCacheDir := filepath.Join(dataDir, "filings")
	_ = os.MkdirAll(filingCacheDir, 0755)
	client := newEDGARClient(userAgent)

	spinner := NewSpinner(errorOutput)
	var loadErr error
	currRaw, err := loadOrFetchFilingHTML(ctx, client, filingCacheDir, currentFiling, offline, spinner)
	if err != nil {
		loadErr = fmt.Errorf("load current filing HTML (%s): %w", currentFiling.Accession, err)
		spinner.Finish(loadErr)
		return loadErr
	}
	priorRaw, err := loadOrFetchFilingHTML(ctx, client, filingCacheDir, priorFiling, offline, spinner)
	if err != nil {
		loadErr = fmt.Errorf("load prior filing HTML (%s): %w", priorFiling.Accession, err)
		spinner.Finish(loadErr)
		return loadErr
	}
	spinner.Finish(nil)

	currParagraphs := fundamentals.ExtractItemFromHTML(string(currRaw), itemType)
	priorParagraphs := fundamentals.ExtractItemFromHTML(string(priorRaw), itemType)
	diffItems, stats := fundamentals.ComputeDiff(priorParagraphs, currParagraphs)

	res := fundamentals.FilingDiffResult{
		Ticker:              ticker,
		Item:                itemType,
		ItemTitle:           itemTitle,
		CurrentFiling:       filingVersionFromStore(currentFiling),
		PriorFiling:         filingVersionFromStore(priorFiling),
		Available10KFilings: available,
		Stats:               stats,
		DiffItems:           diffItems,
	}

	if jsonOut {
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	WriteFreshnessHeader(output, meta)
	return fundamentals.WriteFilingDiff(output, res, showAll, isTerminalWriter(output))
}

func filingVersionFromStore(f store.Filing) fundamentals.FilingVersion {
	year := ""
	if len(f.FilingDate) >= 4 {
		year = f.FilingDate[:4]
	}
	return fundamentals.FilingVersion{
		Accession:       f.Accession,
		FilingDate:      f.FilingDate,
		ReportDate:      f.ReportDate,
		Form:            f.Form,
		PrimaryDocument: f.PrimaryDocument,
		Year:            year,
	}
}

func loadOrFetchFilingHTML(
	ctx context.Context,
	client *edgar.Client,
	cacheDir string,
	filing store.Filing,
	offline bool,
	spinner *Spinner,
) ([]byte, error) {
	numericCIK, err := strconv.ParseUint(strings.TrimSpace(filing.CIK), 10, 64)
	if err != nil || numericCIK == 0 {
		return nil, fmt.Errorf("invalid CIK %q", filing.CIK)
	}
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(filing.Accession), "-", "")
	if len(accessionDigits) != 18 {
		return nil, fmt.Errorf("invalid accession %q", filing.Accession)
	}
	primaryDoc := path.Clean(strings.TrimSpace(filing.PrimaryDocument))
	if primaryDoc == "." || primaryDoc == "" || strings.HasPrefix(primaryDoc, "..") || !inspectDocNamePattern.MatchString(primaryDoc) {
		return nil, fmt.Errorf("invalid primary document %q", filing.PrimaryDocument)
	}

	cacheKey := fmt.Sprintf("%s_%s_%s", filing.CIK, accessionDigits, primaryDoc)
	cacheFile := filepath.Join(cacheDir, cacheKey)
	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		return data, nil
	}
	if offline {
		return nil, fmt.Errorf("filing %s not in local cache (offline mode enabled)", filing.Accession)
	}

	if spinner != nil {
		spinner.Step(fmt.Sprintf("Fetching %s HTML (EDGAR)...", filing.Form))
	}
	secURL := client.ArchiveDocumentURL(numericCIK, accessionDigits, url.PathEscape(primaryDoc))
	data, err := client.FetchDocument(ctx, secURL)
	if err != nil {
		return nil, err
	}
	_ = os.WriteFile(cacheFile, data, 0644)
	return data, nil
}

func runInspectFilings(
	ctx context.Context,
	s *store.Store,
	dbPath, ticker string,
	meta HydrationMetadata,
	formFilter string,
	limit int,
	listOnly bool,
	openIdx int,
	output io.Writer,
) error {
	filings, err := s.LoadFilings(ctx, ticker)
	if err != nil {
		return err
	}
	if formFilter != "" {
		var filtered []store.Filing
		target := strings.ToUpper(strings.TrimSpace(formFilter))
		for _, f := range filings {
			if strings.EqualFold(f.Form, target) || strings.HasPrefix(strings.ToUpper(f.Form), target) {
				filtered = append(filtered, f)
			}
		}
		filings = filtered
	}
	if len(filings) == 0 {
		return fmt.Errorf("no saved filings matching criteria for %s", ticker)
	}

	if openIdx > 0 {
		if openIdx > len(filings) {
			return fmt.Errorf("--open %d out of range (%d filings available)", openIdx, len(filings))
		}
		link := filingDirectURL(filings[openIdx-1])
		if link == "" {
			return fmt.Errorf("unable to build URL for filing %s", filings[openIdx-1].Accession)
		}
		_, _ = fmt.Fprintf(output, "Opening %s (%s) in browser: %s\n", filings[openIdx-1].Form, filings[openIdx-1].FilingDate, link)
		return openSystemBrowser(link)
	}

	if !listOnly && formFilter == "" && isTerminalReader(os.Stdin) && isTerminalWriter(output) {
		WriteFreshnessHeader(output, meta)
		return edgar.ReadFilings(ctx, dbPath, ticker, os.Stdin, output)
	}

	WriteFreshnessHeader(output, meta)
	if limit > 0 && limit < len(filings) {
		filings = filings[:limit]
	}
	_, _ = fmt.Fprintf(output, "%s (%s) — %d filing(s)\n\n", meta.CompanyName, ticker, len(filings))
	tw := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "#\tFORM\tFILING DATE\tREPORT DATE\tTITLE\tACCESSION")
	for i, f := range filings {
		title := f.DocumentTitle
		if title == "" {
			title = f.PrimaryDocument
		}
		if len(title) > 48 {
			title = strings.TrimSpace(title[:47]) + "…"
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, f.Form, f.FilingDate, f.ReportDate, title, f.Accession)
	}
	return tw.Flush()
}

func filingDirectURL(f store.Filing) string {
	numericCIK, err := strconv.ParseUint(strings.TrimSpace(f.CIK), 10, 64)
	if err != nil || numericCIK == 0 {
		return ""
	}
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(f.Accession), "-", "")
	if len(accessionDigits) != 18 {
		return ""
	}
	primaryDoc := path.Clean(strings.TrimSpace(f.PrimaryDocument))
	if primaryDoc == "." || primaryDoc == "" || strings.HasPrefix(primaryDoc, "..") || !inspectDocNamePattern.MatchString(primaryDoc) {
		return ""
	}
	return fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s", numericCIK, accessionDigits, url.PathEscape(primaryDoc))
}

func openSystemBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
