package fundamentals

import (
	"fmt"
	"html"
	"io"
	"math"
	"regexp"
	"strings"

	"sec/internal/store"
)

type DiffWord struct {
	Type string `json:"type"` // "equal", "insert", "delete"
	Text string `json:"text"`
}

type DiffParagraph struct {
	Type        string     `json:"type"` // "equal", "added", "deleted", "modified"
	Text        string     `json:"text"`
	PriorText   string     `json:"prior_text,omitempty"`
	CurrentText string     `json:"current_text,omitempty"`
	Words       []DiffWord `json:"words,omitempty"`
	Heading     string     `json:"heading,omitempty"`
}

type DiffStats struct {
	TotalCurrent        int     `json:"total_current"`
	TotalPrior          int     `json:"total_prior"`
	AddedParagraphs     int     `json:"added_paragraphs"`
	DeletedParagraphs   int     `json:"deleted_paragraphs"`
	ModifiedParagraphs  int     `json:"modified_paragraphs"`
	UnchangedParagraphs int     `json:"unchanged_paragraphs"`
	SimilarityPct       float64 `json:"similarity_pct"`
}

type FilingVersion struct {
	Accession       string `json:"accession"`
	FilingDate      string `json:"filing_date"`
	ReportDate      string `json:"report_date"`
	Form            string `json:"form"`
	PrimaryDocument string `json:"primary_document"`
	Year            string `json:"year,omitempty"`
}

type FilingDiffResult struct {
	Ticker              string          `json:"ticker"`
	Item                string          `json:"item"`
	ItemTitle           string          `json:"item_title"`
	CurrentFiling       FilingVersion   `json:"current_filing"`
	PriorFiling         FilingVersion   `json:"prior_filing"`
	Available10KFilings []FilingVersion `json:"available_10k_filings"`
	Stats               DiffStats       `json:"stats"`
	DiffItems           []DiffParagraph `json:"diff_items"`
}

var (
	htmlScriptStyleRegex = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlTagRegex         = regexp.MustCompile(`<[^>]+>`)
	runningHeaderRegex   = regexp.MustCompile(`(?i)(Apple\s+Inc\.|Form\s+10-K\s*\|\s*\d+|Table\s+of\s+Contents|Index\s+to\s+Financial\s+Statements)`)
	whitespaceRegex      = regexp.MustCompile(`\s+`)
)

// ExtractItemFromHTML parses an SEC 10-K HTML document and extracts the text paragraphs
// for Item 1A (Risk Factors) or Item 7 (MD&A).
func ExtractItemFromHTML(rawHTML string, item string) []string {
	clean := htmlScriptStyleRegex.ReplaceAllString(rawHTML, " ")

	// Replace block-level tags with newlines
	clean = regexp.MustCompile(`(?i)</?(p|div|tr|h[1-6]|li|br)[^>]*>`).ReplaceAllString(clean, "\n")
	clean = htmlTagRegex.ReplaceAllString(clean, " ")
	clean = html.UnescapeString(clean)

	rawLines := strings.Split(clean, "\n")
	var lines []string
	for _, l := range rawLines {
		trimmed := strings.TrimSpace(whitespaceRegex.ReplaceAllString(l, " "))
		if trimmed == "" {
			continue
		}
		if runningHeaderRegex.MatchString(trimmed) && len(trimmed) < 60 {
			continue
		}
		lines = append(lines, trimmed)
	}

	startIdx := -1
	endIdx := -1
	itemNorm := strings.ToUpper(strings.TrimSpace(item))

	if itemNorm == "1A" {
		for i, l := range lines {
			if isItem1AHeading(l) {
				startIdx = i
			}
		}
		if startIdx != -1 {
			for j := startIdx + 1; j < len(lines); j++ {
				if isItem1AEnd(lines[j]) {
					endIdx = j
					break
				}
			}
		}
	} else if itemNorm == "7" {
		for i, l := range lines {
			if isItem7Heading(l) {
				startIdx = i
			}
		}
		if startIdx != -1 {
			for j := startIdx + 1; j < len(lines); j++ {
				if isItem7End(lines[j]) {
					endIdx = j
					break
				}
			}
		}
	}

	if startIdx != -1 {
		if endIdx == -1 || endIdx <= startIdx {
			endIdx = int(math.Min(float64(len(lines)), float64(startIdx+1500)))
		}
		return consolidateParagraphs(lines[startIdx:endIdx])
	}

	return nil
}

