import React, { useEffect, useState } from 'react'
import { Command } from 'cmdk'
import { Search, Building, ArrowRight, Sun, Moon, DollarSign, FileText, Layers, Percent, X, GitCompare, Sparkles } from 'lucide-react'
import { Company } from '../types'
import { TabKey } from './Tabs'
import { RainbowMode } from './RainbowBackground'

interface CommandMenuProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  companies: Company[]
  currentTicker: string
  onSelectTicker: (ticker: string) => void
  activeTab: string
  onSelectTab: (tab: TabKey) => void
  darkMode: boolean
  onToggleTheme: () => void
  onToggleCompact: () => void
  rainbowMode: RainbowMode
  onCycleRainbowMode: () => void
}

export const CommandMenu: React.FC<CommandMenuProps> = ({
  open,
  onOpenChange,
  companies,
  currentTicker,
  onSelectTicker,
  activeTab,
  onSelectTab,
  darkMode,
  onToggleTheme,
  onToggleCompact,
  rainbowMode,
  onCycleRainbowMode,
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

  if (!open) return null

  // Filter companies client-side for instant 60fps fuzzy matching
  const filteredCompanies = search.trim()
    ? companies.filter((c) => {
        const query = search.toLowerCase()
        return (
          c.ticker.toLowerCase().includes(query) ||
          c.name.toLowerCase().includes(query) ||
          c.sic_description.toLowerCase().includes(query)
        )
      }).slice(0, 10)
    : companies.slice(0, 8)

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] px-4 theme-backdrop backdrop-blur-sm transition-all duration-150">
      <div className="w-full max-w-xl shadow-2xl relative animate-in fade-in zoom-in-95 duration-100">
        <Command
          className="border border-neutral-700 dark:border-neutral-800 bg-neutral-950/98 text-neutral-100 overflow-hidden shadow-2xl backdrop-blur-xl font-mono"
          loop
        >
          <div className="flex items-center px-4 border-b border-neutral-800/80">
            <Search className="w-4 h-4 text-neutral-400 mr-3 shrink-0" />
            <Command.Input
              value={search}
              onValueChange={setSearch}
              placeholder="Search 528 companies, sectors, or actions..."
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
            <Command.Empty className="py-6 text-center text-xs text-neutral-500 font-mono">
              No matching tickers or commands found.
            </Command.Empty>

            {filteredCompanies.length > 0 && (
              <Command.Group
                heading="Companies"
                className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5"
              >
                {filteredCompanies.map((c) => (
                  <Command.Item
                    key={c.ticker}
                    value={`${c.ticker} ${c.name} ${c.sic_description}`}
                    onSelect={() => {
                      onSelectTicker(c.ticker)
                      onOpenChange(false)
                    }}
                    className="flex items-center justify-between px-3 py-2 rounded-lg text-sm text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer transition-colors"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-mono font-medium text-white px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-800 text-xs">
                        {c.ticker}
                      </span>
                      <span className="truncate text-xs text-neutral-300 font-sans">
                        {c.name}
                      </span>
                    </div>
                    <span className="text-[11px] text-neutral-500 truncate max-w-[140px] font-mono">
                      {c.sic_description || `SIC ${c.sic}`}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            <Command.Group
              heading="Views & Models"
              className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5 mt-2"
            >
              <Command.Item
                value="statements financials balance sheet income cash flow"
                onSelect={() => {
                  onSelectTab('statements')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <FileText className="w-3.5 h-3.5 text-neutral-400" />
                <span>Financial Statements (5-Year GAAP & SBC)</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  1
                </kbd>
              </Command.Item>

              <Command.Item
                value="dcf valuation intrinsic value sensitivity wacc discount cash flow"
                onSelect={() => {
                  onSelectTab('dcf')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <DollarSign className="w-3.5 h-3.5 text-neutral-400" />
                <span>Discounted Cash Flow (Interactive DCF)</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  2
                </kbd>
              </Command.Item>

              <Command.Item
                value="intangibles roic peters taylor damodaran r&d capitalization"
                onSelect={() => {
                  onSelectTab('intangibles')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <Layers className="w-3.5 h-3.5 text-neutral-400" />
                <span>Intangible Capital & Modern ROIC</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  3
                </kbd>
              </Command.Item>

              <Command.Item
                value="filings sec edgar 10-k 10-q 8-k documents reports"
                onSelect={() => {
                  onSelectTab('filings')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <Building className="w-3.5 h-3.5 text-neutral-400" />
                <span>SEC EDGAR Filings & Primary Documents</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  4
                </kbd>
              </Command.Item>

              <Command.Item
                value="screener universe compounders rank"
                onSelect={() => {
                  onSelectTab('screener')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <Percent className="w-3.5 h-3.5 text-neutral-400" />
                <span>Universe Screener (528 Companies)</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  5
                </kbd>
              </Command.Item>

              <Command.Item
                value="compare peers peer benchmarking competitors"
                onSelect={() => {
                  onSelectTab('compare')
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <GitCompare className="w-3.5 h-3.5 text-neutral-400" />
                <span>Peer Benchmarking & Competitor Comparison</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  6
                </kbd>
              </Command.Item>
            </Command.Group>

            <Command.Group
              heading="Actions"
              className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 px-2 py-1.5 mt-2"
            >
              <Command.Item
                value="toggle theme light dark mode"
                onSelect={() => {
                  onToggleTheme()
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                {darkMode ? <Sun className="w-3.5 h-3.5" /> : <Moon className="w-3.5 h-3.5" />}
                <span>Toggle Theme ({darkMode ? 'Light' : 'Dark'})</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  t
                </kbd>
              </Command.Item>

              <Command.Item
                value="toggle compact currency format full numbers"
                onSelect={() => {
                  onToggleCompact()
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <DollarSign className="w-3.5 h-3.5" />
                <span>Toggle Number Precision ($B/$M vs Full)</span>
              </Command.Item>

              <Command.Item
                value="toggle rainbow background aurora changing colors ambient glow"
                onSelect={() => {
                  onCycleRainbowMode()
                  onOpenChange(false)
                }}
                className="flex items-center gap-3 px-3 py-2 rounded-lg text-xs text-neutral-300 hover:text-white hover:bg-neutral-900 cursor-pointer"
              >
                <Sparkles className="w-3.5 h-3.5 text-amber-400" />
                <span>Cycle Rainbow Background ({rainbowMode})</span>
                <kbd className="ml-auto text-[10px] font-mono text-neutral-500 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                  b
                </kbd>
              </Command.Item>
            </Command.Group>
          </Command.List>

          <div className="flex items-center justify-between px-4 py-2 border-t border-neutral-800/80 bg-neutral-950/60 text-[11px] text-neutral-500 font-mono">
            <span>Navigation: ↑ ↓ Enter</span>
            <span>SEC EDGAR Database</span>
          </div>
        </Command>
      </div>
    </div>
  )
}
