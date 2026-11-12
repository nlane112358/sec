import React, { useState, useEffect, useMemo } from 'react'
import { GitCompare, X, ArrowRight, Plus } from 'lucide-react'
import { Company, RankedCompany, RankResult } from '../types'
import { formatPercent, formatCurrency } from '../lib/format'

interface PeerComparisonProps {
  currentTicker: string
  companies: Company[]
  onSelectTicker: (ticker: string) => void
}

export const PeerComparison: React.FC<PeerComparisonProps> = ({
  currentTicker,
  companies,
  onSelectTicker,
}) => {
  const [peerTickers, setPeerTickers] = useState<string[]>(() => {
    // Default comparison cohort
    const defaults = ['AAPL', 'MSFT', 'GOOGL', 'NVDA']
    if (!defaults.includes(currentTicker)) {
      return [currentTicker, ...defaults.slice(0, 3)]
    }
    return defaults
  })

  const [inputTicker, setInputTicker] = useState('')
  const [rankedData, setRankedData] = useState<RankResult | null>(null)
  const [loading, setLoading] = useState(false)

  // Current company info for industry matching
  const currentCompany = companies.find((c) => c.ticker === currentTicker)
  const currentSIC = currentCompany?.sic
  const currentSICDesc = currentCompany?.sic_description

  // Suggested industry peers sharing the same SIC code
  const suggestedPeers = useMemo(() => {
    if (!currentSIC) return []
    return companies
      .filter((c) => c.sic === currentSIC && !peerTickers.includes(c.ticker) && c.ticker !== currentTicker)
      .slice(0, 6)
  }, [companies, currentSIC, peerTickers, currentTicker])

  // Ensure currentTicker is included when user navigates
  useEffect(() => {
    setPeerTickers((prev) => {
      if (!prev.includes(currentTicker)) {
        return [currentTicker, ...prev.slice(0, 3)]
      }
      return prev
    })
  }, [currentTicker])

  useEffect(() => {
    let ignore = false
    setLoading(true)
    fetch('/api/screener/rank')
      .then((res) => (res.ok ? res.json() : null))
      .then((data: RankResult) => {
        if (!ignore && data) {
          setRankedData(data)
        }
      })
      .catch(() => {})
      .finally(() => {
        if (!ignore) setLoading(false)
      })

    return () => {
      ignore = true
    }
  }, [])

  const addTicker = (tickerToAdd: string) => {
    const clean = tickerToAdd.trim().toUpperCase()
    if (!clean || peerTickers.includes(clean)) return
    if (companies.some((c) => c.ticker === clean)) {
      setPeerTickers([...peerTickers, clean].slice(0, 5))
      setInputTicker('')
    }
  }

  const removeTicker = (tickerToRemove: string) => {
    if (peerTickers.length <= 1) return
    setPeerTickers(peerTickers.filter((t) => t !== tickerToRemove))
  }

  // Find rank data for selected peer tickers
  const peerData = peerTickers
    .map((ticker) => {
      const ranked = rankedData?.companies.find((c) => c.ticker === ticker)
      const company = companies.find((c) => c.ticker === ticker)
      return {
        ticker,
        companyName: company?.name || ranked?.company || ticker,
        sector: ranked?.sector || 'General',
        ranked,
      }
    })
    .filter(Boolean)

  // Determine top score and metrics among peers
  const topScore = Math.max(...peerData.map((p) => p.ranked?.score || 0))
  const topROIC = Math.max(...peerData.map((p) => (p.ranked?.has_roic ? p.ranked.roic : 0)))
  const topFCFMargin = Math.max(...peerData.map((p) => p.ranked?.fcf_margin || 0))
  const topRevCAGR = Math.max(...peerData.map((p) => p.ranked?.revenue_cagr || 0))

  return (
    <div className="space-y-8 animate-in fade-in duration-150">
      {/* Header & Peer Selector Tray */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pt-2">
        <div>
          <div className="flex items-center gap-2">
            <GitCompare className="w-4 h-4 text-neutral-900 dark:text-white" />
            <h2 className="text-sm font-medium text-neutral-900 dark:text-white">
              Side-by-Side Peer Benchmarking
            </h2>
          </div>
          <p className="text-[11px] text-neutral-500 dark:text-neutral-400 mt-0.5 font-sans">
            Compare quality compounder scores, Peters & Taylor adjusted ROIC, and capital allocation across peers.
          </p>
        </div>

        {/* Add Peer Input */}
        <div className="flex items-center gap-2">
          <div className="relative">
            <input
              type="text"
              value={inputTicker}
              onChange={(e) => setInputTicker(e.target.value.toUpperCase())}
              onKeyDown={(e) => {
                if (e.key === 'Enter') addTicker(inputTicker)
              }}
              placeholder="Add ticker..."
              maxLength={6}
              className="w-36 bg-white dark:bg-neutral-900 border border-neutral-300 dark:border-neutral-800 px-2.5 py-1 text-xs text-neutral-900 dark:text-neutral-100 uppercase font-mono outline-none focus:border-neutral-500"
            />
          </div>
          <button
            onClick={() => addTicker(inputTicker)}
            className="px-2.5 py-1 bg-neutral-900 text-white dark:bg-white dark:text-black font-mono text-xs font-medium hover:opacity-90 transition-opacity"
          >
            Add
          </button>
        </div>
      </div>

      {/* Selected Peer Pills */}
      <div className="flex flex-wrap items-center gap-2 font-mono text-xs">
        <span className="text-neutral-500 text-[11px] uppercase tracking-wider">Cohort:</span>
        {peerTickers.map((t) => (
          <span
            key={t}
            className={`inline-flex items-center gap-1.5 px-2 py-0.5 border text-xs ${
              t === currentTicker
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black border-transparent font-medium'
                : 'bg-white dark:bg-neutral-900 border-neutral-300 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300'
            }`}
          >
            <span>{t}</span>
            {peerTickers.length > 1 && (
              <button
                onClick={() => removeTicker(t)}
                className="hover:text-rose-500 transition-colors p-0.5"
                title="Remove peer"
              >
                <X className="w-3 h-3" />
              </button>
            )}
          </span>
        ))}
      </div>

      {/* Suggested Industry Peers from Shared SIC */}
      {suggestedPeers.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 px-3 py-2 border border-neutral-200 dark:border-neutral-800 bg-neutral-50/60 dark:bg-neutral-900/40 rounded-lg text-xs font-mono">
          <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans mr-1">
            Suggested Industry Peers ({currentSICDesc || `SIC ${currentSIC}`}):
          </span>
          {suggestedPeers.map((p) => (
            <button
              key={p.ticker}
              onClick={() => addTicker(p.ticker)}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 text-neutral-700 dark:text-neutral-300 hover:text-neutral-950 dark:hover:text-white hover:border-neutral-400 text-[11px] transition-colors"
              title={`${p.name} (${p.ticker})`}
            >
              <span>+ {p.ticker}</span>
            </button>
          ))}
        </div>
      )}

      {/* Side-by-Side Comparison Matrix */}
      <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead>
              <tr className="border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/40">
                <th className="py-2.5 px-4 font-normal text-neutral-500 w-48 font-mono text-[11px] uppercase tracking-wider">
                  Metric
                </th>
                {peerData.map((p) => {
                  const isCurrent = p.ticker === currentTicker
                  return (
                    <th
                      key={p.ticker}
                      className={`py-2.5 px-4 font-normal text-right ${
                        isCurrent
                          ? 'bg-neutral-100/60 dark:bg-neutral-900/60 font-semibold text-neutral-950 dark:text-white'
                          : 'text-neutral-700 dark:text-neutral-300'
                      }`}
                    >
                      <div className="flex flex-col items-end">
                        <button
                          onClick={() => onSelectTicker(p.ticker)}
                          className="hover:underline flex items-center gap-1 text-xs font-mono font-bold"
                        >
                          <span>{p.ticker}</span>
                          <ArrowRight className="w-3 h-3 text-neutral-400" />
                        </button>
                        <span className="text-[10px] text-neutral-400 font-sans font-normal truncate max-w-[130px]">
                          {p.companyName}
                        </span>
                      </div>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
              {/* Composite Score Row */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-900 dark:text-white font-medium font-sans">
                  Buffett Quality Score (0-100)
                </td>
                {peerData.map((p) => {
                  const s = p.ranked?.score || 0
                  const isLeader = s === topScore && topScore > 0
                  return (
                    <td key={p.ticker} className="py-2.5 px-4 text-right tabular-nums">
                      <span
                        className={`font-mono text-xs ${
                          isLeader
                            ? 'font-bold text-neutral-950 dark:text-white underline decoration-emerald-500 decoration-2 underline-offset-4'
                            : 'text-neutral-700 dark:text-neutral-300'
                        }`}
                      >
                        {s > 0 ? s.toFixed(1) : '—'}
                      </span>
                    </td>
                  )
                })}
              </tr>

              {/* Adjusted ROIC */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  Intangible-Adjusted ROIC
                </td>
                {peerData.map((p) => {
                  const roic = p.ranked?.has_roic ? p.ranked.roic : 0
                  const isLeader = roic === topROIC && topROIC > 0
                  return (
                    <td key={p.ticker} className="py-2.5 px-4 text-right tabular-nums">
                      <span
                        className={`font-mono text-xs ${
                          isLeader
                            ? 'text-emerald-600 dark:text-emerald-400 font-semibold'
                            : 'text-neutral-700 dark:text-neutral-300'
                        }`}
                      >
                        {p.ranked?.has_roic ? formatPercent(p.ranked.roic) : '—'}
                      </span>
                    </td>
                  )
                })}
              </tr>

              {/* FCF Margin */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  Free Cash Flow Margin
                </td>
                {peerData.map((p) => {
                  const m = p.ranked?.fcf_margin || 0
                  const isLeader = m === topFCFMargin && topFCFMargin > 0
                  return (
                    <td key={p.ticker} className="py-2.5 px-4 text-right tabular-nums">
                      <span
                        className={`font-mono text-xs ${
                          isLeader
                            ? 'text-emerald-600 dark:text-emerald-400 font-semibold'
                            : 'text-neutral-700 dark:text-neutral-300'
                        }`}
                      >
                        {p.ranked ? formatPercent(p.ranked.fcf_margin) : '—'}
                      </span>
                    </td>
                  )
                })}
              </tr>

              {/* 5-Year Revenue CAGR */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  Revenue 5-Year CAGR
                </td>
                {peerData.map((p) => {
                  const g = p.ranked?.revenue_cagr || 0
                  const isLeader = g === topRevCAGR && topRevCAGR > 0
                  return (
                    <td key={p.ticker} className="py-2.5 px-4 text-right tabular-nums">
                      <span
                        className={`font-mono text-xs ${
                          isLeader
                            ? 'text-neutral-900 dark:text-neutral-100 font-semibold'
                            : 'text-neutral-700 dark:text-neutral-300'
                        }`}
                      >
                        {p.ranked ? formatPercent(p.ranked.revenue_cagr) : '—'}
                      </span>
                    </td>
                  )
                })}
              </tr>

              {/* Operating Margin */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  GAAP Operating Margin
                </td>
                {peerData.map((p) => (
                  <td
                    key={p.ticker}
                    className="py-2.5 px-4 text-right tabular-nums font-mono text-xs text-neutral-700 dark:text-neutral-300"
                  >
                    {p.ranked && p.ranked.has_operating_margin
                      ? formatPercent(p.ranked.operating_margin)
                      : '—'}
                  </td>
                ))}
              </tr>

              {/* Share Count Reduction */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  Share Count Reduction (Buybacks)
                </td>
                {peerData.map((p) => {
                  const red = p.ranked?.has_share_trend ? p.ranked.share_reduction : 0
                  return (
                    <td key={p.ticker} className="py-2.5 px-4 text-right tabular-nums font-mono text-xs">
                      {p.ranked?.has_share_trend ? (
                        <span
                          className={
                            red > 0
                              ? 'text-emerald-600 dark:text-emerald-400 font-medium'
                              : 'text-neutral-500'
                          }
                        >
                          {red > 0 ? '+' : ''}
                          {formatPercent(red)}
                        </span>
                      ) : (
                        <span className="text-neutral-400">—</span>
                      )}
                    </td>
                  )
                })}
              </tr>

              {/* Annual Revenue */}
              <tr className="hover:bg-neutral-50/60 dark:hover:bg-neutral-900/30">
                <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 font-sans">
                  Annual Revenue (Trailing)
                </td>
                {peerData.map((p) => (
                  <td
                    key={p.ticker}
                    className="py-2.5 px-4 text-right tabular-nums font-mono text-xs text-neutral-700 dark:text-neutral-300"
                  >
                    {p.ranked ? formatCurrency(p.ranked.revenue, true, p.ranked.currency) : '—'}
                  </td>
                ))}
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
