package market

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// LoadValuationInputs reads ticker valuations from CSV, TSV, or JSON. CSV/TSV
// files can use TICKER,PRICE or TICKER,MARKET_CAP headers; a legacy
// TICKER,VALUE column is classified by magnitude (values below one million are
// treated as per-share prices).
func LoadValuationInputs(path string) (marketCaps map[string]float64, prices map[string]float64, err error) {
	marketCaps = make(map[string]float64)
	prices = make(map[string]float64)
	if strings.TrimSpace(path) == "" {
		return marketCaps, prices, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read valuation inputs: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	if strings.HasPrefix(string(trimmed), "{") || strings.HasPrefix(string(trimmed), "[") || strings.EqualFold(strings.TrimSpace(path[strings.LastIndex(path, ".")+1:]), "json") {
		if err := loadJSONValuationInputs(trimmed, marketCaps, prices); err != nil {
			return nil, nil, fmt.Errorf("parse JSON valuation inputs: %w", err)
		}
		return marketCaps, prices, nil
	}
	if err := loadDelimitedValuationInputs(data, marketCaps, prices); err != nil {
		return nil, nil, fmt.Errorf("parse valuation inputs: %w", err)
	}
	return marketCaps, prices, nil
}

func loadDelimitedValuationInputs(data []byte, marketCaps, prices map[string]float64) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var cleaned bytes.Buffer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if cleaned.Len() == 0 {
		return nil
	}
	firstLine, _, _ := bytes.Cut(cleaned.Bytes(), []byte("\n"))
	delimiter := ','
	if !bytes.Contains(firstLine, []byte(",")) {
		if bytes.Contains(firstLine, []byte("\t")) {
			delimiter = '\t'
		} else if bytes.Contains(firstLine, []byte(";")) {
			delimiter = ';'
		}
	}
	reader := csv.NewReader(bytes.NewReader(cleaned.Bytes()))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	columns := map[string]int{}
	for index, heading := range records[0] {
		columns[normalizeValuationHeading(heading)] = index
	}
	tickerColumn, tickerHeader := findColumn(columns, "ticker", "symbol")
	priceColumn, hasPriceColumn := findColumn(columns, "price", "shareprice", "stockprice")
	marketCapColumn, hasMarketCapColumn := findColumn(columns, "marketcap", "marketcapitalization", "marketvalue")
	valueColumn, hasValueColumn := findColumn(columns, "value")
	header := tickerHeader && (hasPriceColumn || hasMarketCapColumn || hasValueColumn)
	if !header {
		tickerColumn, priceColumn, marketCapColumn, valueColumn = 0, 1, -1, -1
		hasPriceColumn, hasMarketCapColumn, hasValueColumn = false, false, false
	}
	start := 0
	if header {
		start = 1
	}
	for i := start; i < len(records); i++ {
		row := records[i]
		if tickerColumn >= len(row) {
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(row[tickerColumn]))
		if ticker == "" || strings.HasPrefix(ticker, "#") {
			continue
		}
		if hasPriceColumn && priceColumn < len(row) {
			if value, err := parseValuationNumber(row[priceColumn]); err == nil && value > 0 {
				prices[ticker] = value
			}
		}
		if hasMarketCapColumn && marketCapColumn < len(row) {
			if value, err := parseValuationNumber(row[marketCapColumn]); err == nil && value > 0 {
				marketCaps[ticker] = value
			}
		}
		if !hasPriceColumn && !hasMarketCapColumn && hasValueColumn && valueColumn < len(row) {
			if value, err := parseValuationNumber(row[valueColumn]); err == nil && value > 0 {
				classifyValuation(ticker, value, marketCaps, prices)
			}
		}
		if !header && len(row) > 1 {
			if value, err := parseValuationNumber(row[1]); err == nil && value > 0 {
				classifyValuation(ticker, value, marketCaps, prices)
			}
		}
	}
	return nil
}

func loadJSONValuationInputs(data []byte, marketCaps, prices map[string]float64) error {
	var asArray []map[string]any
	if err := json.Unmarshal(data, &asArray); err == nil {
		for _, entry := range asArray {
			ticker := jsonString(entry, "ticker", "symbol")
			if ticker == "" {
				continue
			}
			if price := jsonFloat(entry, "price", "share_price", "sharePrice"); price > 0 {
				prices[ticker] = price
			}
			if cap := jsonFloat(entry, "market_cap", "marketCap", "market_capitalization"); cap > 0 {
				marketCaps[ticker] = cap
			}
			if val := jsonFloat(entry, "value"); val > 0 && prices[ticker] == 0 && marketCaps[ticker] == 0 {
				classifyValuation(ticker, val, marketCaps, prices)
			}
		}
		return nil
	}
	var asMap map[string]any
	if err := json.Unmarshal(data, &asMap); err != nil {
		return err
	}
	for key, value := range asMap {
		ticker := strings.ToUpper(strings.TrimSpace(key))
		if ticker == "" {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed > 0 {
				classifyValuation(ticker, typed, marketCaps, prices)
			}
		case string:
			if parsed, err := parseValuationNumber(typed); err == nil && parsed > 0 {
				classifyValuation(ticker, parsed, marketCaps, prices)
			}
		case map[string]any:
			if price := jsonFloat(typed, "price", "share_price", "sharePrice"); price > 0 {
				prices[ticker] = price
			}
			if cap := jsonFloat(typed, "market_cap", "marketCap", "market_capitalization"); cap > 0 {
				marketCaps[ticker] = cap
			}
			if val := jsonFloat(typed, "value"); val > 0 && prices[ticker] == 0 && marketCaps[ticker] == 0 {
				classifyValuation(ticker, val, marketCaps, prices)
			}
		}
	}
	return nil
}