func isItem1AHeading(s string) bool {
	if len(s) > 100 {
		return false
	}
	sLow := strings.ToLower(s)
	return strings.Contains(sLow, "item 1a") && strings.Contains(sLow, "risk factors")
}

func isItem1AEnd(s string) bool {
	if len(s) > 100 {
		return false
	}
	sLow := strings.ToLower(s)
	matched, _ := regexp.MatchString(`^item\s+(1b|1c|2)[\.\:\s]`, sLow)
	return matched
}

func isItem7Heading(s string) bool {
	if len(s) > 120 {
		return false
	}
	sLow := strings.ToLower(s)
	return strings.Contains(sLow, "item 7") && (strings.Contains(sLow, "management") || strings.Contains(sLow, "discussion"))
}

func isItem7End(s string) bool {
	if len(s) > 100 {
		return false
	}
	sLow := strings.ToLower(s)
	matched, _ := regexp.MatchString(`^item\s+(7a|8)[\.\:\s]`, sLow)
	return matched
}

func consolidateParagraphs(lines []string) []string {
	var paragraphs []string
	var current strings.Builder

	for _, l := range lines {
		lTrim := strings.TrimSpace(l)
		if len(lTrim) == 0 {
			continue
		}
		// If current line looks like a bold header (short, capitalized) or bullet, flush previous
		isHeading := len(lTrim) < 140 && (strings.HasSuffix(lTrim, ".") || strings.HasSuffix(lTrim, ":") || strings.ToUpper(lTrim) == lTrim)
		if isHeading && current.Len() > 0 {
			paragraphs = append(paragraphs, strings.TrimSpace(current.String()))
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(lTrim)

		// If line ends with period or semicolon and is long enough, it's likely paragraph boundary
		if len(lTrim) > 80 && (strings.HasSuffix(lTrim, ".") || strings.HasSuffix(lTrim, ":")) {
			paragraphs = append(paragraphs, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}

	if current.Len() > 0 {
		paragraphs = append(paragraphs, strings.TrimSpace(current.String()))
	}

	var filtered []string
	for _, p := range paragraphs {
		pTrim := strings.TrimSpace(p)
		if len(pTrim) > 15 {
			filtered = append(filtered, pTrim)
		}
	}
	return filtered
}

// ComputeDiff runs an LCS-based diff comparison between prior and current paragraphs.
func ComputeDiff(priorParagraphs, currentParagraphs []string) ([]DiffParagraph, DiffStats) {
	n := len(priorParagraphs)
	m := len(currentParagraphs)

	// Compute LCS table on paragraphs
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}

	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			if priorParagraphs[i] == currentParagraphs[j] {
				lcs[i+1][j+1] = lcs[i][j] + 1
			} else {
				lcs[i+1][j+1] = int(math.Max(float64(lcs[i+1][j]), float64(lcs[i][j+1])))
			}
		}
	}

	// Backtrack to extract matches
	type pair struct {
		i, j int
	}
	var matchedPairs []pair
	i, j := n, m
	for i > 0 && j > 0 {
		if priorParagraphs[i-1] == currentParagraphs[j-1] {
			matchedPairs = append([]pair{{i - 1, j - 1}}, matchedPairs...)
			i--
			j--
		} else if lcs[i-1][j] >= lcs[i][j-1] {
			i--
		} else {
			j--
		}
	}

	var items []DiffParagraph
	priorIdx, currIdx := 0, 0

	var addedCount, deletedCount, modifiedCount, unchangedCount int

	for _, match := range matchedPairs {
		// Collect unmatched prior paragraphs before this match (deleted or modified candidates)
		unmatchedPrior := priorParagraphs[priorIdx:match.i]
		// Collect unmatched current paragraphs before this match (added or modified candidates)
		unmatchedCurr := currentParagraphs[currIdx:match.j]

		diffBlock := pairUnmatched(unmatchedPrior, unmatchedCurr)
		for _, item := range diffBlock {
			switch item.Type {
			case "added":
				addedCount++
			case "deleted":
				deletedCount++
			case "modified":
				modifiedCount++
			}
			items = append(items, item)
		}

		// Matched equal paragraph
		items = append(items, DiffParagraph{
			Type: "equal",
			Text: currentParagraphs[match.j],
		})
		unchangedCount++

		priorIdx = match.i + 1
		currIdx = match.j + 1
	}

	// Tail
	diffTail := pairUnmatched(priorParagraphs[priorIdx:], currentParagraphs[currIdx:])
	for _, item := range diffTail {
		switch item.Type {
		case "added":
			addedCount++
		case "deleted":
			deletedCount++
		case "modified":
			modifiedCount++
		}
		items = append(items, item)
	}

	total := float64(n + m)
	similarity := 0.0
	if total > 0 {
		similarity = (2.0 * float64(unchangedCount+modifiedCount)) / total * 100.0
		if similarity > 100.0 {
			similarity = 100.0
		}
	}

	stats := DiffStats{
		TotalCurrent:        m,
		TotalPrior:          n,
		AddedParagraphs:     addedCount,
		DeletedParagraphs:   deletedCount,
		ModifiedParagraphs:  modifiedCount,
		UnchangedParagraphs: unchangedCount,
		SimilarityPct:       math.Round(similarity*10) / 10,
	}

	return items, stats
}

