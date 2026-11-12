import React, { useEffect, useState } from 'react'
import { ArrowLeftRight, X } from 'lucide-react'
import { Company, QuoteResponse, SummaryResponse } from '../types'
import { summaryCache, quoteCache, prefetchQuote } from '../lib/cache'
import { TabKey } from './Tabs'
import { FinancialStatement } from './FinancialStatement'
import { DCFModeler } from './DCFModeler'
import { IntangiblesView } from './IntangiblesView'
import { FilingsBrowser } from './FilingsBrowser'
import { UniverseScreener } from './UniverseScreener'
import { PeerComparison } from './PeerComparison'

interface SplitWorkspaceProps {
  initialTicker: string
  initialTab: TabKey
  companies: Company[]
  onExit: () => void
}

interface SplitPaneProps {
  side: 'LEFT' | 'RIGHT'
  ticker: string
  tab: TabKey
  companies: Company[]
  onTickerChange: (ticker: string) => void
  onTabChange: (tab: TabKey) => void
}

const paneTabs: { key: TabKey; label: string }[] = [
  { key: 'statements', label: 'Statements' },
  { key: 'dcf', label: 'DCF' },
  { key: 'intangibles', label: 'ROIC' },
  { key: 'filings', label: 'Filings' },
  { key: 'screener', label: 'Screener' },
  { key: 'compare', label: 'Peers' },
]

const summaryURL = (ticker: string, cadence: 'annual' | 'quarterly' | 'ttm') =>
  cadence === 'quarterly'
    ? `/api/company/${ticker}/summary?view=quarterly&quarters=8`
    : cadence === 'ttm'
      ? `/api/company/${ticker}/summary?view=ttm`
      : `/api/company/${ticker}/summary?view=annual&years=5`

