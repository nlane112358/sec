import React, { useState, useEffect } from 'react'
import { ExternalLink, Copy, Check, ChevronLeft, ChevronRight, ArrowUpRight, Star } from 'lucide-react'
import { SummaryResponse, QuoteResponse } from '../types'
import { TabKey } from './Tabs'
import { formatCurrency } from '../lib/format'

interface CompanyHeaderProps {
  summary: SummaryResponse | null
  onPrevTicker: () => void
  onNextTicker: () => void
  onSelectTab: (tab: TabKey) => void
  isWatched?: boolean
  onToggleWatchlist?: () => void
}

export const CompanyHeader: React.FC<CompanyHeaderProps> = ({
  summary,
  onPrevTicker,
  onNextTicker,
  onSelectTab,
  isWatched = false,
  onToggleWatchlist,
}) => {
  const [copied, setCopied] = useState(false)
  const [quote, setQuote] = useState<QuoteResponse | null>(null)
  const [loadingQuote, setLoadingQuote] = useState(false)

  useEffect(() => {
    if (!summary?.ticker) return
    let ignore = false
    setLoadingQuote(true)
    fetch(`/api/company/${summary.ticker}/quote`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data: QuoteResponse | null) => {
        if (!ignore && data && data.price > 0) {
          setQuote(data)
        }
      })
      .catch(() => {})
      .finally(() => {
        if (!ignore) setLoadingQuote(false)
      })
    return () => {
      ignore = true
    }
  }, [summary?.ticker])

  if (!summary) {
    return (
      <div className="py-6 border-b border-neutral-200 dark:border-neutral-800 animate-pulse font-mono text-xs text-neutral-400">
        Loading financial data from SEC database...
      </div>
    )
  }

  const numericCIK = parseInt(summary.cik, 10).toString()
  const secEdgarURL = `https://www.sec.gov/edgar/browse/?CIK=${numericCIK}`

  const copyTicker = () => {
    navigator.clipboard.writeText(summary.ticker)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const findVal = (name: string): number | undefined => {
    const m = summary.metrics.find((x) => x.Name === name)
    return m?.Values?.[0]?.Present ? m.Values[0].Value : undefined
  }

  const netIncome = findVal('Net Income')
  const adjEBIT = summary.intangibles.AdjustedEBIT?.[0]?.Value
  const fcf = findVal('Free Cash Flow')
  const adjOwnerEarnings = summary.intangibles.AdjustedOwnerEarnings?.[0]?.Value || fcf

  const shares = findVal('Diluted Weighted Average Shares') || findVal('Shares Outstanding')
  const equity = findVal("Stockholders' Equity")
  const debt = findVal('Debt / Borrowings') || 0
  const cash = findVal('Cash & Equivalents') || 0

  const curr = summary.intangibles.Currency || 'USD'

  // Valuation multiples calculated against live market quote
  const livePrice = quote?.price
  const marketCap = livePrice && shares ? livePrice * shares : undefined
  const enterpriseValue = marketCap !== undefined ? marketCap + debt - cash : undefined
  const pe = marketCap && netIncome && netIncome > 0 ? marketCap / netIncome : undefined
  const p_fcf = marketCap && fcf && fcf > 0 ? marketCap / fcf : undefined
  const ev_ebit = enterpriseValue && adjEBIT && adjEBIT > 0 ? enterpriseValue / adjEBIT : undefined
  const pb = marketCap && equity && equity > 0 ? marketCap / equity : undefined
  const ownerEarningsYield =
    adjOwnerEarnings && marketCap && marketCap > 0 ? (adjOwnerEarnings / marketCap) * 100 : undefined

  return (
    <div className="pt-1 pb-4 border-b border-neutral-200 dark:border-neutral-800 space-y-4 font-sans">
      {/* Top Title & Header Navigation */}
      <div className="flex flex-col sm:flex-row sm:items-baseline justify-between gap-3">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-xl sm:text-2xl font-medium text-neutral-950 dark:text-white tracking-tight">
              {summary.company}
            </h1>
            <span className="font-mono text-sm font-semibold text-neutral-900 dark:text-neutral-100 px-2 py-0.5 rounded border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900">
              {summary.ticker}
            </span>
          </div>

          <div className="flex items-center gap-2 mt-1 text-xs text-neutral-500 font-mono">
            <span>CIK {summary.cik}</span>
            <span>•</span>
            <span>{summary.sic_description || `SIC ${summary.sic}`}</span>
            <span>•</span>
            <span className="italic font-serif text-neutral-600 dark:text-neutral-400">
              Standardized SEC XBRL Facts
            </span>
          </div>
        </div>

        {/* Navigation & Controls */}
        <div className="flex items-center gap-1.5 text-xs font-mono">
          {onToggleWatchlist && (
            <button
              onClick={onToggleWatchlist}
              className={`p-1.5 rounded border transition-colors ${
                isWatched
                  ? 'border-amber-400/40 text-amber-500 bg-amber-500/10'
                  : 'border-neutral-200 dark:border-neutral-800 text-neutral-400 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-900'
              }`}
              title={isWatched ? 'Remove from Watchlist' : 'Add to Watchlist'}
            >
              <Star className={`w-3.5 h-3.5 ${isWatched ? 'fill-amber-500' : ''}`} />
            </button>
          )}

          <button
            onClick={copyTicker}
            className="flex items-center gap-1 px-2 py-1 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-colors"
          >
            {copied ? <Check className="w-3 h-3 text-emerald-500" /> : <Copy className="w-3 h-3" />}
            <span>{copied ? 'Copied' : summary.ticker}</span>
          </button>

          <a
            href={secEdgarURL}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1 px-2 py-1 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-colors"
          >
            <span>EDGAR</span>
            <ExternalLink className="w-2.5 h-2.5" />
          </a>

          <div className="flex items-center rounded border border-neutral-200 dark:border-neutral-800 p-0.5">
            <button
              onClick={onPrevTicker}
              className="p-1 rounded text-neutral-500 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-800 transition-colors"
              title="Previous company ([)"
            >
              <ChevronLeft className="w-3.5 h-3.5" />
            </button>
            <span className="text-[10px] text-neutral-400 dark:text-neutral-600 px-1 font-mono">
              [ ]
            </span>
            <button
              onClick={onNextTicker}
              className="p-1 rounded text-neutral-500 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-800 transition-colors"
              title="Next company (])"
            >
              <ChevronRight className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>

      {/* Live Market Quote & Valuation Multiples Command Strip */}
      <div className="flex flex-wrap items-center justify-between gap-y-2 gap-x-6 border-t border-b border-neutral-200 dark:border-neutral-800 py-2.5 text-xs font-mono">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5">
          {/* Price */}
          <div className="flex items-baseline gap-1.5">
            <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
              Price
            </span>
            <span className="font-semibold text-neutral-950 dark:text-white text-sm">
              {livePrice ? `$${livePrice.toFixed(2)}` : loadingQuote ? '...' : '—'}
            </span>
          </div>

          <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden sm:block" />

          {/* Market Cap */}
          {marketCap !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                Mkt Cap
              </span>
              <span className="text-neutral-800 dark:text-neutral-200 font-medium">
                {formatCurrency(marketCap, true, curr)}
              </span>
            </div>
          )}

          {/* P/E */}
          {pe !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                P/E
              </span>
              <span className="text-neutral-800 dark:text-neutral-200 font-medium">
                {pe.toFixed(1)}x
              </span>
            </div>
          )}

          {/* P/FCF */}
          {p_fcf !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                P/FCF
              </span>
              <span className="text-neutral-800 dark:text-neutral-200 font-medium">
                {p_fcf.toFixed(1)}x
              </span>
            </div>
          )}

          {/* EV/Adj EBIT */}
          {ev_ebit !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                EV/Adj. EBIT
              </span>
              <span className="text-neutral-800 dark:text-neutral-200 font-medium">
                {ev_ebit.toFixed(1)}x
              </span>
            </div>
          )}

          {/* P/Book */}
          {pb !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                P/B
              </span>
              <span className="text-neutral-800 dark:text-neutral-200 font-medium">
                {pb.toFixed(1)}x
              </span>
            </div>
          )}

          {/* Owner Earnings Yield */}
          {ownerEarningsYield !== undefined && (
            <div className="flex items-baseline gap-1.5">
              <span className="text-[10px] font-sans uppercase tracking-wider text-neutral-400">
                Owner Yield
              </span>
              <span className="text-neutral-900 dark:text-neutral-100 font-semibold">
                {ownerEarningsYield.toFixed(1)}%
              </span>
            </div>
          )}
        </div>

        <button
          onClick={() => onSelectTab('dcf')}
          className="text-[11px] text-neutral-500 hover:text-neutral-950 dark:hover:text-white flex items-center gap-1 font-sans transition-colors"
        >
          <span>Model Expectations (Reverse DCF)</span>
          <ArrowUpRight className="w-3 h-3" />
        </button>
      </div>
    </div>
  )
}
