import React, { useState, useEffect, useCallback } from 'react'
import { Navbar } from './components/Navbar'
import { CommandMenu } from './components/CommandMenu'
import { CompanyHeader } from './components/CompanyHeader'
import { Tabs, TabKey } from './components/Tabs'
import { FinancialStatement } from './components/FinancialStatement'
import { DCFModeler } from './components/DCFModeler'
import { IntangiblesView } from './components/IntangiblesView'
import { FilingsBrowser } from './components/FilingsBrowser'
import { UniverseScreener } from './components/UniverseScreener'
import { PeerComparison } from './components/PeerComparison'
import { ShortcutsModal } from './components/ShortcutsModal'
import { RainbowBackground, RainbowMode } from './components/RainbowBackground'
import { Company, SummaryResponse, DatabaseStats } from './types'

const VALID_TABS: TabKey[] = ['statements', 'dcf', 'intangibles', 'filings', 'screener', 'compare']

const parseHashRoute = (): { ticker: string; tab: TabKey } => {
  const raw = window.location.hash.replace(/^#\/?/, '')
  const [routePart] = raw.split('?')
  const parts = routePart.split('/').filter(Boolean)
  let ticker = 'AAPL'
  let tab: TabKey = 'statements'

  if (parts.length === 1) {
    if (VALID_TABS.includes(parts[0].toLowerCase() as TabKey)) {
      tab = parts[0].toLowerCase() as TabKey
    } else if (parts[0].length <= 6) {
      ticker = parts[0].toUpperCase()
    }
  } else if (parts.length >= 2) {
    if (VALID_TABS.includes(parts[0].toLowerCase() as TabKey)) {
      tab = parts[0].toLowerCase() as TabKey
      ticker = parts[1].toUpperCase()
    } else {
      ticker = parts[0].toUpperCase()
      if (VALID_TABS.includes(parts[1].toLowerCase() as TabKey)) {
        tab = parts[1].toLowerCase() as TabKey
      }
    }
  }
  return { ticker, tab }
}

// In-memory LRU cache for 0ms transitions
const summaryCache = new Map<string, SummaryResponse>()

export const App: React.FC = () => {
  const [stats, setStats] = useState<DatabaseStats | null>(null)
  const [companies, setCompanies] = useState<Company[]>([])
  
  const initialRoute = parseHashRoute()
  const [currentTicker, setCurrentTicker] = useState<string>(initialRoute.ticker)
  const [activeTab, setActiveTab] = useState<TabKey>(initialRoute.tab)
  const [cadence, setCadence] = useState<'annual' | 'quarterly' | 'ttm'>('annual')

  const [summary, setSummary] = useState<SummaryResponse | null>(() => summaryCache.get(`${initialRoute.ticker}_annual`) || null)
  const [loadingSummary, setLoadingSummary] = useState<boolean>(!summaryCache.has(`${initialRoute.ticker}_annual`))
  const [summaryError, setSummaryError] = useState<string | null>(null)

  const [compact, setCompact] = useState<boolean>(true)
  const [cmdkOpen, setCmdkOpen] = useState<boolean>(false)
  const [shortcutsOpen, setShortcutsOpen] = useState<boolean>(false)

  // Watchlist & Recents persisted to localStorage
  const [watchlist, setWatchlist] = useState<string[]>(() => {
    try {
      const saved = localStorage.getItem('sec_watchlist')
      return saved ? JSON.parse(saved) : ['AAPL', 'MSFT', 'GOOGL', 'NVDA']
    } catch {
      return ['AAPL', 'MSFT', 'GOOGL', 'NVDA']
    }
  })

  const [recentTickers, setRecentTickers] = useState<string[]>(() => {
    try {
      const saved = localStorage.getItem('sec_recent')
      return saved ? JSON.parse(saved) : ['AAPL']
    } catch {
      return ['AAPL']
    }
  })

  const toggleWatchlist = (ticker: string) => {
    setWatchlist((prev) => {
      const next = prev.includes(ticker)
        ? prev.filter((t) => t !== ticker)
        : [...prev, ticker]
      try {
        localStorage.setItem('sec_watchlist', JSON.stringify(next))
      } catch {}
      return next
    })
  }

  // Update recents when ticker changes
  useEffect(() => {
    setRecentTickers((prev) => {
      const filtered = prev.filter((t) => t !== currentTicker)
      const next = [currentTicker, ...filtered].slice(0, 6)
      try {
        localStorage.setItem('sec_recent', JSON.stringify(next))
      } catch {}
      return next
    })
  }, [currentTicker])

  // Theme preference
  const [darkMode, setDarkMode] = useState<boolean>(() => {
    const saved = localStorage.getItem('sec_theme')
    if (saved) return saved === 'dark'
    return false
  })

  // Google Gemini Changing Rainbow Aurora Ambient Background Effect
  const [rainbowMode, setRainbowMode] = useState<RainbowMode>(() => {
    try {
      const saved = localStorage.getItem('sec_rainbow_mode')
      if (saved && ['aurora', 'vibrant', 'subtle', 'off'].includes(saved)) {
        return saved as RainbowMode
      }
    } catch {}
    return 'aurora'
  })

  const handleCycleRainbowMode = useCallback(() => {
    setRainbowMode((curr) => {
      const next: RainbowMode =
        curr === 'aurora'
          ? 'vibrant'
          : curr === 'vibrant'
          ? 'subtle'
          : curr === 'subtle'
          ? 'off'
          : 'aurora'
      try {
        localStorage.setItem('sec_rainbow_mode', next)
      } catch {}
      return next
    })
  }, [])

  useEffect(() => {
    const root = document.documentElement
    if (darkMode) {
      root.classList.add('dark')
      root.classList.remove('light')
      localStorage.setItem('sec_theme', 'dark')
    } else {
      root.classList.remove('dark')
      root.classList.add('light')
      localStorage.setItem('sec_theme', 'light')
    }
  }, [darkMode])

  // Fetch initial stats and companies
  useEffect(() => {
    fetch('/api/stats')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data) setStats(data)
      })
      .catch(() => {})

    fetch('/api/companies')
      .then((res) => (res.ok ? res.json() : []))
      .then((data: Company[]) => {
        setCompanies(data)
      })
      .catch(() => {})
  }, [])

  // Keep URL hash in sync with ticker and tab
  useEffect(() => {
    const targetHash = `#${currentTicker}/${activeTab}`
    if (window.location.hash !== targetHash) {
      window.history.replaceState(null, '', targetHash)
    }
  }, [currentTicker, activeTab])

  // Listen for browser back/forward hash navigation
  useEffect(() => {
    const handleHashChange = () => {
      const { ticker, tab } = parseHashRoute()
      if (ticker && ticker !== currentTicker) {
        setCurrentTicker(ticker)
      }
      if (tab && tab !== activeTab) {
        setActiveTab(tab)
      }
    }

    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [currentTicker, activeTab])

  // Fetch company summary when ticker or statement cadence changes (with in-memory cache)
  useEffect(() => {
    let ignore = false
    const cacheKey = `${currentTicker}_${cadence}`

    if (summaryCache.has(cacheKey)) {
      setSummary(summaryCache.get(cacheKey)!)
      setLoadingSummary(false)
      setSummaryError(null)
    } else {
      setLoadingSummary(true)
      setSummaryError(null)
    }

    const url =
      cadence === 'quarterly'
        ? `/api/company/${currentTicker}/summary?view=quarterly&quarters=8`
        : cadence === 'ttm'
        ? `/api/company/${currentTicker}/summary?view=ttm`
        : `/api/company/${currentTicker}/summary?view=annual&years=5`

    fetch(url)
      .then((res) => {
        if (!res.ok) throw new Error(`Company ${currentTicker} (${cadence}) not found or failed to load`)
        return res.json()
      })
      .then((data: SummaryResponse) => {
        if (!ignore) {
          summaryCache.set(cacheKey, data)
          setSummary(data)
        }
      })
      .catch((err) => {
        if (!ignore && !summaryCache.has(cacheKey)) {
          setSummaryError(err.message)
        }
      })
      .finally(() => {
        if (!ignore) setLoadingSummary(false)
      })

    return () => {
      ignore = true
    }
  }, [currentTicker, cadence])

  // Navigation handlers
  const handlePrevTicker = useCallback(() => {
    if (companies.length === 0) return
    const idx = companies.findIndex((c) => c.ticker === currentTicker)
    const prevIdx = idx > 0 ? idx - 1 : companies.length - 1
    setCurrentTicker(companies[prevIdx].ticker)
  }, [companies, currentTicker])

  const handleNextTicker = useCallback(() => {
    if (companies.length === 0) return
    const idx = companies.findIndex((c) => c.ticker === currentTicker)
    const nextIdx = idx < companies.length - 1 ? idx + 1 : 0
    setCurrentTicker(companies[nextIdx].ticker)
  }, [companies, currentTicker])

  // Global keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT'
      ) {
        return
      }

      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setCmdkOpen((prev) => !prev)
        return
      }

      if (e.key === '/') {
        e.preventDefault()
        setCmdkOpen(true)
        return
      }

      if (e.key === '1') {
        e.preventDefault()
        setActiveTab('statements')
      } else if (e.key === '2') {
        e.preventDefault()
        setActiveTab('dcf')
      } else if (e.key === '3') {
        e.preventDefault()
        setActiveTab('intangibles')
      } else if (e.key === '4') {
        e.preventDefault()
        setActiveTab('filings')
      } else if (e.key === '5') {
        e.preventDefault()
        setActiveTab('screener')
      } else if (e.key === '6') {
        e.preventDefault()
        setActiveTab('compare')
      } else if (e.key === '[') {
        e.preventDefault()
        handlePrevTicker()
      } else if (e.key === ']') {
        e.preventDefault()
        handleNextTicker()
      } else if (e.key === 't' || e.key === 'T') {
        e.preventDefault()
        setDarkMode((prev) => !prev)
      } else if (e.key === 'b' || e.key === 'B') {
        e.preventDefault()
        handleCycleRainbowMode()
      } else if (e.key === '?') {
        e.preventDefault()
        setShortcutsOpen((prev) => !prev)
      } else if (e.key === 'c') {
        e.preventDefault()
        setCompact((prev) => !prev)
      } else if (e.key === 'Escape') {
        setCmdkOpen(false)
        setShortcutsOpen(false)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handlePrevTicker, handleNextTicker, handleCycleRainbowMode])

  return (
    <div className="min-h-screen relative text-neutral-900 dark:text-neutral-100 flex flex-col font-sans selection:bg-neutral-200 dark:selection:bg-neutral-800 transition-colors duration-150">
      {/* Google Gemini Changing Rainbow Aurora Ambient Background */}
      <RainbowBackground mode={rainbowMode} darkMode={darkMode} />

      {/* Top Navigation */}
      <Navbar
        stats={stats}
        currentTicker={currentTicker}
        watchlist={watchlist}
        recentTickers={recentTickers}
        onSelectTicker={(t) => setCurrentTicker(t)}
        onOpenCmdk={() => setCmdkOpen(true)}
        onOpenShortcuts={() => setShortcutsOpen(true)}
        darkMode={darkMode}
        onToggleTheme={() => setDarkMode(!darkMode)}
        rainbowMode={rainbowMode}
        onCycleRainbowMode={handleCycleRainbowMode}
      />

      {/* Main Container */}
      <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-4 space-y-6">
        {/* Company Headline & 3-Column Editorial Grid */}
        <CompanyHeader
          summary={summary}
          onPrevTicker={handlePrevTicker}
          onNextTicker={handleNextTicker}
          onSelectTab={setActiveTab}
          isWatched={watchlist.includes(currentTicker)}
          onToggleWatchlist={() => toggleWatchlist(currentTicker)}
        />

        {/* View Tabs */}
        <Tabs activeTab={activeTab} onSelectTab={setActiveTab} />

        {/* Loading / Error States */}
        {loadingSummary && !summary && (
          <div className="py-24 text-center font-mono text-xs text-neutral-400">
            Fetching facts from SEC SQLite database for {currentTicker}...
          </div>
        )}

        {summaryError && (
          <div className="p-4 rounded-xl border border-rose-300 dark:border-rose-900/50 bg-rose-50 dark:bg-rose-950/20 text-xs font-mono text-rose-700 dark:text-rose-300">
            {summaryError}
          </div>
        )}

        {/* Tab Views */}
        {summary && (
          <div className="pt-2">
            {activeTab === 'statements' && (
              <FinancialStatement
                summary={summary}
                compact={compact}
                onToggleCompact={() => setCompact(!compact)}
                cadence={cadence}
                onSelectCadence={setCadence}
              />
            )}

            {activeTab === 'dcf' && (
              <DCFModeler summary={summary} compact={compact} />
            )}

            {activeTab === 'intangibles' && (
              <IntangiblesView summary={summary} compact={compact} />
            )}

            {activeTab === 'filings' && (
              <FilingsBrowser ticker={currentTicker} />
            )}

            {activeTab === 'screener' && (
              <UniverseScreener
                companies={companies}
                currentTicker={currentTicker}
                onSelectTicker={(t) => {
                  setCurrentTicker(t)
                  setActiveTab('statements')
                }}
              />
            )}

            {activeTab === 'compare' && (
              <PeerComparison
                currentTicker={currentTicker}
                companies={companies}
                onSelectTicker={(t) => {
                  setCurrentTicker(t)
                  setActiveTab('statements')
                }}
              />
            )}
          </div>
        )}
      </main>

      {/* Editorial Paco Coursey Style Footer */}
      <footer className="border-t border-neutral-200/80 dark:border-white/10 py-8 mt-16 text-xs font-sans text-neutral-500 bg-white/40 dark:bg-black/20 backdrop-blur-md">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 flex flex-col sm:flex-row items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <span className="text-neutral-700 dark:text-neutral-400 font-medium">
              SEC EDGAR Terminal
            </span>
            <span>•</span>
            <span className="italic font-serif text-neutral-600 dark:text-neutral-400">
              Peters & Taylor Intangibles & DCF Model
            </span>
          </div>

          <div className="flex items-center gap-4 text-[11px] text-neutral-500 font-mono">
            <span>528 Companies in SQLite</span>
            <span>•</span>
            <span>
              Press{' '}
              <kbd className="px-1 py-0.5 rounded bg-neutral-100 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800">
                ⌘K
              </kbd>{' '}
              to search
            </span>
          </div>
        </div>
      </footer>

      {/* Command Palette */}
      <CommandMenu
        open={cmdkOpen}
        onOpenChange={setCmdkOpen}
        companies={companies}
        currentTicker={currentTicker}
        onSelectTicker={(t) => setCurrentTicker(t)}
        activeTab={activeTab}
        onSelectTab={setActiveTab}
        darkMode={darkMode}
        onToggleTheme={() => setDarkMode(!darkMode)}
        onToggleCompact={() => setCompact(!compact)}
        rainbowMode={rainbowMode}
        onCycleRainbowMode={handleCycleRainbowMode}
      />

      {/* Keyboard Shortcuts Modal */}
      <ShortcutsModal
        open={shortcutsOpen}
        onClose={() => setShortcutsOpen(false)}
      />
    </div>
  )
}