func pairUnmatched(priors, currents []string) []DiffParagraph {
	var result []DiffParagraph
	usedCurr := make([]bool, len(currents))

	for _, p := range priors {
		bestMatch := -1
		bestScore := 0.0

		for cIdx, c := range currents {
			if usedCurr[cIdx] {
				continue
			}
			score := paragraphSimilarity(p, c)
			if score > 0.45 && score > bestScore {
				bestScore = score
				bestMatch = cIdx
			}
		}

		if bestMatch != -1 {
			usedCurr[bestMatch] = true
			c := currents[bestMatch]
			words := computeWordDiff(p, c)
			result = append(result, DiffParagraph{
				Type:        "modified",
				Text:        c,
				PriorText:   p,
				CurrentText: c,
				Words:       words,
			})
		} else {
			result = append(result, DiffParagraph{
				Type: "deleted",
				Text: p,
			})
		}
	}

	for cIdx, c := range currents {
		if !usedCurr[cIdx] {
			result = append(result, DiffParagraph{
				Type: "added",
				Text: c,
			})
		}
	}

	return result
}

func paragraphSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]int)
	for _, w := range wordsA {
		setA[w]++
	}

	intersect := 0
	for _, w := range wordsB {
		if count, ok := setA[w]; ok && count > 0 {
			intersect++
			setA[w]--
		}
	}

	return (2.0 * float64(intersect)) / float64(len(wordsA)+len(wordsB))
}

func computeWordDiff(prior, current string) []DiffWord {
	wordsA := strings.Fields(prior)
	wordsB := strings.Fields(current)

	n := len(wordsA)
	m := len(wordsB)

	// Word LCS
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			if strings.EqualFold(wordsA[i], wordsB[j]) {
				dp[i+1][j+1] = dp[i][j] + 1
			} else {
				dp[i+1][j+1] = int(math.Max(float64(dp[i+1][j]), float64(dp[i][j+1])))
			}
		}
	}

	var words []DiffWord
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && strings.EqualFold(wordsA[i-1], wordsB[j-1]) {
			words = append([]DiffWord{{Type: "equal", Text: wordsB[j-1]}}, words...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			words = append([]DiffWord{{Type: "insert", Text: wordsB[j-1]}}, words...)
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			words = append([]DiffWord{{Type: "delete", Text: wordsA[i-1]}}, words...)
			i--
		}
	}

	return words
}

