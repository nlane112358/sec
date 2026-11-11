package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	fxMutex sync.RWMutex
	fxCache = map[string]float64{
		"USD": 1.0,
		"EUR": 1.08,
		"GBP": 1.28,
		"CHF": 1.12,
		"DKK": 0.145,
		"SEK": 0.095,
		"NOK": 0.092,
		"CAD": 0.73,
		"AUD": 0.65,
		"JPY": 0.0065,
	}
)

// CurrencySymbol returns the symbol for common currency units.
func CurrencySymbol(unit string) string {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY", "CNY":
		return "¥"
	case "CAD":
		return "C$"
	case "AUD":
		return "A$"
	case "CHF":
		return "CHF "
	case "DKK":
		return "DKK "
	case "SEK":
		return "SEK "
	case "NOK":
		return "NOK "
	default:
		return unit + " "
	}
}

// GetFXRateToUSD returns the exchange rate to convert 1 unit of currency to USD.
func GetFXRateToUSD(currency string) float64 {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" {
		return 1.0
	}
	fxMutex.RLock()
	rate, ok := fxCache[cur]
	fxMutex.RUnlock()
	if ok && rate > 0 {
		return rate
	}
	return 1.0
}

// SetFXRate updates the cached FX rate for a given currency to USD.
func SetFXRate(currency string, rate float64) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" || rate <= 0 || !finite(rate) {
		return
	}
	fxMutex.Lock()
	fxCache[cur] = rate
	fxMutex.Unlock()
}

// FetchLiveFXRate queries Yahoo Finance for CURRENCY + "USD=X" and caches the result.
func FetchLiveFXRate(ctx context.Context, currency string) (float64, error) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" {
		return 1.0, nil
	}
	symbol := cur + "USD=X"
	endpoint := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=5d", url.PathEscape(symbol))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GetFXRateToUSD(cur), err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return GetFXRateToUSD(cur), err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GetFXRateToUSD(cur), fmt.Errorf("yahoo FX HTTP %d", resp.StatusCode)
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&payload); err != nil {
		return GetFXRateToUSD(cur), err
	}
	if len(payload.Chart.Result) > 0 {
		meta := payload.Chart.Result[0].Meta
		price := meta.RegularMarketPrice
		if price <= 0 || !finite(price) {
			price = meta.PreviousClose
		}
		if price > 0 && finite(price) {
			SetFXRate(cur, price)
			return price, nil
		}
	}
	return GetFXRateToUSD(cur), nil
}

// ToUSD converts an amount in the given currency to USD.
func ToUSD(amount float64, currency string) float64 {
	if !finite(amount) || amount == 0 {
		return amount
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "USD" {
		return amount
	}
	return amount * GetFXRateToUSD(cur)
}

// FromUSD converts an amount in USD to the target currency.
func FromUSD(amountUSD float64, targetCurrency string) float64 {
	if !finite(amountUSD) || amountUSD == 0 {
		return amountUSD
	}
	cur := strings.ToUpper(strings.TrimSpace(targetCurrency))
	if cur == "" || cur == "USD" {
		return amountUSD
	}
	rate := GetFXRateToUSD(cur)
	if rate <= 0 {
		return amountUSD
	}
	return amountUSD / rate
}

// ConvertCurrency converts an amount between two currencies.
func ConvertCurrency(amount float64, fromCurrency, toCurrency string) float64 {
	from := strings.ToUpper(strings.TrimSpace(fromCurrency))
	to := strings.ToUpper(strings.TrimSpace(toCurrency))
	if from == to || from == "" || to == "" {
		return amount
	}
	usd := ToUSD(amount, from)
	return FromUSD(usd, to)
}
