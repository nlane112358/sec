package edgar

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

	"sec/internal/store"
)

const (
	filingPageSize = 20
)

var secDocumentName = regexp.MustCompile(`^[A-Za-z0-9_.~%-]+$`)

func ReadFilings(ctx context.Context, databasePath, ticker string, input io.Reader, output io.Writer) error {
	s, err := store.Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("load saved company data for %s: %w", ticker, err)
	}
	defer s.Close()
	company, savedTicker, err := s.LoadCompany(ctx, ticker)
	if err != nil {
		return fmt.Errorf("load saved company data for %s: %w", ticker, err)
	}
	if savedTicker != "" {
		ticker = savedTicker
	}
	filings, err := s.LoadFilings(ctx, ticker)
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

func printFilingList(output io.Writer, company, ticker string, filings []store.Filing, filter string, page int) {
	start := page * filingPageSize
	end := start + filingPageSize
	if end > len(filings) {
		end = len(filings)
	}
	fmt.Fprintf(output, "\n%s (%s) filings", company, ticker)
	if filter != "" {
		fmt.Fprintf(output, " matching %q", filter)
	}
	fmt.Fprintf(output, " [page %d of %d]\n", page+1, (len(filings)+filingPageSize-1)/filingPageSize)
	for i := start; i < end; i++ {
		filing := filings[i]
		number := i - start + 1
		title := filing.DocumentTitle
		if title == "" {
			title = filing.PrimaryDocument
		}
		fmt.Fprintf(output, "  [%d] %s (%s) — %s\n", number, filing.Form, filing.FilingDate, title)
	}
}

func searchFilings(filings []store.Filing, filter string) []store.Filing {
	if filter == "" {
		return filings
	}
	target := strings.ToLower(filter)
	matches := make([]store.Filing, 0)
	for _, filing := range filings {
		if strings.Contains(strings.ToLower(filing.Form), target) ||
			strings.Contains(strings.ToLower(filing.FilingDate), target) ||
			strings.Contains(strings.ToLower(filing.ReportDate), target) ||
			strings.Contains(strings.ToLower(filing.PrimaryDocument), target) ||
			strings.Contains(strings.ToLower(filing.DocumentTitle), target) {
			matches = append(matches, filing)
		}
	}
	return matches
}

func openSelectedFiling(output io.Writer, filing store.Filing) error {
	link, err := filingURL(filing)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Opening %s in your browser (%s)\n", filing.Form, link)
	return openBrowser(link)
}

func filingURL(filing store.Filing) (string, error) {
	numericCIK, err := strconv.ParseUint(strings.TrimSpace(filing.CIK), 10, 64)
	if err != nil || numericCIK == 0 {
		return "", fmt.Errorf("invalid CIK %q", filing.CIK)
	}
	accessionDigits := strings.ReplaceAll(strings.TrimSpace(filing.Accession), "-", "")
	if len(accessionDigits) != 18 {
		return "", fmt.Errorf("invalid accession number %q", filing.Accession)
	}
	primaryDocument := path.Clean(strings.TrimSpace(filing.PrimaryDocument))
	if primaryDocument == "." || primaryDocument == "" || strings.HasPrefix(primaryDocument, "..") || !secDocumentName.MatchString(primaryDocument) {
		return "", fmt.Errorf("invalid filing document %q", filing.PrimaryDocument)
	}
	return fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s", numericCIK, accessionDigits, url.PathEscape(primaryDocument)), nil
}

func openBrowser(target string) error {
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

func readTerminalCommand(ctx context.Context, reader *bufio.Reader) (string, error) {
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.line, res.err
	}
}
