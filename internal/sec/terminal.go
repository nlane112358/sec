package sec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	filingPageSize = 20
)

var secDocumentName = regexp.MustCompile(`^[A-Za-z0-9_.~%-]+$`)

func ReadFilings(ctx context.Context, databasePath, ticker string, input io.Reader, output io.Writer) error {
	store, err := OpenStore(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("load saved company data for %s: %w", ticker, err)
	}
	defer store.Close()
	company, savedTicker, err := store.LoadCompany(ctx)
	if err != nil {
		return fmt.Errorf("load saved company data for %s: %w", ticker, err)
	}
	if savedTicker != "" {
		ticker = savedTicker
	}
	filings, err := store.LoadFilings(ctx)
	if err != nil {
		return err
	}
	if len(filings) == 0 {
		return fmt.Errorf("no saved filings for %s; refresh with sec sync %s first", ticker, ticker)
	}

	reader := bufio.NewReader(input)
	filtered := filings
	filter := ""
	page := 0
	for {
		printFilingList(output, company, ticker, filtered, filter, page)
		if _, err := fmt.Fprint(output, "Choose a filing to open in your browser, n/p for filing pages, /text to search, or q to quit: "); err != nil {
			return err
		}
		command, err := readTerminalCommand(ctx, reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		command = strings.TrimSpace(command)
		switch {
		case command == "q" || command == "quit":
			return nil
		case command == "n":
			if (page+1)*filingPageSize < len(filtered) {
				page++
			}
		case command == "p":
			if page > 0 {
				page--
			}
		case strings.HasPrefix(command, "/"):
			filter = strings.TrimSpace(strings.TrimPrefix(command, "/"))
			filtered = searchFilings(filings, filter)
			page = 0
		case command == "":
			continue
		default:
			choice, err := strconv.Atoi(command)
			if err != nil || choice < 1 || choice > filingPageSize {
				fmt.Fprintln(output, "Enter a number shown on this page, n, p, /text, or q.")
				continue
			}
			index := page*filingPageSize + choice - 1
			if index >= len(filtered) {
				fmt.Fprintln(output, "That number is not on this page.")
				continue
			}
			if err := openSelectedFiling(output, filtered[index]); err != nil {
				fmt.Fprintf(output, "Could not open filing: %v\n", err)
			}
		}
	}
}

func printFilingList(output io.Writer, company, ticker string, filings []Filing, filter string, page int) {
	start := page * filingPageSize
	end := start + filingPageSize
	if end > len(filings) {
		end = len(filings)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "%s (%s) — %d filings", company, ticker, len(filings))
	if filter != "" {
		fmt.Fprintf(output, " matching %q", filter)
	}
	fmt.Fprintf(output, " · page %d of %d\n", page+1, maxInt(1, (len(filings)+filingPageSize-1)/filingPageSize))
	fmt.Fprintln(output, strings.Repeat("─", 96))
	if start >= len(filings) {
		fmt.Fprintln(output, "No filings match. Use /text to change the search or / to clear it.")
		return
	}
	for index, filing := range filings[start:end] {
		period := filing.ReportDate
		if period == "" {
			period = "—"
		}
		title := filing.DocumentTitle
		if title == "" {
			title = filing.PrimaryDocument
		}
		fmt.Fprintf(output, "%2d  %-7s filed %-10s period %-10s  %s\n", index+1, filing.Form, filing.FilingDate, period, truncateTerminalText(title, 48))
	}
	fmt.Fprintln(output, strings.Repeat("─", 96))
	fmt.Fprintln(output, "n = older filings · p = newer filings · / = clear search")
}

func searchFilings(filings []Filing, query string) []Filing {
	query = strings.ToLower(query)
	if query == "" {
		return filings
	}
	result := make([]Filing, 0)
	for _, filing := range filings {
		fields := strings.ToLower(strings.Join([]string{
			filing.Form, filing.FilingDate, filing.ReportDate,
			filing.DocumentTitle, filing.PrimaryDocument, filing.Accession,
		}, " "))
		if strings.Contains(fields, query) {
			result = append(result, filing)
		}
	}
	return result
}

func openSelectedFiling(output io.Writer, filing Filing) error {
	target, err := filingURL(filing)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "\nOpening %s %s in your browser…\n", filing.Form, filing.Accession); err != nil {
		return err
	}
	if err := openURLInBrowser(target); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Opened SEC filing %s.\n", filing.Accession)
	return err
}

func filingURL(filing Filing) (string, error) {
	if !validAccession(filing.Accession) {
		return "", fmt.Errorf("invalid filing accession number")
	}
	document, err := safeFilingDocumentPath(filing.PrimaryDocument)
	if err != nil {
		return "", err
	}
	cik, err := NormaliseCIK(filing.CIK)
	if err != nil {
		return "", err
	}
	secCIK := strings.TrimLeft(cik, "0")
	if secCIK == "" {
		secCIK = "0"
	}
	accessionFolder := strings.ReplaceAll(filing.Accession, "-", "")
	return "https://www.sec.gov/Archives/edgar/data/" + secCIK + "/" + accessionFolder + "/" + escapeTerminalPath(document), nil
}

func openURLInBrowser(target string) error {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "www.sec.gov" {
		return fmt.Errorf("invalid SEC filing URL")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open filing in browser: %w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}

func safeFilingDocumentPath(raw string) (string, error) {
	clean := path.Clean(strings.TrimSpace(raw))
	if clean == "." || clean != raw || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid filing document path")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." || !secDocumentName.MatchString(part) {
			return "", fmt.Errorf("invalid filing document path")
		}
	}
	return clean, nil
}

func escapeTerminalPath(document string) string {
	parts := strings.Split(document, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func validAccession(accession string) bool {
	if len(accession) != 20 || accession[10] != '-' || accession[13] != '-' {
		return false
	}
	for i, r := range accession {
		if i != 10 && i != 13 && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func truncateTerminalText(value string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func readTerminalCommand(ctx context.Context, reader *bufio.Reader) (string, error) {
	type lineResult struct {
		line string
		err  error
	}
	result := make(chan lineResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err == io.EOF && line == "" {
			result <- lineResult{err: io.EOF}
			return
		}
		if err != nil && err != io.EOF {
			result <- lineResult{err: err}
			return
		}
		result <- lineResult{line: strings.TrimRight(line, "\r\n")}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case completed := <-result:
		return completed.line, completed.err
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
