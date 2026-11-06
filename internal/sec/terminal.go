package sec

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	filingPageSize   = 20
	documentPageSize = 24
	maxDocumentSize  = 32 << 20
)

var terminalDocName = regexp.MustCompile(`^[A-Za-z0-9_.~%-]+$`)

func ReadFilings(ctx context.Context, databasePath, ticker, userAgent string, input io.Reader, output io.Writer) error {
	if strings.TrimSpace(userAgent) == "" {
		return fmt.Errorf("provide -ua or set SEC_USER_AGENT to read SEC filings")
	}
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
		return fmt.Errorf("no saved filings for %s; refresh with sec -ticker %s first", ticker, ticker)
	}

	reader := bufio.NewReader(input)
	client := &http.Client{Timeout: 35 * time.Second}
	documentCacheDir := filepath.Join(filepath.Dir(databasePath), "documents")
	filtered := filings
	filter := ""
	page := 0
	for {
		printFilingList(output, company, ticker, filtered, filter, page)
		if _, err := fmt.Fprint(output, "Choose a number, n/p for filing pages, /text to search, or q to quit: "); err != nil {
			return err
		}
		command, err := readTerminalCommand(reader)
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
			backToList, err := readTerminalFiling(ctx, reader, output, client, userAgent, documentCacheDir, ticker, filtered[index])
			if err != nil {
				fmt.Fprintf(output, "Could not read filing: %v\n", err)
				continue
			}
			if !backToList {
				return nil
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

func readTerminalFiling(ctx context.Context, reader *bufio.Reader, output io.Writer, client *http.Client, userAgent, documentCacheDir, ticker string, filing Filing) (bool, error) {
	fmt.Fprintf(output, "\nOpening %s %s...\n", filing.Form, filing.Accession)
	body, err := fetchPrimaryDocument(ctx, client, userAgent, documentCacheDir, filing)
	if err != nil {
		return true, err
	}
	text := htmlToTerminalText(body)
	if strings.TrimSpace(text) == "" {
		return true, fmt.Errorf("the filing document contains no readable text")
	}
	width, pageLines := terminalSize()
	lines := wrapTerminalText(text, width)
	if len(lines) == 0 {
		return true, fmt.Errorf("the filing document contains no readable text")
	}
	page := 0
	for {
		pages := maxInt(1, (len(lines)+pageLines-1)/pageLines)
		if page >= pages {
			page = pages - 1
		}
		start := page * pageLines
		end := minInt(start+pageLines, len(lines))
		title := filing.DocumentTitle
		if title == "" {
			title = filing.PrimaryDocument
		}
		fmt.Fprintln(output)
		fmt.Fprintf(output, "%s · %s · Filed %s · Page %d/%d\n", ticker, filing.Form, filing.FilingDate, page+1, pages)
		fmt.Fprintln(output, truncateTerminalText(title, width))
		fmt.Fprintln(output, strings.Repeat("─", width))
		for _, line := range lines[start:end] {
			fmt.Fprintln(output, line)
		}
		fmt.Fprintln(output, strings.Repeat("─", width))
		fmt.Fprint(output, "n/p next/previous · g N jump · /text find · b filings · q quit: ")
		command, err := readTerminalCommand(reader)
		if err != nil {
			if err == io.EOF {
				return false, nil
			}
			return false, err
		}
		command = strings.TrimSpace(command)
		switch {
		case command == "q" || command == "quit":
			return false, nil
		case command == "b" || command == "back":
			return true, nil
		case command == "n":
			if page+1 < pages {
				page++
			}
		case command == "p":
			if page > 0 {
				page--
			}
		case strings.HasPrefix(command, "g "):
			requested, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(command, "g ")))
			if err == nil && requested >= 1 && requested <= pages {
				page = requested - 1
			} else {
				fmt.Fprintf(output, "Enter a page number from 1 to %d.\n", pages)
			}
		case strings.HasPrefix(command, "/"):
			needle := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command, "/")))
			if needle == "" {
				fmt.Fprintln(output, "Enter / followed by text to find it in this filing.")
				continue
			}
			line := findTerminalText(lines, needle, minInt(end, len(lines)))
			if line < 0 {
				line = findTerminalText(lines, needle, 0)
			}
			if line < 0 {
				fmt.Fprintf(output, "No match for %q.\n", needle)
			} else {
				page = line / pageLines
			}
		case command == "":
			continue
		default:
			fmt.Fprintln(output, "Use n, p, g N, /text, b, or q.")
		}
	}
}

func fetchPrimaryDocument(ctx context.Context, client *http.Client, userAgent, documentCacheDir string, filing Filing) ([]byte, error) {
	if !validAccession(filing.Accession) {
		return nil, fmt.Errorf("invalid filing accession number")
	}
	document, err := safeTerminalDocumentPath(filing.PrimaryDocument)
	if err != nil {
		return nil, err
	}
	cik, err := NormaliseCIK(filing.CIK)
	if err != nil {
		return nil, err
	}
	cachePath := filepath.Join(documentCacheDir, filing.Accession+".html")
	if body, err := os.ReadFile(cachePath); err == nil && len(body) > 0 && len(body) <= maxDocumentSize {
		return body, nil
	}
	accessionFolder := strings.ReplaceAll(filing.Accession, "-", "")
	remoteURL := "https://www.sec.gov/Archives/edgar/data/" + strings.TrimLeft(cik, "0") + "/" + accessionFolder + "/" + escapeTerminalPath(document)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request SEC filing: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("SEC returned HTTP %d for this document", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentSize+1))
	if err != nil {
		return nil, fmt.Errorf("read SEC filing: %w", err)
	}
	if len(body) > maxDocumentSize {
		return nil, fmt.Errorf("filing document is too large to display in the terminal")
	}
	_ = writeDocumentCache(cachePath, body)
	return body, nil
}

