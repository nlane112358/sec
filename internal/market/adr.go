package market

import (
	"strings"
	"sync"
)

// ADRInfo holds ADR-to-ordinary share ratio and domestic ticker metadata.
type ADRInfo struct {
	Ticker           string
	OrdinaryTicker   string
	Ratio            float64 // Ordinary shares represented by 1 ADR (e.g. 0.5 for AZN, 2.0 for SHEL)
	DomesticExchange string
}

var (
	adrMutex    sync.RWMutex
	adrRegistry = map[string]ADRInfo{
		"AZN":   {Ticker: "AZN", OrdinaryTicker: "AZN.L", Ratio: 0.5, DomesticExchange: "LSE"},
		"SHEL":  {Ticker: "SHEL", OrdinaryTicker: "SHEL.L", Ratio: 2.0, DomesticExchange: "LSE"},
		"BP":    {Ticker: "BP", OrdinaryTicker: "BP.L", Ratio: 6.0, DomesticExchange: "LSE"},
		"GSK":   {Ticker: "GSK", OrdinaryTicker: "GSK.L", Ratio: 2.0, DomesticExchange: "LSE"},
		"SNY":   {Ticker: "SNY", OrdinaryTicker: "SAN.PA", Ratio: 0.5, DomesticExchange: "Euronext Paris"},
		"NVO":   {Ticker: "NVO", OrdinaryTicker: "NOVO-B.CO", Ratio: 1.0, DomesticExchange: "Nasdaq Copenhagen"},
		"ASML":  {Ticker: "ASML", OrdinaryTicker: "ASML.AS", Ratio: 1.0, DomesticExchange: "Euronext Amsterdam"},
		"SAP":   {Ticker: "SAP", OrdinaryTicker: "SAP.DE", Ratio: 1.0, DomesticExchange: "Xetra"},
		"BTI":   {Ticker: "BTI", OrdinaryTicker: "BATS.L", Ratio: 1.0, DomesticExchange: "LSE"},
		"UL":    {Ticker: "UL", OrdinaryTicker: "ULVR.L", Ratio: 1.0, DomesticExchange: "LSE"},
		"DEO":   {Ticker: "DEO", OrdinaryTicker: "DGE.L", Ratio: 4.0, DomesticExchange: "LSE"},
		"RIO":   {Ticker: "RIO", OrdinaryTicker: "RIO.L", Ratio: 1.0, DomesticExchange: "LSE"},
		"HSBC":  {Ticker: "HSBC", OrdinaryTicker: "HSBA.L", Ratio: 5.0, DomesticExchange: "LSE"},
		"BCS":   {Ticker: "BCS", OrdinaryTicker: "BARC.L", Ratio: 4.0, DomesticExchange: "LSE"},
		"LYG":   {Ticker: "LYG", OrdinaryTicker: "LLOY.L", Ratio: 4.0, DomesticExchange: "LSE"},
		"RELX":  {Ticker: "RELX", OrdinaryTicker: "REL.L", Ratio: 1.0, DomesticExchange: "LSE"},
		"VOD":   {Ticker: "VOD", OrdinaryTicker: "VOD.L", Ratio: 10.0, DomesticExchange: "LSE"},
		"ORAN":  {Ticker: "ORAN", OrdinaryTicker: "ORA.PA", Ratio: 1.0, DomesticExchange: "Euronext Paris"},
		"TTE":   {Ticker: "TTE", OrdinaryTicker: "TTE.PA", Ratio: 1.0, DomesticExchange: "Euronext Paris"},
		"NVS":   {Ticker: "NVS", OrdinaryTicker: "NOVN.SW", Ratio: 1.0, DomesticExchange: "SIX Swiss"},
		"ABBNY": {Ticker: "ABBNY", OrdinaryTicker: "ABBN.SW", Ratio: 1.0, DomesticExchange: "SIX Swiss"},
		"ARM":   {Ticker: "ARM", OrdinaryTicker: "ARM", Ratio: 1.0, DomesticExchange: "Nasdaq"},
		"BNTX":  {Ticker: "BNTX", OrdinaryTicker: "22UA.DE", Ratio: 1.0, DomesticExchange: "Nasdaq"},
		"SPOT":  {Ticker: "SPOT", OrdinaryTicker: "SPOT", Ratio: 1.0, DomesticExchange: "NYSE"},
	}
)

// RegisterADRRatio records or overrides the ADR ratio for a ticker.
func RegisterADRRatio(ticker string, ratio float64, ordinaryTicker ...string) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" || ratio <= 0 || !finite(ratio) {
		return
	}
	ord := ""
	if len(ordinaryTicker) > 0 {
		ord = ordinaryTicker[0]
	}
	adrMutex.Lock()
	adrRegistry[ticker] = ADRInfo{
		Ticker:         ticker,
		OrdinaryTicker: ord,
		Ratio:          ratio,
	}
	adrMutex.Unlock()
}

// GetADRInfo returns ADR details for a ticker if known.
func GetADRInfo(ticker string) (ADRInfo, bool) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	adrMutex.RLock()
	info, ok := adrRegistry[ticker]
	adrMutex.RUnlock()
	return info, ok
}

// ADRRatio returns the ordinary shares per ADR for a ticker. Returns 1.0 if unknown.
func ADRRatio(ticker string) float64 {
	if info, ok := GetADRInfo(ticker); ok && info.Ratio > 0 {
		return info.Ratio
	}
	return 1.0
}

// OrdinaryToADRShares converts ordinary share counts reported in Form 20-F into
// equivalent ADR share counts trading in the US market.
func OrdinaryToADRShares(ticker string, ordinaryShares float64) float64 {
	if !finite(ordinaryShares) || ordinaryShares <= 0 {
		return ordinaryShares
	}
	ratio := ADRRatio(ticker)
	if ratio <= 0 {
		return ordinaryShares
	}
	return ordinaryShares / ratio
}

// ADRSharesToOrdinary converts ADR share counts into ordinary shares.
func ADRSharesToOrdinary(ticker string, adrShares float64) float64 {
	if !finite(adrShares) || adrShares <= 0 {
		return adrShares
	}
	ratio := ADRRatio(ticker)
	if ratio <= 0 {
		return adrShares
	}
	return adrShares * ratio
}