const SplitPane: React.FC<SplitPaneProps> = ({
  side,
  ticker,
  tab,
  companies,
  onTickerChange,
  onTabChange,
}) => {
  const [draftTicker, setDraftTicker] = useState(ticker)
  const [summary, setSummary] = useState<SummaryResponse | null>(null)
  const [quote, setQuote] = useState<QuoteResponse | null>(null)
  const [cadence, setCadence] = useState<'annual' | 'quarterly' | 'ttm'>('annual')
  const [compact, setCompact] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => setDraftTicker(ticker), [ticker])

  useEffect(() => {
    let ignore = false
    const cacheKey = `${ticker}_${cadence}`
    const cached = summaryCache.get(cacheKey)
    setSummary(cached || null)
    setLoading(!cached)
    setError(null)

    fetch(summaryURL(ticker, cadence))
      .then((response) => {
        if (!response.ok) throw new Error(`Could not load ${ticker} financials.`)
        return response.json()
      })
      .then((data: SummaryResponse) => {
        if (ignore) return
        summaryCache.set(cacheKey, data)
        setSummary(data)
      })
      .catch((reason: Error) => {
        if (!ignore && !cached) setError(reason.message)
      })
      .finally(() => {
        if (!ignore) setLoading(false)
      })

    return () => {
      ignore = true
    }
  }, [ticker, cadence])

  useEffect(() => {
    let ignore = false
    setQuote(quoteCache.get(ticker) || null)
    prefetchQuote(ticker).then((data) => {
      if (!ignore && data) setQuote(data)
    })
    return () => {
      ignore = true
    }
  }, [ticker])

  const submitTicker = () => {
    const next = draftTicker.trim().toUpperCase()
    if (next) onTickerChange(next)
  }

  const companyName = summary?.ticker === ticker
    ? summary.company
    : companies.find((company) => company.ticker === ticker)?.name || 'Company workspace'

  return (
    <section className="min-w-0 overflow-hidden rounded-2xl border border-neutral-200/80 dark:border-white/10 bg-white/60 dark:bg-[#030611]/50 shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-neutral-200/80 dark:border-white/10 px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[9px] font-semibold tracking-wider text-neutral-500 dark:bg-neutral-900">{side}</span>
            <h2 className="truncate text-sm font-medium text-neutral-900 dark:text-white">{companyName}</h2>
            <span className="font-mono text-[10px] font-semibold text-indigo-600 dark:text-indigo-300">{ticker}</span>
          </div>
          <div className="mt-1 flex items-center gap-2 font-mono text-[9px] text-neutral-400">
            <span>{summary?.sic_description || 'SEC EDGAR financial workspace'}</span>
            {quote?.ticker === ticker && <span>· ${quote.price.toFixed(2)}</span>}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <input
            value={draftTicker}
            onChange={(event) => setDraftTicker(event.target.value.toUpperCase())}
            onKeyDown={(event) => {
              event.stopPropagation()
              if (event.key === 'Enter') submitTicker()
            }}
            aria-label={`${side.toLowerCase()} pane ticker`}
            placeholder="Ticker"
            maxLength={10}
            className="w-20 rounded-md border border-neutral-300 bg-white px-2 py-1.5 font-mono text-[10px] uppercase text-neutral-900 outline-none focus:border-indigo-500 dark:border-neutral-700 dark:bg-neutral-950 dark:text-white"
          />
          <button onClick={submitTicker} className="rounded-md bg-neutral-900 px-2.5 py-1.5 font-mono text-[10px] text-white hover:opacity-85 dark:bg-white dark:text-neutral-950">Go</button>
        </div>
      </div>

      <nav className="flex gap-1 overflow-x-auto border-b border-neutral-200/70 px-2 py-1.5 dark:border-white/10" aria-label={`${side.toLowerCase()} pane views`}>
        {paneTabs.map((item) => (
          <button
            key={item.key}
            onClick={() => onTabChange(item.key)}
            className={`shrink-0 rounded-md px-2.5 py-1.5 text-[10px] transition-colors ${tab === item.key ? 'bg-neutral-900 font-medium text-white dark:bg-white dark:text-neutral-950' : 'text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-900 dark:hover:text-white'}`}
          >
            {item.label}
          </button>
        ))}
      </nav>

      <div className="min-h-[65vh] min-w-0 overflow-x-hidden px-3 py-3 sm:px-4">
        {loading && !summary && <div className="py-16 text-center font-mono text-[10px] text-neutral-400">Loading {ticker}…</div>}
        {error && <div className="mb-3 rounded-lg border border-rose-300 bg-rose-50 px-3 py-2 text-[10px] text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">{error}</div>}
        {summary?.ticker === ticker && (
          <>
            {tab === 'statements' && (
              <FinancialStatement
                summary={summary}
                compact={compact}
                onToggleCompact={() => setCompact((value) => !value)}
                cadence={cadence}
                onSelectCadence={setCadence}
              />
            )}
            {tab === 'dcf' && <DCFModeler summary={summary} compact={compact} quote={quote} />}
            {tab === 'intangibles' && <IntangiblesView summary={summary} compact={compact} />}
            {tab === 'filings' && <FilingsBrowser ticker={ticker} />}
            {tab === 'screener' && (
              <UniverseScreener
                companies={companies}
                currentTicker={ticker}
                onSelectTicker={onTickerChange}
              />
            )}
            {tab === 'compare' && (
              <PeerComparison
                currentTicker={ticker}
                companies={companies}
                onSelectTicker={onTickerChange}
              />
            )}
          </>
        )}
      </div>
    </section>
  )
}

export const SplitWorkspace: React.FC<SplitWorkspaceProps> = ({
  initialTicker,
  initialTab,
  companies,
  onExit,
}) => {
  const [leftTicker, setLeftTicker] = useState(initialTicker)
  const [rightTicker, setRightTicker] = useState(initialTicker === 'MSFT' ? 'AAPL' : 'MSFT')
  const [leftTab, setLeftTab] = useState<TabKey>(initialTab)
  const [rightTab, setRightTab] = useState<TabKey>(initialTab === 'statements' ? 'dcf' : 'statements')

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 px-1">
        <div>
          <div className="flex items-center gap-2 text-sm font-medium text-neutral-900 dark:text-white">
            <ArrowLeftRight className="h-4 w-4 text-indigo-500" />
            Split workspace
          </div>
          <p className="mt-1 text-[10px] text-neutral-500">Each pane has its own company, view, and statement cadence.</p>
        </div>
        <button onClick={onExit} className="inline-flex items-center gap-1.5 rounded-lg border border-neutral-200 px-2.5 py-1.5 text-[10px] text-neutral-500 hover:text-neutral-900 dark:border-white/10 dark:hover:text-white">
          <X className="h-3 w-3" /> Exit split view
        </button>
      </div>
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <SplitPane side="LEFT" ticker={leftTicker} tab={leftTab} companies={companies} onTickerChange={setLeftTicker} onTabChange={setLeftTab} />
        <SplitPane side="RIGHT" ticker={rightTicker} tab={rightTab} companies={companies} onTickerChange={setRightTicker} onTabChange={setRightTab} />
      </div>
    </div>
  )
}
