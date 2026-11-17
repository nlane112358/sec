import React, { useEffect, useState, useMemo } from 'react'
import { Command } from 'cmdk'
import { Search, Building, Sun, Moon, DollarSign, FileText, Layers, Percent, X, GitCompare, Sparkles, Camera } from 'lucide-react'
import { Company, DatabaseStats } from '../types'
import { TabKey } from './Tabs'
import { RainbowMode } from './RainbowBackground'
import { prefetchSummary } from '../lib/cache'

interface CommandMenuProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  companies: Company[]
  stats?: DatabaseStats | null
  currentTicker: string
  onSelectTicker: (ticker: string) => void
  activeTab: string
  onSelectTab: (tab: TabKey) => void
  darkMode: boolean
  onToggleTheme: () => void
  onToggleCompact: () => void
  rainbowMode: RainbowMode
  onCycleRainbowMode: () => void
  onOpenTearSheet?: () => void
}

const POPULAR_TICKERS = [
  'AAPL', 'NVDA', 'MSFT', 'GOOGL', 'AMZN', 'META', 'TSLA', 'BRK-B',
  'JPM', 'V', 'WMT', 'UNH', 'MA', 'COST', 'HD', 'PG', 'AMD', 'NFLX',
  'DIS', 'CRM', 'ORCL', 'ADBE', 'INTC', 'CSCO', 'QCOM', 'TXN', 'SNOW', 'NET'
]

