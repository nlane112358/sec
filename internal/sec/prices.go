package sec

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
	for _, record := range records[start:] {
		if tickerColumn < 0 || tickerColumn >= len(record) {
			continue
		}
		ticker := normalizeValuationTicker(record[tickerColumn])
		if ticker == "" {
			continue
		}
		if hasPriceColumn && priceColumn < len(record) {
			setValuation(marketCaps, prices, ticker, "price", record[priceColumn])
		}
		if hasMarketCapColumn && marketCapColumn < len(record) {
			setValuation(marketCaps, prices, ticker, "market_cap", record[marketCapColumn])
		}
		if hasValueColumn && valueColumn < len(record) {
			setValuation(marketCaps, prices, ticker, "", record[valueColumn])
		}
		if !header && len(record) > 1 {
			setValuation(marketCaps, prices, ticker, "", record[1])
		}
	}
	return nil
}

func loadJSONValuationInputs(data []byte, marketCaps, prices map[string]float64) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	consumeJSONValuations(value, "", "", marketCaps, prices)
	return nil
}

func consumeJSONValuations(value any, tickerHint, kindHint string, marketCaps, prices map[string]float64) {
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			consumeJSONValuations(item, "", kindHint, marketCaps, prices)
		}
	case map[string]any:
		lower := make(map[string]any, len(node))
		for key, item := range node {
			lower[normalizeValuationHeading(key)] = item
		}
		ticker := ""
		for _, key := range []string{"ticker", "symbol", "tickersymbol"} {
			if item, ok := lower[key]; ok {
				ticker = normalizeValuationTicker(fmt.Sprint(item))
				break
			}
		}
		if ticker == "" {
			ticker = normalizeValuationTicker(tickerHint)
		}
		if ticker != "" {
			foundValue := false
			for _, key := range []string{"marketcap", "marketcapitalization", "marketvalue"} {
				if item, ok := lower[key]; ok {
					setValuation(marketCaps, prices, ticker, "market_cap", item)
					foundValue = true
					break
				}
			}
			for _, key := range []string{"price", "shareprice", "stockprice", "regularmarketprice"} {
				if item, ok := lower[key]; ok {
					setValuation(marketCaps, prices, ticker, "price", item)
					foundValue = true
					break
				}
			}
			if item, ok := lower["value"]; ok {
				setValuation(marketCaps, prices, ticker, kindHint, item)
				foundValue = true
			}
			if foundValue {
				return
			}
		}
		for key, item := range node {
			kind := ""
			switch normalizeValuationHeading(key) {
			case "prices", "shareprices", "stockprices":
				kind = "price"
			case "marketcaps", "marketcapitalizations", "marketvalues":
				kind = "market_cap"
			case "quotes", "data", "results":
				consumeJSONValuations(item, "", kindHint, marketCaps, prices)
				continue
			}
			if kind != "" {
				consumeJSONValuations(item, "", kind, marketCaps, prices)
				continue
			}
			if nested, ok := item.(map[string]any); ok {
				consumeJSONValuations(nested, key, kindHint, marketCaps, prices)
				continue
			}
			if list, ok := item.([]any); ok {
				consumeJSONValuations(list, "", kindHint, marketCaps, prices)
				continue
			}
			ticker := normalizeValuationTicker(key)
			if ticker != "" {
				setValuation(marketCaps, prices, ticker, kindHint, item)
			}
		}
	}
}

func findColumn(columns map[string]int, names ...string) (int, bool) {
	for _, name := range names {
		if index, ok := columns[name]; ok {
			return index, true
		}
	}
	return -1, false
}

func normalizeValuationHeading(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(value)
	return value
}

func normalizeValuationTicker(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Trim(value, "\"'$")
	return strings.TrimSpace(value)
}

func setValuation(marketCaps, prices map[string]float64, ticker, kind string, raw any) {
	ticker = normalizeValuationTicker(ticker)
	if ticker == "" {
		return
	}
	value, ok := parseValuationNumber(raw)
	if !ok || !finite(value) || value <= 0 {
		return
	}
	switch kind {
	case "market_cap":
		marketCaps[ticker] = value
	case "price":
		prices[ticker] = value
	default:
		if value >= 1_000_000 {
			marketCaps[ticker] = value
		} else {
			prices[ticker] = value
		}
	}
}

func parseValuationNumber(raw any) (float64, bool) {
	text := strings.TrimSpace(fmt.Sprint(raw))
	text = strings.Trim(text, "\"'$")
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(text), "USD"))
	if text == "" {
		return 0, false
	}
	multiplier := 1.0
	switch strings.ToUpper(text[len(text)-1:]) {
	case "T":
		multiplier, text = 1e12, text[:len(text)-1]
	case "B":
		multiplier, text = 1e9, text[:len(text)-1]
	case "M":
		multiplier, text = 1e6, text[:len(text)-1]
	case "K":
		multiplier, text = 1e3, text[:len(text)-1]
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, false
	}
	return value * multiplier, true
}

// FetchLivePrices queries Yahoo Finance's chart endpoint and falls back to
// Stooq for symbols Yahoo does not return. Quote resolution is written to logOut.
func FetchLivePrices(ctx context.Context, tickers []string, logOut io.Writer) map[string]float64 {
	out := make(map[string]float64, len(tickers))
	httpClient := &http.Client{Timeout: 10 * time.Second}
	for _, rawTicker := range tickers {
		if ctx.Err() != nil {
			break
		}
		ticker := normalizeValuationTicker(rawTicker)
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

func fetchYahooChartPrice(ctx context.Context, client *http.Client, ticker string) (float64, error) {
	yahooSymbol := strings.ReplaceAll(ticker, ".", "-")
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
	if meta.RegularMarketPrice > 0 && finite(meta.RegularMarketPrice) {
		return meta.RegularMarketPrice, nil
	}
	if meta.PreviousClose > 0 && finite(meta.PreviousClose) {
		return meta.PreviousClose, nil
	}
	return 0, fmt.Errorf("no positive price in yahoo response for %s", ticker)
}

func fetchStooqPrice(ctx context.Context, client *http.Client, ticker string) (float64, error) {
	stooqSymbol := strings.ToLower(strings.ReplaceAll(ticker, ".", "-")) + ".us"
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
