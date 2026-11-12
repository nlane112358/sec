import React, { useEffect, useMemo, useState } from 'react'
import { Activity, Code2, GitCompare, Loader2 } from 'lucide-react'
import { Company, QuoteResponse, SummaryResponse } from '../types'
import { formatCurrency } from '../lib/format'
import { hasDivision, FormulaNode, parseCustomFormula } from '../lib/customFormula'
import { prefetchQuote, quoteCache, summaryCache } from '../lib/cache'

interface CustomRatioBuilderProps {
  currentTicker: string
  companies: Company[]
  tickers: string[]
}

interface FormulaDataset {
  ticker: string
  summary?: SummaryResponse
  quote?: QuoteResponse
  error?: string
}

type ChartMode = 'trend' | 'peers'
type DisplayMode = 'percent' | 'multiple' | 'value'

const valueAt = (summary: SummaryResponse, name: string, index: number): number | undefined => {
  const value = summary.metrics.find((metric) => metric.Name === name)?.Values?.[index]
  return value?.Present && Number.isFinite(value.Value) ? value.Value : undefined
}

const evaluate = (
  node: FormulaNode,
  dataset: FormulaDataset,
  periodIndex: number,
): number | undefined => {
  if (!dataset.summary) return undefined
  const summary = dataset.summary

  if (node.kind === 'number') return node.value
  if (node.kind === 'unary') {
    const child = evaluate(node.child, dataset, periodIndex)
    if (child === undefined) return undefined
    return node.operator === '-' ? -child : child
  }
  if (node.kind === 'metric') {
    if (node.name === 'Gross Profit') {
      const revenue = valueAt(summary, 'Revenue', periodIndex)
      const cost = valueAt(summary, 'Cost of Revenue', periodIndex)
      return revenue !== undefined && cost !== undefined ? revenue - Math.abs(cost) : undefined
    }
    if (node.name === 'Enterprise Value') {
      const price = dataset.quote?.ticker === dataset.ticker ? dataset.quote.price : undefined
      const shares = valueAt(summary, 'Shares Outstanding', periodIndex) ??
        valueAt(summary, 'Diluted Weighted Average Shares', periodIndex)
      if (price === undefined || price <= 0 || shares === undefined || shares <= 0) return undefined
      const debt = valueAt(summary, 'Debt / Borrowings', periodIndex) ?? 0
      const cash = valueAt(summary, 'Cash & Equivalents', periodIndex) ?? 0
      return price * shares + debt - cash
    }
    return valueAt(summary, node.name, periodIndex)
  }

  const left = evaluate(node.left, dataset, periodIndex)
  const right = evaluate(node.right, dataset, periodIndex)
  if (left === undefined || right === undefined) return undefined

  let result = Number.NaN
  switch (node.operator) {
    case '+': result = left + right; break
    case '-': result = left - right; break
    case '*': result = left * right; break
    case '/':
      if (right === 0) return undefined
      result = left / right
      break
  }
  return Number.isFinite(result) ? result : undefined
}

const formatResult = (
  value: number | undefined,
  mode: DisplayMode,
  currency: string,
): string => {
  if (value === undefined || !Number.isFinite(value)) return '—'
  if (mode === 'percent') return `${(value * 100).toFixed(2)}%`
  if (mode === 'multiple') return `${value.toFixed(2)}x`
  return formatCurrency(value, true, currency)
}