// SelectDiffFilings chooses the current and prior annual filings (10-K/20-F/40-F) to diff.
// currentRef and priorRef may be accession numbers or 4-digit years (e.g. "2025").
func SelectDiffFilings(filings []store.Filing, currentRef, priorRef string) (store.Filing, store.Filing, []FilingVersion, error) {
	var annuals []store.Filing
	for _, f := range filings {
		form := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(f.Form), "/A"))
		if form == "10-K" || form == "20-F" || form == "40-F" {
			annuals = append(annuals, f)
		}
	}
	if len(annuals) < 2 {
		return store.Filing{}, store.Filing{}, nil, fmt.Errorf("at least two annual filings (10-K/20-F/40-F) are required to compute a diff (found %d)", len(annuals))
	}

	matchFiling := func(ref string) (store.Filing, bool) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return store.Filing{}, false
		}
		for _, f := range annuals {
			if strings.EqualFold(f.Accession, ref) {
				return f, true
			}
		}
		for _, f := range annuals {
			if strings.HasPrefix(f.FilingDate, ref) || strings.HasPrefix(f.ReportDate, ref) {
				return f, true
			}
		}
		return store.Filing{}, false
	}

	var currentFiling, priorFiling store.Filing
	if currentRef != "" {
		f, ok := matchFiling(currentRef)
		if !ok {
			return store.Filing{}, store.Filing{}, nil, fmt.Errorf("annual filing matching %q not found", currentRef)
		}
		currentFiling = f
	} else {
		currentFiling = annuals[0]
	}

	if priorRef != "" {
		f, ok := matchFiling(priorRef)
		if !ok {
			return store.Filing{}, store.Filing{}, nil, fmt.Errorf("prior annual filing matching %q not found", priorRef)
		}
		priorFiling = f
	} else {
		for idx, f := range annuals {
			if f.Accession == currentFiling.Accession && idx+1 < len(annuals) {
				priorFiling = annuals[idx+1]
				break
			}
		}
		if priorFiling.Accession == "" {
			priorFiling = annuals[1]
		}
	}

	available := make([]FilingVersion, len(annuals))
	for i, f := range annuals {
		year := ""
		if len(f.FilingDate) >= 4 {
			year = f.FilingDate[:4]
		}
		available[i] = FilingVersion{
			Accession:       f.Accession,
			FilingDate:      f.FilingDate,
			ReportDate:      f.ReportDate,
			Form:            f.Form,
			PrimaryDocument: f.PrimaryDocument,
			Year:            year,
		}
	}
	return currentFiling, priorFiling, available, nil
}

// WriteFilingDiff renders a FilingDiffResult for terminal inspection.
func WriteFilingDiff(output io.Writer, res FilingDiffResult, showAll, color bool) error {
	if _, err := fmt.Fprintf(output, "%s — %s Diff\n", res.Ticker, res.ItemTitle); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Current: %s (%s, acc %s)\n", res.CurrentFiling.Form, res.CurrentFiling.FilingDate, res.CurrentFiling.Accession); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Prior:   %s (%s, acc %s)\n", res.PriorFiling.Form, res.PriorFiling.FilingDate, res.PriorFiling.Accession); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Stats:   Similarity %.1f%% · +%d added · -%d deleted · ~%d modified · %d unchanged\n\n",
		res.Stats.SimilarityPct, res.Stats.AddedParagraphs, res.Stats.DeletedParagraphs, res.Stats.ModifiedParagraphs, res.Stats.UnchangedParagraphs); err != nil {
		return err
	}

	if res.Stats.TotalCurrent == 0 && res.Stats.TotalPrior == 0 {
		_, err := fmt.Fprintf(output, "Could not extract Item %s paragraphs from the selected filings.\n", res.Item)
		return err
	}

	changes := res.Stats.AddedParagraphs + res.Stats.DeletedParagraphs + res.Stats.ModifiedParagraphs
	if changes == 0 && !showAll {
		_, err := fmt.Fprintln(output, "No material text changes detected in this section.")
		return err
	}

	green, red, yellow, dim, reset := "", "", "", "", ""
	if color {
		green = "\033[32m"
		red = "\033[31m"
		yellow = "\033[33m"
		dim = "\033[2m"
		reset = "\033[0m"
	}

	for _, item := range res.DiffItems {
		switch item.Type {
		case "added":
			if _, err := fmt.Fprintf(output, "%s+ %s%s\n\n", green, item.Text, reset); err != nil {
				return err
			}
		case "deleted":
			if _, err := fmt.Fprintf(output, "%s- %s%s\n\n", red, item.Text, reset); err != nil {
				return err
			}
		case "modified":
			var b strings.Builder
			b.WriteString(yellow + "~ " + reset)
			if len(item.Words) > 0 {
				for idx, w := range item.Words {
					if idx > 0 {
						b.WriteByte(' ')
					}
					switch w.Type {
					case "insert":
						b.WriteString(green + "{+" + w.Text + "+}" + reset)
					case "delete":
						b.WriteString(red + "[-" + w.Text + "-]" + reset)
					default:
						b.WriteString(w.Text)
					}
				}
			} else {
				b.WriteString(item.Text)
			}
			if _, err := fmt.Fprintln(output, b.String()); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		case "equal":
			if showAll {
				if _, err := fmt.Fprintf(output, "%s  %s%s\n\n", dim, item.Text, reset); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