func writeDocumentCache(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".filing-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func safeTerminalDocumentPath(raw string) (string, error) {
	clean := path.Clean(strings.TrimSpace(raw))
	if clean == "." || clean != raw || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid filing document path")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." || !terminalDocName.MatchString(part) {
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

func htmlToTerminalText(body []byte) string {
	input := strings.ToValidUTF8(string(body), "�")
	var result strings.Builder
	for i := 0; i < len(input); {
		if input[i] != '<' {
			next := strings.IndexByte(input[i:], '<')
			if next < 0 {
				next = len(input)
			} else {
				next += i
			}
			result.WriteString(html.UnescapeString(input[i:next]))
			i = next
			continue
		}
		if strings.HasPrefix(input[i:], "<!--") {
			if end := strings.Index(input[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
		}
		end := terminalTagEnd(input, i+1)
		if end < 0 {
			result.WriteByte('<')
			i++
			continue
		}
		name, closing := terminalTagName(input[i+1 : end])
		tag := strings.ToLower(input[i+1 : end])
		if !closing && terminalSkipTag(name, tag) {
			if strings.HasSuffix(strings.TrimSpace(input[i+1:end]), "/") || terminalVoidTag(name) {
				i = end + 1
			} else {
				i = skipTerminalElement(input, end+1, name)
			}
			continue
		}
		if terminalBlockTag(name) {
			result.WriteByte('\n')
		} else if name == "td" || name == "th" {
			result.WriteString("\t")
		}
		i = end + 1
	}
	return normaliseTerminalText(result.String())
}

func terminalTagEnd(input string, start int) int {
	var quote byte
	for i := start; i < len(input); i++ {
		c := input[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == '>' {
			return i
		}
	}
	return -1
}

func terminalTagName(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	closing := strings.HasPrefix(raw, "/")
	if closing {
		raw = strings.TrimSpace(raw[1:])
	}
	end := 0
	for end < len(raw) {
		c := raw[end]
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c != ':' && c != '-' {
			break
		}
		end++
	}
	return strings.ToLower(raw[:end]), closing
}

func terminalSkipTag(name, tag string) bool {
	return name == "head" || name == "script" || name == "style" || name == "noscript" || name == "svg" || name == "ix:hidden" || name == "ix:header" || name == "template" || strings.Contains(tag, "display:none") || strings.Contains(tag, "display: none") || strings.Contains(tag, "visibility:hidden") || strings.Contains(tag, "visibility: hidden")
}

func skipTerminalElement(input string, start int, name string) int {
	depth := 1
	for i := start; i < len(input); {
		rel := strings.IndexByte(input[i:], '<')
		if rel < 0 {
			return len(input)
		}
		i += rel
		if strings.HasPrefix(input[i:], "<!--") {
			if end := strings.Index(input[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
		}
		end := terminalTagEnd(input, i+1)
		if end < 0 {
			return len(input)
		}
		tagName, closing := terminalTagName(input[i+1 : end])
		if tagName == name {
			if closing {
				depth--
				if depth == 0 {
					return end + 1
				}
			} else if !strings.HasSuffix(strings.TrimSpace(input[i+1:end]), "/") {
				depth++
			}
		}
		i = end + 1
	}
	return len(input)
}

func terminalBlockTag(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "br", "caption", "dd", "details", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "ol", "p", "pre", "section", "summary", "table", "tbody", "tfoot", "thead", "tr", "ul":
		return true
	default:
		return false
	}
}

func terminalVoidTag(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func normaliseTerminalText(text string) string {
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\u200b", "")
	var lines []string
	blank := false
	for _, line := range strings.Split(text, "\n") {
		cells := strings.Split(line, "\t")
		for i, cell := range cells {
			cells[i] = strings.Join(strings.Fields(cell), " ")
		}
		line = strings.TrimSpace(strings.Join(cells, "  |  "))
		if line == "" {
			if len(lines) > 0 && !blank {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		lines = append(lines, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func wrapTerminalText(text string, width int) []string {
	if width < 40 {
		width = 40
	}
	var wrapped []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		words := strings.Fields(line)
		var current strings.Builder
		for _, word := range words {
			wordRunes := []rune(word)
			for len(wordRunes) > width {
				if current.Len() > 0 {
					wrapped = append(wrapped, current.String())
					current.Reset()
				}
				wrapped = append(wrapped, string(wordRunes[:width]))
				wordRunes = wordRunes[width:]
			}
			word = string(wordRunes)
			if current.Len() == 0 {
				current.WriteString(word)
			} else if utf8.RuneCountInString(current.String())+1+utf8.RuneCountInString(word) <= width {
				current.WriteByte(' ')
				current.WriteString(word)
			} else {
				wrapped = append(wrapped, current.String())
				current.Reset()
				current.WriteString(word)
			}
		}
		if current.Len() > 0 {
			wrapped = append(wrapped, current.String())
		}
	}
	return wrapped
}

func findTerminalText(lines []string, needle string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.Contains(strings.ToLower(lines[i]), needle) {
			return i
		}
	}
	return -1
}

func terminalSize() (int, int) {
	width, height := 100, documentPageSize+8
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n >= 40 && n <= 300 {
		width = n
	}
	if n, err := strconv.Atoi(os.Getenv("LINES")); err == nil && n >= 16 && n <= 200 {
		height = n
	}
	return width, maxInt(8, height-8)
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

func readTerminalCommand(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", io.EOF
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
