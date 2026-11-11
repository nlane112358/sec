package fundamentals

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"sec/internal/store"
)

type AuditMetricSummary struct {
	Name      string
	Available int
	Missing   int
	Coverage  float64
}

type CompanyAuditReport struct {
	Ticker         string
	Company        string
	YearsAvailable int
	MissingMetrics []string
	IsViableDCF    bool
	IsViableROIC   bool
}

type AuditResults struct {
	TotalCompanies int
	MetricCoverage []AuditMetricSummary
	CompanyReports []CompanyAuditReport
}

// AuditDatabase scans all saved companies in the database for metric completeness
// over their latest fiscal year.
func AuditDatabase(ctx context.Context, s *store.Store) (AuditResults, error) {
	companies, err := s.Companies(ctx)
	if err != nil {
		return AuditResults{}, err
	}

	res := AuditResults{
		TotalCompanies: len(companies),
	}
	if len(companies) == 0 {
		return res, nil
	}

	criticalMetrics := []string{
		"Revenue",
		"Operating Income",
		"Net Income",
		"Operating Cash Flow",
		"Capital Expenditure",
		"Free Cash Flow",
		"Cash & Equivalents",
		"Stockholders' Equity",
		"Debt / Borrowings",
		"Shares Outstanding",
	}

	counts := make(map[string]int)

	for _, c := range companies {
		summary, err := LoadFinancialSummary(ctx, s, 3, c.Ticker)
		report := CompanyAuditReport{
			Ticker:  c.Ticker,
			Company: c.Name,
		}

		if err != nil || len(summary.Periods) == 0 {
			report.MissingMetrics = append(report.MissingMetrics, "NO_ANNUAL_DATA")
			res.CompanyReports = append(res.CompanyReports, report)
			continue
		}

		report.YearsAvailable = len(summary.Periods)

		hasRev := false
		hasCapEx := false
		hasOCF := false
		hasEquity := false
		hasShares := false

		for _, metricName := range criticalMetrics {
			val := summaryMetricValue(summary.Metrics, metricName, 0)
			if val.Present {
				counts[metricName]++
				switch metricName {
				case "Revenue":
					hasRev = true
				case "Capital Expenditure":
					hasCapEx = true
				case "Operating Cash Flow":
					hasOCF = true
				case "Stockholders' Equity":
					hasEquity = true
				case "Shares Outstanding":
					hasShares = true
				}
			} else {
				report.MissingMetrics = append(report.MissingMetrics, metricName)
			}
		}

		report.IsViableDCF = hasRev && hasOCF && hasCapEx && hasShares
		report.IsViableROIC = hasRev && hasEquity

		res.CompanyReports = append(res.CompanyReports, report)
	}

	for _, m := range criticalMetrics {
		avail := counts[m]
		cov := 0.0
		if res.TotalCompanies > 0 {
			cov = float64(avail) * 100.0 / float64(res.TotalCompanies)
		}
		res.MetricCoverage = append(res.MetricCoverage, AuditMetricSummary{
			Name:      m,
			Available: avail,
			Missing:   res.TotalCompanies - avail,
			Coverage:  cov,
		})
	}

	return res, nil
}

// DiagnoseTicker inspects raw facts in sec_facts to help you find the exact alternative tags
// that a company is using for a missing line item.
func DiagnoseTicker(ctx context.Context, s *store.Store, ticker string, output io.Writer) error {
	cik, name, _, err := s.CompanyForTicker(ctx, ticker)
	if err != nil {
		return err
	}

	summary, err := LoadFinancialSummary(ctx, s, 1, ticker)
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "=== Diagnostic Audit for %s (%s) ===\n", name, ticker)
	if len(summary.Periods) == 0 {
		fmt.Fprintln(output, "No annual period ends found.")
		return nil
	}
	latestEnd := summary.Periods[0].End
	fmt.Fprintf(output, "Latest Annual Period End: %s\n\n", latestEnd)

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "METRIC\tSTATUS\tRESOLVED VALUE")

	var missing []string
	for _, m := range summary.Metrics {
		val := m.Values[0]
		status := "OK"
		valStr := formatSummaryValue(val.Value, val.Unit)
		if !val.Present {
			status = "MISSING"
			valStr = "—"
			missing = append(missing, m.Name)
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\n", m.Name, status, valStr)
	}
	writer.Flush()

	if len(missing) > 0 {
		fmt.Fprintf(output, "\n--- Raw Tag Discovery for %s (Period: %s) ---\n", ticker, latestEnd)
		fmt.Fprintln(output, "Querying sec_facts for candidate concepts filed on or near this period:")

		rows, err := s.DB().QueryContext(ctx, `
			SELECT DISTINCT taxonomy, concept, unit, value, form, filing_date
			FROM sec_facts
			WHERE cik = ? AND (period_end = ? OR (abs(julianday(period_end) - julianday(?)) <= 105 AND (form LIKE '10-K%' OR form LIKE '20-F%' OR form LIKE '40-F%')))
			ORDER BY taxonomy, concept`, cik, latestEnd, latestEnd)
		if err != nil {
			return err
		}
		defer rows.Close()

		tagWriter := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tagWriter, "TAXONOMY\tCONCEPT\tFORM\tVALUE\tUNIT")
		count := 0
		for rows.Next() {
			var tax, concept, unit, form, fDate string
			var val float64
			if err := rows.Scan(&tax, &concept, &unit, &val, &form, &fDate); err == nil {
				fmt.Fprintf(tagWriter, "%s\t%s\t%s\t%s\t%s\n", tax, concept, form, formatSummaryValue(val, unit), unit)
				count++
			}
		}
		tagWriter.Flush()
		fmt.Fprintf(output, "\nTotal available facts on %s: %d\n", latestEnd, count)
	}

	return nil
}

func WriteAuditSummary(output io.Writer, res AuditResults, onlyMissing bool) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintf(output, "Database Audit: %d Total Companies Synced\n\n", res.TotalCompanies)

	fmt.Fprintln(output, "--- Metric Coverage (Latest Annual Period) ---")
	fmt.Fprintln(writer, "METRIC\tAVAILABLE\tMISSING\tCOVERAGE")
	for _, m := range res.MetricCoverage {
		fmt.Fprintf(writer, "%s\t%d\t%d\t%.1f%%\n", m.Name, m.Available, m.Missing, m.Coverage)
	}
	writer.Flush()

	fmt.Fprintln(output, "\n--- Company Gap Analysis ---")
	compWriter := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(compWriter, "TICKER\tCOMPANY\tYEARS\tDCF READY\tROIC READY\tMISSING METRICS")

	gapCount := 0
	for _, cr := range res.CompanyReports {
		if onlyMissing && len(cr.MissingMetrics) == 0 {
			continue
		}
		gapCount++
		missingStr := "None (Complete)"
		if len(cr.MissingMetrics) > 0 {
			missingStr = strings.Join(cr.MissingMetrics, ", ")
		}
		dcfReady := "Yes"
		if !cr.IsViableDCF {
			dcfReady = "NO"
		}
		roicReady := "Yes"
		if !cr.IsViableROIC {
			roicReady = "NO"
		}

		fmt.Fprintf(compWriter, "%s\t%s\t%d\t%s\t%s\t%s\n",
			cr.Ticker, cr.Company, cr.YearsAvailable, dcfReady, roicReady, missingStr)
	}
	compWriter.Flush()

	if onlyMissing && gapCount == 0 {
		fmt.Fprintln(output, "All companies have 100% metric coverage!")
	}
	return nil
}