func classifyValuation(ticker string, value float64, marketCaps, prices map[string]float64) {
	if value >= 1_000_000 {
		marketCaps[ticker] = value
	} else {
		prices[ticker] = value
	}
}

func normalizeValuationHeading(h string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(h) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func findColumn(columns map[string]int, aliases ...string) (int, bool) {
	for _, alias := range aliases {
		if index, ok := columns[alias]; ok {
			return index, true
		}
	}
	return -1, false
}

func parseValuationNumber(raw string) (float64, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "$")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	multiplier := 1.0
	lower := strings.ToLower(cleaned)
	if strings.HasSuffix(lower, "b") {
		multiplier = 1e9
		cleaned = cleaned[:len(cleaned)-1]
	} else if strings.HasSuffix(lower, "m") {
		multiplier = 1e6
		cleaned = cleaned[:len(cleaned)-1]
	} else if strings.HasSuffix(lower, "k") {
		multiplier = 1e3
		cleaned = cleaned[:len(cleaned)-1]
	} else if strings.HasSuffix(lower, "t") {
		multiplier = 1e12
		cleaned = cleaned[:len(cleaned)-1]
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(cleaned), 64)
	if err != nil {
		return 0, err
	}
	return value * multiplier, nil
}

func jsonString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.ToUpper(strings.TrimSpace(s))
			}
		}
	}
	return ""
}

func jsonFloat(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch typed := v.(type) {
			case float64:
				return typed
			case string:
				if parsed, err := parseValuationNumber(typed); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

// FetchLivePrices fetches share prices for missing tickers.
func FetchLivePrices(ctx context.Context, tickers []string, logOut io.Writer) map[string]float64 {
	out := make(map[string]float64)
	if len(tickers) == 0 {
		return out
	}
	httpClient := &http.Client{Timeout: 6 * time.Second}
	for _, rawTicker := range tickers {
		ticker := strings.ToUpper(strings.TrimSpace(rawTicker))
		if ticker == "" {
			continue
		}
		if price, err := fetchYahooChartPrice(ctx, httpClient, ticker); err == nil && price > 0 {
			out[ticker] = price
			if logOut != nil {
				_, _ = fmt.Fprintf(logOut, "  [quote] %s: $%.2f (yahoo)\n", ticker, price)
			}
			continue
		}
		if price, err := fetchStooqPrice(ctx, httpClient, ticker); err == nil && price > 0 {
			out[ticker] = price
			if logOut != nil {
				_, _ = fmt.Fprintf(logOut, "  [quote] %s: $%.2f (stooq)\n", ticker, price)
			}
			continue
		}
		if logOut != nil {
			_, _ = fmt.Fprintf(logOut, "  [quote] %s: failed to resolve live price\n", ticker)
		}
	}
	return out
}

func yahooSymbolForTicker(ticker string) string {
	upper := strings.ToUpper(strings.TrimSpace(ticker))
	dotIndex := strings.LastIndex(upper, ".")
	if dotIndex > 0 {
		suffix := upper[dotIndex:]
		switch suffix {
		case ".L", ".PA", ".DE", ".AS", ".SW", ".MI", ".MC", ".CO", ".ST", ".HE", ".VI", ".BR", ".LS", ".IR", ".TO", ".V", ".AX":
			return ticker
		}
	}
	return strings.ReplaceAll(ticker, ".", "-")
}

func fetchYahooChartPrice(ctx context.Context, client *http.Client, ticker string) (float64, error) {
	yahooSymbol := yahooSymbolForTicker(ticker)
	endpoint := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=5d", url.PathEscape(yahooSymbol))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", browserUserAgent)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return 0, fmt.Errorf("yahoo HTTP %d", response.StatusCode)
	}
	var payload struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Currency           string  `json:"currency"`
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					PreviousClose      float64 `json:"previousClose"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return 0, err
	}
	if len(payload.Chart.Result) == 0 {
		return 0, fmt.Errorf("empty chart result for %s", ticker)
	}
	meta := payload.Chart.Result[0].Meta
	price := meta.RegularMarketPrice
	if price <= 0 || !finite(price) {
		price = meta.PreviousClose
	}
	if price > 0 && finite(price) {
		if strings.EqualFold(meta.Currency, "GBp") {
			price = price / 100.0
		}
		return price, nil
	}
	return 0, fmt.Errorf("no positive price in yahoo response for %s", ticker)
}

func fetchStooqPrice(ctx context.Context, client *http.Client, ticker string) (float64, error) {
	upper := strings.ToUpper(strings.TrimSpace(ticker))
	stooqSymbol := strings.ToLower(strings.ReplaceAll(ticker, ".", "-")) + ".us"
	if strings.HasSuffix(upper, ".L") {
		stooqSymbol = strings.ToLower(strings.TrimSuffix(upper, ".L")) + ".uk"
	} else if strings.HasSuffix(upper, ".DE") {
		stooqSymbol = strings.ToLower(strings.TrimSuffix(upper, ".DE")) + ".de"
	}
	endpoint := "https://stooq.com/q/l/?s=" + url.QueryEscape(stooqSymbol) + "&f=sd2t2ohlcv&h&e=csv"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", browserUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return 0, fmt.Errorf("stooq HTTP %d", response.StatusCode)
	}
	rows, err := csv.NewReader(io.LimitReader(response.Body, 4096)).ReadAll()
	if err != nil {
		return 0, err
	}
	if len(rows) < 2 || len(rows[1]) < 7 {
		return 0, fmt.Errorf("empty stooq response")
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(rows[1][6]), 64)
	if err != nil || price <= 0 || !finite(price) {
		return 0, fmt.Errorf("stooq returned non-numeric close for %s", ticker)
	}
	return price, nil
}