export const CustomRatioBuilder: React.FC<CustomRatioBuilderProps> = ({
  currentTicker,
  companies,
  tickers,
}) => {
  const [expression, setExpression] = useState('(Free Cash Flow - Stock-Based Compensation) / Enterprise Value')
  const [chartMode, setChartMode] = useState<ChartMode>('trend')
  const [displayMode, setDisplayMode] = useState<DisplayMode>('percent')
  const [selectedTicker, setSelectedTicker] = useState(currentTicker)
  const [datasets, setDatasets] = useState<Record<string, FormulaDataset>>({})
  const [loading, setLoading] = useState(false)
  const [dataError, setDataError] = useState<string | null>(null)

  const normalizedTickers = useMemo(() => [...new Set(tickers.map((ticker) => ticker.toUpperCase()))], [tickers])
  const tickersKey = normalizedTickers.join(',')
  const parsedFormula = useMemo(() => {
    try {
      return { node: parseCustomFormula(expression), error: null as string | null }
    } catch (error) {
      return { node: null, error: error instanceof Error ? error.message : 'Unable to parse formula.' }
    }
  }, [expression])

  useEffect(() => {
    if (!normalizedTickers.includes(selectedTicker)) {
      setSelectedTicker(normalizedTickers.includes(currentTicker) ? currentTicker : normalizedTickers[0] || currentTicker)
    }
  }, [tickersKey, currentTicker, selectedTicker])

  useEffect(() => {
    let ignore = false
    const loadDatasets = async () => {
      setLoading(true)
      setDataError(null)
      const loaded = await Promise.all(normalizedTickers.map(async (ticker): Promise<FormulaDataset> => {
        const cacheKey = `${ticker}_annual_10`
        let summary = summaryCache.get(cacheKey)
        if (!summary) {
          try {
            const response = await fetch(`/api/company/${ticker}/summary?view=annual&years=10`)
            if (!response.ok) throw new Error(`Could not load ${ticker}`)
            summary = await response.json() as SummaryResponse
            summaryCache.set(cacheKey, summary)
          } catch {
            return { ticker, error: '10-year financial data is unavailable.' }
          }
        }

        let quote = quoteCache.get(ticker)
        if (!quote) quote = (await prefetchQuote(ticker)) || undefined
        return { ticker, summary, quote }
      }))

      if (!ignore) {
        const byTicker = Object.fromEntries(loaded.map((item) => [item.ticker, item])) as Record<string, FormulaDataset>
        setDatasets(byTicker)
        if (loaded.every((item) => !item.summary)) {
          setDataError('No 10-year financial data could be loaded for this cohort.')
        }
        setLoading(false)
      }
    }

    void loadDatasets()
    return () => {
      ignore = true
    }
  }, [tickersKey])

  const selectedDataset = datasets[selectedTicker]
  const trendRows = useMemo(() => {
    if (!parsedFormula.node || !selectedDataset?.summary) return []
    const { periods } = selectedDataset.summary
    return [...periods].reverse().map((period, index) => {
      const periodIndex = periods.length - 1 - index
      return {
        year: period.Year || period.End.slice(0, 4),
        value: evaluate(parsedFormula.node!, selectedDataset, periodIndex),
      }
    })
  }, [parsedFormula.node, selectedDataset])

  const peerRows = useMemo(() => {
    if (!parsedFormula.node) return []
    return normalizedTickers.map((ticker) => {
      const dataset = datasets[ticker]
      const value = dataset?.summary ? evaluate(parsedFormula.node!, dataset, 0) : undefined
      const company = companies.find((item) => item.ticker === ticker)
      return { ticker, company: company?.name || dataset?.summary?.company || ticker, value }
    })
  }, [parsedFormula.node, normalizedTickers, datasets, companies])

  const ratioLike = parsedFormula.node ? hasDivision(parsedFormula.node) : true
  const validTrendRows = trendRows.filter((row) => row.value !== undefined)
  const trendMin = Math.min(0, ...validTrendRows.map((row) => row.value!))
  const trendMax = Math.max(0, ...validTrendRows.map((row) => row.value!))
  const trendRange = trendMax - trendMin || 1
  const yForValue = (value: number) => 92 - ((value - trendMin) / trendRange) * 84
  let isDrawing = false
  const pathSegments: string[] = []
  trendRows.forEach((row, index) => {
    if (row.value === undefined) {
      isDrawing = false
      return
    }
    const x = trendRows.length <= 1 ? 500 : (index / (trendRows.length - 1)) * 1000
    const y = yForValue(row.value)
    pathSegments.push(`${isDrawing ? 'L' : 'M'} ${x.toFixed(1)} ${y.toFixed(1)}`)
    isDrawing = true
  })

  const maxAbsPeerValue = Math.max(0.000001, ...peerRows.map((row) => Math.abs(row.value || 0)))

  return (
    <section className="rounded-xl border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-neutral-200 dark:border-white/10 px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
            <Code2 className="h-4 w-4 text-indigo-500" />
            <h3 className="text-xs font-semibold uppercase tracking-wider text-neutral-900 dark:text-neutral-100">Custom Formula / Ratio</h3>
          </div>
          <p className="mt-1 text-[10px] text-neutral-500">Use financial line-item names, numbers, parentheses, and + - * /.</p>
        </div>
        <div className="flex items-center gap-1 rounded-lg border border-neutral-200 dark:border-white/10 p-0.5">
          <button
            onClick={() => setChartMode('trend')}
            className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1.5 text-[10px] transition-colors ${chartMode === 'trend' ? 'bg-neutral-900 text-white dark:bg-white dark:text-neutral-950' : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white'}`}
          >
            <Activity className="h-3 w-3" /> 10-year trend
          </button>
          <button
            onClick={() => setChartMode('peers')}
            className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1.5 text-[10px] transition-colors ${chartMode === 'peers' ? 'bg-neutral-900 text-white dark:bg-white dark:text-neutral-950' : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white'}`}
          >
            <GitCompare className="h-3 w-3" /> Peer group
          </button>
        </div>
      </div>

      <div className="space-y-3 px-4 py-3">
        <div className="flex flex-col gap-2 lg:flex-row">
          <div className="flex min-w-0 flex-1 items-center rounded-lg border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-950 focus-within:border-indigo-500">
            <span className="px-3 text-[10px] font-mono text-neutral-400">ƒx</span>
            <input
              value={expression}
              onChange={(event) => setExpression(event.target.value)}
              onKeyDown={(event) => event.stopPropagation()}
              aria-label="Custom financial formula"
              placeholder="(Free Cash Flow - Stock-Based Compensation) / Enterprise Value"
              className="min-w-0 flex-1 bg-transparent py-2 pr-3 font-mono text-[11px] text-neutral-900 outline-none dark:text-neutral-100"
            />
          </div>
          <div className="flex items-center gap-2">
            {chartMode === 'trend' && (
              <select
                value={selectedTicker}
                onChange={(event) => setSelectedTicker(event.target.value)}
                className="rounded-lg border border-neutral-300 bg-white px-2.5 py-2 text-[10px] font-mono text-neutral-700 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-200"
                aria-label="Select company for historical trend"
              >
                {normalizedTickers.map((ticker) => <option key={ticker} value={ticker}>{ticker}</option>)}
              </select>
            )}
            <select
              value={displayMode}
              onChange={(event) => setDisplayMode(event.target.value as DisplayMode)}
              className="rounded-lg border border-neutral-300 bg-white px-2.5 py-2 text-[10px] font-mono text-neutral-700 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-200"
              aria-label="Custom formula display format"
            >
              <option value="percent">Percent</option>
              <option value="multiple">Multiple</option>
              <option value="value">Value</option>
            </select>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-1.5 text-[9px]">
          <span className="mr-1 text-neutral-400">Examples:</span>
          <button
            onClick={() => setExpression('(Free Cash Flow - Stock-Based Compensation) / Enterprise Value')}
            className="rounded-md border border-neutral-200 px-2 py-1 font-mono text-neutral-500 hover:border-indigo-400 hover:text-indigo-600 dark:border-white/10 dark:hover:text-indigo-300"
          >
            (FCF − SBC) / EV
          </button>
          <button
            onClick={() => setExpression('Research & Development / Gross Profit')}
            className="rounded-md border border-neutral-200 px-2 py-1 font-mono text-neutral-500 hover:border-indigo-400 hover:text-indigo-600 dark:border-white/10 dark:hover:text-indigo-300"
          >
            R&amp;D / Gross Profit
          </button>
        </div>

        {parsedFormula.error ? (
          <p className="text-[10px] text-rose-600 dark:text-rose-400">{parsedFormula.error}</p>
        ) : (
          <p className="text-[9px] text-neutral-400">
            Gross Profit = Revenue − Cost of Revenue. Enterprise Value uses today’s quote with each year’s period-end shares, debt, and cash.
          </p>
        )}

        {loading && (
          <div className="flex items-center gap-2 py-8 text-xs text-neutral-500">
            <Loader2 className="h-4 w-4 animate-spin" /> Loading 10 years for {normalizedTickers.length} companies…
          </div>
        )}
        {!loading && dataError && <p className="py-4 text-xs text-amber-600 dark:text-amber-400">{dataError}</p>}

        {!loading && !parsedFormula.error && chartMode === 'trend' && !selectedDataset?.summary && (
          <p className="py-6 text-center text-xs text-neutral-500">10-year financial data is unavailable for {selectedTicker}.</p>
        )}

        {!loading && !parsedFormula.error && chartMode === 'trend' && selectedDataset?.summary && (
          <div>
            <div className="mb-2 flex items-center justify-between text-[10px] text-neutral-500">
              <span>{selectedTicker} · annual series · {validTrendRows.length} periods with complete inputs</span>
              {ratioLike && <span>{displayMode === 'percent' ? 'Ratio shown as percent' : `Ratio shown as ${displayMode}`}</span>}
            </div>
            {validTrendRows.length > 0 ? (
              <div className="rounded-lg border border-neutral-200/70 dark:border-white/10 bg-white/40 dark:bg-black/10 px-2 pt-2">
                <div className="relative h-48">
                  {[0, 0.25, 0.5, 0.75, 1].map((fraction) => {
                    const value = trendMax - trendRange * fraction
                    const top = 8 + fraction * 84
                    return (
                      <React.Fragment key={fraction}>
                        <span className="absolute left-0 -top-2 w-12 text-right font-mono text-[9px] text-neutral-400" style={{ top: `${top}%` }}>
                          {formatResult(value, displayMode, selectedDataset.summary?.intangibles.Currency || 'USD')}
                        </span>
                        <div className="absolute left-14 right-3 border-t border-dashed border-neutral-200 dark:border-white/10" style={{ top: `${top}%` }} />
                      </React.Fragment>
                    )
                  })}
                  <div className="absolute inset-y-0 left-14 right-3">
                    <svg className="absolute inset-0 h-full w-full" viewBox="0 0 1000 100" preserveAspectRatio="none" aria-label={`${selectedTicker} custom formula trend`}>
                      <path d={pathSegments.join(' ')} fill="none" stroke="#6366f1" strokeWidth="1.8" vectorEffect="non-scaling-stroke" />
                    </svg>
                    {trendRows.map((row, index) => {
                      if (row.value === undefined) return null
                      const left = trendRows.length <= 1 ? 50 : (index / (trendRows.length - 1)) * 100
                      const top = yForValue(row.value)
                      return (
                        <span
                          key={`${row.year}-${index}`}
                          title={`${row.year}: ${formatResult(row.value, displayMode, selectedDataset.summary?.intangibles.Currency || 'USD')}`}
                          className="absolute z-10 h-2.5 w-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-indigo-500 bg-white dark:bg-neutral-950"
                          style={{ left: `${left}%`, top: `${top}%` }}
                        />
                      )
                    })}
                  </div>
                </div>
                <div className="ml-14 mr-3 flex justify-between pb-2 text-[9px] font-mono text-neutral-400">
                  {trendRows.map((row, index) => (
                    <span key={`${row.year}-label`} className={index % Math.max(1, Math.ceil(trendRows.length / 10)) === 0 ? '' : 'hidden'}>{row.year}</span>
                  ))}
                </div>
              </div>
            ) : (
              <p className="py-8 text-center text-xs text-neutral-500">This formula has no complete annual observations for {selectedTicker}.</p>
            )}
          </div>
        )}

        {!loading && !parsedFormula.error && chartMode === 'peers' && (
          <div className="space-y-2">
            <p className="text-[10px] text-neutral-500">Latest annual value by selected peer · EV uses the current quote</p>
            {peerRows.map((row) => {
              const value = row.value
              const width = value === undefined ? 0 : (Math.abs(value) / maxAbsPeerValue) * 100
              const dataset = datasets[row.ticker]
              const currency = dataset?.summary?.intangibles.Currency || 'USD'
              return (
                <div key={row.ticker} className="grid grid-cols-[4.5rem_minmax(0,1fr)_6rem] items-center gap-3 rounded-lg border border-neutral-200/60 dark:border-white/[0.06] px-3 py-2">
                  <div>
                    <div className="font-mono text-[11px] font-semibold text-neutral-900 dark:text-neutral-100">{row.ticker}</div>
                    <div className="truncate text-[9px] text-neutral-400">{row.company}</div>
                  </div>
                  <div className="h-3 rounded-full bg-neutral-100 dark:bg-neutral-900">
                    {value !== undefined && <div className={`h-full rounded-full ${value < 0 ? 'bg-rose-500/70' : 'bg-indigo-500/75'}`} style={{ width: `${width}%` }} />}
                  </div>
                  <div className="text-right font-mono text-[10px] text-neutral-700 dark:text-neutral-300">{formatResult(value, displayMode, currency)}</div>
                </div>
              )
            })}
            {peerRows.every((row) => row.value === undefined) && <p className="py-6 text-center text-xs text-neutral-500">No peer has complete inputs for this formula.</p>}
          </div>
        )}
      </div>
    </section>
  )
}