export const CommandMenu: React.FC<CommandMenuProps> = ({
  open,
  onOpenChange,
  companies,
  stats,
  currentTicker: _currentTicker,
  onSelectTicker,
  activeTab: _activeTab,
  onSelectTab,
  darkMode,
  onToggleTheme,
  onToggleCompact,
  rainbowMode,
  onCycleRainbowMode,
  onOpenTearSheet,
}) => {
  const [search, setSearch] = useState('')

  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        onOpenChange(!open)
      }
      if (e.key === 'Escape' && open) {
        onOpenChange(false)
      }
    }

    document.addEventListener('keydown', down)
    return () => document.removeEventListener('keydown', down)
  }, [open, onOpenChange])

  useEffect(() => {
    if (!open) {
      setSearch('')
    }
  }, [open])

  // When query is empty, provide a generous curated list of top compounders followed by alphabetical companies
  const defaultCompanies = useMemo(() => {
    const popularSet = new Set(POPULAR_TICKERS)
    const popular: Company[] = []
    const others: Company[] = []

    for (const t of POPULAR_TICKERS) {
      const match = companies.find((c) => c.ticker === t)
      if (match) popular.push(match)
    }

    for (let i = 0; i < companies.length; i++) {
      const c = companies[i]
      if (!popularSet.has(c.ticker)) {
        others.push(c)
        if (popular.length + others.length >= 60) break
      }
    }

    return [...popular, ...others]
  }, [companies])

  // Relevance ranking algorithm across all listed companies
  const { filteredCompanies, matchingTotal } = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) {
      return {
        filteredCompanies: defaultCompanies,
        matchingTotal: companies.length,
      }
    }

    const scored: { company: Company; score: number }[] = []

    for (let i = 0; i < companies.length; i++) {
      const c = companies[i]
      const tickerLower = c.ticker.toLowerCase()
      const nameLower = c.name ? c.name.toLowerCase() : ''
      const sicLower = c.sic_description ? c.sic_description.toLowerCase() : ''
      const cikStr = c.cik || ''

      let score = 0

      if (tickerLower === q) {
        // Exact ticker match gets top priority
        score = 10000
      } else if (tickerLower.startsWith(q)) {
        // Prefix ticker match (shorter tickers like "C" or "AA" rank before longer ones)
        score = 5000 - (tickerLower.length - q.length) * 10
      } else if (nameLower.startsWith(q)) {
        score = 3000
      } else if (tickerLower.includes(q)) {
        score = 2000
      } else if (nameLower.includes(q)) {
        const words = nameLower.split(/\s+/)
        if (words.some((w) => w.startsWith(q))) {
          score = 1500
        } else {
          score = 1000
        }
      } else if (sicLower.includes(q)) {
        score = 500
      } else if (cikStr.includes(q)) {
        score = 250
      }

      if (score > 0) {
        scored.push({ company: c, score })
      }
    }

    scored.sort((a, b) => b.score - a.score)

    return {
      filteredCompanies: scored.slice(0, 100).map((s) => s.company),
      matchingTotal: scored.length,
    }
  }, [companies, search, defaultCompanies])

  const q = search.trim().toLowerCase()

  // Views & Models commands
  const views = useMemo(
    () => [
      {
        id: 'tearsheet',
        title: 'Export Tear Sheet Card (2X Retina PNG)',
        shortcut: '⌘⇧S',
        icon: Camera,
        keywords: 'tear sheet export image png card share twitter substack slack ⌘⇧s',
        onSelect: () => {
          onOpenTearSheet?.()
          onOpenChange(false)
        },
        show: Boolean(onOpenTearSheet),
      },
      {
        id: 'statements',
        title: 'Financial Statements (5-Year GAAP & SBC)',
        shortcut: '1',
        icon: FileText,
        keywords: 'statements financials balance sheet income cash flow gaap sbc 1',
        onSelect: () => {
          onSelectTab('statements')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'dcf',
        title: 'Discounted Cash Flow (Interactive DCF)',
        shortcut: '2',
        icon: DollarSign,
        keywords: 'dcf valuation intrinsic value sensitivity wacc discount cash flow 2',
        onSelect: () => {
          onSelectTab('dcf')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'intangibles',
        title: 'Intangible Capital & Modern ROIC',
        shortcut: '3',
        icon: Layers,
        keywords: 'intangibles roic peters taylor damodaran r&d capitalization 3',
        onSelect: () => {
          onSelectTab('intangibles')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'filings',
        title: 'SEC EDGAR Filings & Primary Documents',
        shortcut: '4',
        icon: Building,
        keywords: 'filings sec edgar 10-k 10-q 8-k documents reports 4',
        onSelect: () => {
          onSelectTab('filings')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'screener',
        title: `Universe Screener & Quality-at-a-Discount (${stats?.total_companies ? stats.total_companies.toLocaleString() : companies.length || 'Universe'} Companies)`,
        shortcut: '5',
        icon: Percent,
        keywords: 'screener universe compounders rank discount qaad quality margin of safety 5',
        onSelect: () => {
          onSelectTab('screener')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'owner-yield',
        title: 'Rank Companies by Owner Yield (Buffett & Damodaran)',
        shortcut: 'Y',
        icon: Percent,
        keywords: 'owner yield rank ranking buffett earnings cash flow sbc',
        onSelect: () => {
          window.location.hash = `#${_currentTicker}/screener?mode=owner-yield`
          onSelectTab('screener')
          onOpenChange(false)
        },
        show: true,
      },
      {
        id: 'compare',
        title: 'Peer Benchmarking & Competitor Comparison',
        shortcut: '6',
        icon: GitCompare,
        keywords: 'compare peers peer benchmarking competitors 6',
        onSelect: () => {
          onSelectTab('compare')
          onOpenChange(false)
        },
        show: true,
      },
    ],
    [onOpenTearSheet, onSelectTab, onOpenChange, stats?.total_companies, companies.length]
  )

  // Actions
  const actions = useMemo(
    () => [
      {
        id: 'theme',
        title: `Toggle Theme (${darkMode ? 'Light' : 'Dark'})`,
        shortcut: 't',
        icon: darkMode ? Sun : Moon,
        keywords: 'toggle theme light dark mode t',
        onSelect: () => {
          onToggleTheme()
          onOpenChange(false)
        },
      },
      {
        id: 'compact',
        title: 'Toggle Number Precision ($B/$M vs Full)',
        shortcut: 'c',
        icon: DollarSign,
        keywords: 'toggle compact currency format full numbers c',
        onSelect: () => {
          onToggleCompact()
          onOpenChange(false)
        },
      },
      {
        id: 'rainbow',
        title: `Cycle Rainbow Background (${rainbowMode})`,
        shortcut: 'b',
        icon: Sparkles,
        keywords: 'toggle rainbow background aurora changing colors ambient glow b',
        onSelect: () => {
          onCycleRainbowMode()
          onOpenChange(false)
        },
      },
    ],
    [darkMode, onToggleTheme, onToggleCompact, rainbowMode, onCycleRainbowMode, onOpenChange]
  )

  const filteredViews = useMemo(() => {
    if (!q) return views.filter((v) => v.show)
    return views.filter((v) => v.show && (v.title.toLowerCase().includes(q) || v.keywords.includes(q)))
  }, [views, q])

  const filteredActions = useMemo(() => {
    if (!q) return actions
    return actions.filter((a) => a.title.toLowerCase().includes(q) || a.keywords.includes(q))
  }, [actions, q])

  if (!open) return null

  const totalCount = stats?.total_companies || companies.length

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] px-4 theme-backdrop backdrop-blur-sm transition-all duration-150">
      <div className="w-full max-w-xl shadow-2xl relative animate-in fade-in zoom-in-95 duration-100">
        <Command
          shouldFilter={false}
          className="border border-neutral-700 dark:border-neutral-800 bg-neutral-950/98 text-neutral-100 overflow-hidden shadow-2xl backdrop-blur-xl font-mono"
          loop
        >
          <div className="flex items-center px-4 border-b border-neutral-800/80">
            <Search className="w-4 h-4 text-neutral-400 mr-3 shrink-0" />
            <Command.Input
              value={search}
              onValueChange={setSearch}
              placeholder={`Search ${totalCount ? totalCount.toLocaleString() : 'all'} companies, sectors, or actions...`}
              className="w-full bg-transparent py-3.5 text-sm outline-none placeholder:text-neutral-500 text-neutral-100"
              autoFocus
            />
            <button
              onClick={() => onOpenChange(false)}
              className="text-neutral-500 hover:text-neutral-300 p-1"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          <Command.List className="max-h-80 overflow-y-auto p-2 scroll-py-2">
            {filteredCompanies.length === 0 && filteredViews.length === 0 && filteredActions.length === 0 && (
              <Command.Empty className="py-6 text-center text-xs text-neutral-500 font-mono">
                No matching tickers, companies, or commands found for "{search}".
              </Command.Empty>
            )}

            {filteredCompanies.length > 0 && (
              <Command.Group
                heading={
                  search.trim()
                    ? `Companies (${filteredCompanies.length}${matchingTotal > filteredCompanies.length ? ` of ${matchingTotal.toLocaleString()} matches` : ' matches'})`
                    : `Companies (${totalCount.toLocaleString()} listings)`
                }
                className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5"
              >
                {filteredCompanies.map((c) => (
                  <Command.Item
                    key={c.ticker}
                    value={c.ticker}
                    onMouseEnter={() => prefetchSummary(c.ticker)}
                    onSelect={() => {
                      onSelectTicker(c.ticker)
                      onOpenChange(false)
                    }}
                    className="flex items-center justify-between px-3 py-2 rounded-lg text-sm text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer transition-colors"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-mono font-medium text-white px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-800 text-xs shrink-0">
                        {c.ticker}
                      </span>
                      <span className="truncate text-xs text-neutral-300 font-sans">
                        {c.name || c.ticker}
                      </span>
                    </div>
                    <span className="text-[11px] text-neutral-500 truncate max-w-[160px] font-mono shrink-0 ml-2">
                      {c.sic_description || (c.sic ? `SIC ${c.sic}` : `CIK ${c.cik}`)}
                    </span>
                  </Command.Item>
                ))}
                {matchingTotal > filteredCompanies.length && (
                  <div className="px-3 py-2 text-[10px] text-neutral-500 font-mono text-center border-t border-neutral-800/40 mt-1">
                    +{matchingTotal - filteredCompanies.length} more matching companies (type to refine)
                  </div>
                )}
              </Command.Group>
            )}

            {filteredViews.length > 0 && (
              <Command.Group
                heading="Views & Models"
                className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5 mt-2"
              >
                {filteredViews.map((v) => {
                  const Icon = v.icon
                  return (
                    <Command.Item
                      key={v.id}
                      value={v.id}
                      onSelect={v.onSelect}
                      className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
                    >
                      <Icon className="w-3.5 h-3.5 text-neutral-400" />
                      <span>{v.title}</span>
                      <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                        {v.shortcut}
                      </kbd>
                    </Command.Item>
                  )
                })}
              </Command.Group>
            )}

            {filteredActions.length > 0 && (
              <Command.Group
                heading="Actions"
                className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5 mt-2"
              >
                {filteredActions.map((a) => {
                  const Icon = a.icon
                  return (
                    <Command.Item
                      key={a.id}
                      value={a.id}
                      onSelect={a.onSelect}
                      className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
                    >
                      <Icon className="w-3.5 h-3.5 text-neutral-400" />
                      <span>{a.title}</span>
                      <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                        {a.shortcut}
                      </kbd>
                    </Command.Item>
                  )
                })}
              </Command.Group>
            )}
          </Command.List>

          <div className="flex items-center justify-between px-4 py-2 border-t border-neutral-800/80 bg-neutral-950/60 text-[11px] text-neutral-500 font-mono">
            <span>Navigation: ↑ ↓ Enter</span>
            <span>SEC EDGAR Master Directory ({totalCount.toLocaleString()} listings)</span>
          </div>
        </Command>
      </div>
    </div>
  )
}
