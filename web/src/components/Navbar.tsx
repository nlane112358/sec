import React from 'react'
import { ArrowLeftRight, Command, Sun, Moon, Database, HelpCircle, Star, Sparkles, Search, Camera } from 'lucide-react'
import { DatabaseStats } from '../types'
import { RainbowMode } from './RainbowBackground'
import { prefetchOnHover } from '../lib/cache'

interface NavbarProps {
  stats: DatabaseStats | null
  currentTicker: string
  watchlist?: string[]
  recentTickers?: string[]
  onSelectTicker?: (ticker: string) => void
  onOpenCmdk: () => void
  onOpenShortcuts: () => void
  onOpenTearSheet?: () => void
  splitView?: boolean
  onToggleSplitView?: () => void
  darkMode: boolean
  onToggleTheme: () => void
  rainbowMode: RainbowMode
  onCycleRainbowMode: () => void
}

export const Navbar: React.FC<NavbarProps> = ({
  stats,
  currentTicker,
  watchlist = [],
  recentTickers = [],
  onSelectTicker,
  onOpenCmdk,
  onOpenShortcuts,
  onOpenTearSheet,
  splitView = false,
  onToggleSplitView,
  darkMode,
  onToggleTheme,
  rainbowMode,
  onCycleRainbowMode,
}) => {
  // Visual pulse highlight when numbers automatically update
  const [statsUpdated, setStatsUpdated] = React.useState(false)
  const prevStatsRef = React.useRef<{ companies: number; facts: number } | null>(null)

  React.useEffect(() => {
    if (!stats) return
    if (prevStatsRef.current) {
      if (
        prevStatsRef.current.companies !== stats.total_companies ||
        prevStatsRef.current.facts !== stats.total_facts
      ) {
        setStatsUpdated(true)
        const timer = setTimeout(() => setStatsUpdated(false), 2000)
        return () => clearTimeout(timer)
      }
    }
    prevStatsRef.current = {
      companies: stats.total_companies,
      facts: stats.total_facts,
    }
  }, [stats?.total_companies, stats?.total_facts])

  return (
    <header className="sticky top-0 z-40 w-full border-b border-neutral-200/80 dark:border-white/10 bg-white/75 dark:bg-[#030611]/60 backdrop-blur-xl transition-colors duration-150">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 h-13 flex items-center justify-between text-xs">
        {/* Brand / Logo & Quick Watchlist */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
            <span className="font-sans font-medium tracking-tight text-neutral-900 dark:text-white text-sm">
              sec<span className="text-neutral-400 font-mono">.edgar</span>
            </span>
          </div>

          <div
            className="hidden lg:flex items-center gap-2 text-neutral-500 font-mono text-[11px] pl-3 border-l border-neutral-200 dark:border-neutral-800"
            title="Live SQLite database statistics: automatically updates in real time as facts and filings are synced"
          >
            <div className="relative flex items-center justify-center">
              <Database className="w-3 h-3 text-neutral-400" />
              <span className="absolute -top-0.5 -right-0.5 flex h-1.5 w-1.5">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500" />
              </span>
            </div>
            <span
              className={`transition-colors duration-500 ${
                statsUpdated
                  ? 'text-emerald-600 dark:text-emerald-400 font-semibold'
                  : ''
              }`}
            >
              {stats
                ? `${stats.total_companies.toLocaleString()} companies • ${(stats.total_facts / 1e6).toFixed(1)}M facts`
                : 'connecting...'}
            </span>
          </div>

          {/* Quick Watchlist / Recents Bar */}
          {watchlist.length > 0 && onSelectTicker && (
            <div className="hidden xl:flex items-center gap-1 pl-3 border-l border-neutral-200 dark:border-neutral-800 font-mono text-[11px]">
              <Star className="w-3 h-3 text-amber-500 fill-amber-500 mr-1" />
              {watchlist.slice(0, 5).map((t) => (
                <button
                  key={t}
                  onClick={() => onSelectTicker(t)}
                  {...prefetchOnHover(t)}
                  className={`px-1.5 py-0.5 transition-colors ${
                    t === currentTicker
                      ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                      : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-200 hover:bg-neutral-100 dark:hover:bg-neutral-900'
                  }`}
                >
                  {t}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Center: cmdk trigger */}
        <button
          onClick={onOpenCmdk}
          className="flex items-center gap-2.5 px-3 py-1.5 rounded-lg border border-neutral-300 dark:border-neutral-800 bg-neutral-100/60 dark:bg-neutral-900/60 hover:bg-white dark:hover:bg-neutral-900 hover:border-neutral-400 dark:hover:border-neutral-700 text-neutral-500 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-neutral-200 transition-all text-xs font-sans group w-44 sm:w-64 md:w-80 justify-between shadow-2xs"
          title="Fuzzy search ticker, name, or run actions (⌘K or /)"
        >
          <div className="flex items-center gap-2 truncate">
            <Search className="w-3.5 h-3.5 text-neutral-400 group-hover:text-neutral-700 dark:group-hover:text-neutral-200 shrink-0" />
            <span className="truncate">
              Search {stats ? `${stats.total_companies.toLocaleString()} companies` : 'companies'} or type /...
            </span>
          </div>
          <kbd className="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] text-neutral-500 dark:text-neutral-400 bg-white dark:bg-neutral-800 border border-neutral-200 dark:border-neutral-700 rounded font-mono shrink-0">
            <Command className="w-2.5 h-2.5" />
            <span>K</span>
          </kbd>
        </button>

        {/* Right Actions */}
        <div className="flex items-center gap-2 text-neutral-500">
          {onToggleSplitView && (
            <button
              onClick={onToggleSplitView}
              aria-pressed={splitView}
              className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1.5 transition-colors ${
                splitView
                  ? 'border-indigo-500/30 bg-indigo-500/10 text-indigo-600 dark:text-indigo-300'
                  : 'border-transparent hover:border-neutral-200 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:border-white/10 dark:hover:bg-neutral-900 dark:hover:text-white'
              }`}
              title={`${splitView ? 'Exit' : 'Open'} split view (⌘ + \\)`}
            >
              <ArrowLeftRight className="h-4 w-4" />
              <span className="hidden xl:inline text-[10px] font-mono">Split</span>
            </button>
          )}
          {onOpenTearSheet && (
            <button
              onClick={onOpenTearSheet}
              className="p-1.5 rounded-md hover:text-neutral-900 dark:hover:text-neutral-100 hover:bg-neutral-100 dark:hover:bg-neutral-900 transition-colors"
              title="Export Tear Sheet Card PNG (⌘+Shift+S)"
            >
              <Camera className="w-4 h-4" />
            </button>
          )}

          <button
            onClick={onOpenShortcuts}
            className="p-1.5 rounded-md hover:text-neutral-900 dark:hover:text-neutral-100 hover:bg-neutral-100 dark:hover:bg-neutral-900 transition-colors"
            title="Keyboard Shortcuts (?)"
          >
            <HelpCircle className="w-4 h-4" />
          </button>

          <button
            onClick={onToggleTheme}
            className="p-1.5 rounded-md hover:text-neutral-900 dark:hover:text-neutral-100 hover:bg-neutral-100 dark:hover:bg-neutral-900 transition-colors"
            title="Toggle theme (t)"
          >
            {darkMode ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
          </button>

          {/* Rainbow Aurora Ambient Background Cycle Button */}
          <button
            onClick={onCycleRainbowMode}
            className={`p-1.5 rounded-md transition-all flex items-center gap-1.5 text-[11px] font-mono ${
              rainbowMode !== 'off'
                ? 'text-indigo-600 dark:text-indigo-300 bg-indigo-500/10 dark:bg-indigo-500/15 hover:bg-indigo-500/20 border border-indigo-500/30'
                : 'hover:text-neutral-900 dark:hover:text-neutral-100 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-400 border border-transparent'
            }`}
            title={`Rainbow Aurora Effect: ${rainbowMode.toUpperCase()} (Click to cycle: Aurora → Vibrant → Subtle → Off, or press 'b')`}
          >
            <Sparkles
              className={`w-3.5 h-3.5 ${
                rainbowMode !== 'off'
                  ? 'text-amber-500 dark:text-amber-300 animate-pulse'
                  : 'text-neutral-400'
              }`}
            />
            <span className="hidden md:inline capitalize text-[10px] tracking-wide">{rainbowMode}</span>
          </button>

          <div className="hidden sm:flex items-center pl-2 border-l border-neutral-200 dark:border-neutral-800">
            <span className="font-mono text-neutral-700 dark:text-neutral-300 bg-neutral-100 dark:bg-neutral-900 px-2 py-0.5 rounded text-[11px] border border-neutral-200 dark:border-neutral-800 font-medium">
              {currentTicker}
            </span>
          </div>
        </div>
      </div>
    </header>
  )
}
