package fundamentals

import (
	"bytes"
	"strings"
	"testing"

	"sec/internal/store"
)

func TestExtractItemFromHTML(t *testing.T) {
	sample10K := `
	<html>
	<head><style>p { color: red; }</style></head>
	<body>
	<div>Table of Contents</div>
	<div><a href="#item1a">Item 1A. Risk Factors</a></div>
	<p>General intro text here.</p>
	<h2>Item 1A. Risk Factors</h2>
	<p>The Company faces intense competition in global markets and rapid technological change.</p>
	<p>Global supply chain disruptions, geopolitical events, and export controls could adversely impact operations.</p>
	<p>New regulations regarding artificial intelligence and antitrust could increase compliance costs.</p>
	<h2>Item 1B. Unresolved Staff Comments</h2>
	<p>None.</p>
	</body>
	</html>
	`

	paragraphs := ExtractItemFromHTML(sample10K, "1A")
	if len(paragraphs) < 3 {
		t.Fatalf("expected at least 3 paragraphs, got %d: %v", len(paragraphs), paragraphs)
	}

	foundAI := false
	for _, p := range paragraphs {
		if stringsContains(p, "artificial intelligence") {
			foundAI = true
		}
	}
	if !foundAI {
		t.Errorf("expected to find AI paragraph in extracted section, got %v", paragraphs)
	}
}

func TestComputeDiff(t *testing.T) {
	prior := []string{
		"The Company faces intense competition in global markets.",
		"Global supply chain disruptions and COVID-19 lockdowns could impact operations.",
		"Interest rate fluctuations may affect short-term investment portfolio yields.",
	}

	current := []string{
		"The Company faces intense competition in global markets.",
		"Global supply chain disruptions, geopolitical tariffs and AI restrictions could impact operations.",
		"Extreme weather events caused by climate change may harm physical facilities.",
	}

	diffItems, stats := ComputeDiff(prior, current)
	if stats.UnchangedParagraphs != 1 {
		t.Errorf("expected 1 unchanged paragraph, got %d", stats.UnchangedParagraphs)
	}
	if stats.ModifiedParagraphs != 1 {
		t.Errorf("expected 1 modified paragraph, got %d", stats.ModifiedParagraphs)
	}
	if stats.AddedParagraphs != 1 {
		t.Errorf("expected 1 added paragraph, got %d", stats.AddedParagraphs)
	}
	if stats.DeletedParagraphs != 1 {
		t.Errorf("expected 1 deleted paragraph, got %d", stats.DeletedParagraphs)
	}

	// Verify word diff exists on modified item
	var modItem *DiffParagraph
	for i := range diffItems {
		if diffItems[i].Type == "modified" {
			modItem = &diffItems[i]
			break
		}
	}
	if modItem == nil || len(modItem.Words) == 0 {
		t.Fatalf("expected word diff on modified paragraph, got nil or empty")
	}

	hasInsert := false
	hasDelete := false
	for _, w := range modItem.Words {
		if w.Type == "insert" {
			hasInsert = true
		}
		if w.Type == "delete" {
			hasDelete = true
		}
	}
	if !hasInsert || !hasDelete {
		t.Errorf("expected both inserted and deleted words, got hasInsert=%v, hasDelete=%v", hasInsert, hasDelete)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && len(s) > 0 && findSub(s, sub)))
}

func findSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSelectDiffFilingsAndWriteFilingDiff(t *testing.T) {
	filings := []store.Filing{
		{CIK: "0000320193", Accession: "0000320193-26-000010", Form: "10-Q", FilingDate: "2026-08-01"},
		{CIK: "0000320193", Accession: "0000320193-25-000099", Form: "10-K", FilingDate: "2025-11-01", ReportDate: "2025-09-27", PrimaryDocument: "aapl-2025.htm"},
		{CIK: "0000320193", Accession: "0000320193-24-000088", Form: "10-K", FilingDate: "2024-11-01", ReportDate: "2024-09-28", PrimaryDocument: "aapl-2024.htm"},
	}
	curr, prior, avail, err := SelectDiffFilings(filings, "", "")
	if err != nil {
		t.Fatalf("SelectDiffFilings failed: %v", err)
	}
	if curr.Accession != "0000320193-25-000099" || prior.Accession != "0000320193-24-000088" || len(avail) != 2 {
		t.Fatalf("unexpected filing selection: curr=%+v prior=%+v avail=%d", curr, prior, len(avail))
	}

	items, stats := ComputeDiff(
		[]string{"The Company faces intense competition in global markets."},
		[]string{"The Company faces intense AI competition in global markets."},
	)
	var out bytes.Buffer
	err = WriteFilingDiff(&out, FilingDiffResult{
		Ticker:        "AAPL",
		Item:          "1A",
		ItemTitle:     "Item 1A. Risk Factors",
		CurrentFiling: avail[0],
		PriorFiling:   avail[1],
		Stats:         stats,
		DiffItems:     items,
	}, false, false)
	if err != nil {
		t.Fatalf("WriteFilingDiff failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Item 1A. Risk Factors Diff") || !strings.Contains(text, "{+AI+}") {
		t.Fatalf("unexpected diff output:\n%s", text)
	}
}
