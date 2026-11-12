import React, { useState, useEffect, useCallback, useRef } from 'react'
import { Navbar } from './components/Navbar'
import { CommandMenu } from './components/CommandMenu'
import { CompanyHeader } from './components/CompanyHeader'
import { ForensicBriefing } from './components/ForensicBriefing'
import { SplitWorkspace } from './components/SplitWorkspace'
import { Tabs, TabKey } from './components/Tabs'
import { FinancialStatement } from './components/FinancialStatement'
import { DCFModeler } from './components/DCFModeler'
import { IntangiblesView } from './components/IntangiblesView'
import { FilingsBrowser } from './components/FilingsBrowser'
import { UniverseScreener } from './components/UniverseScreener'
import { PeerComparison } from './components/PeerComparison'
import { ShortcutsModal } from './components/ShortcutsModal'
import { TearSheetModal } from './components/TearSheetModal'
import { RainbowBackground, RainbowMode } from './components/RainbowBackground'
import { Company, SummaryResponse, DatabaseStats, QuoteResponse } from './types'
import { summaryCache, quoteCache, prefetchQuote } from './lib/cache'
import { renderTearSheetToBlob } from './lib/tearSheetCanvas'
import { X } from 'lucide-react'

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

export const App: React.FC = () => {
  const [stats, setStats] = useState<DatabaseStats | null>(null)
  const [companies, setCompanies] = useState<Company[]>([])
  
  const initialRoute = parseHashRoute()
  const [currentTicker, setCurrentTicker] = useState<string>(initialRoute.ticker)
  const [activeTab, setActiveTab] = useState<TabKey>(initialRoute.tab)
  const [cadence, setCadence] = useState<'annual' | 'quarterly' | 'ttm'>('annual')
  const [splitView, setSplitView] = useState(false)

  const [summary, setSummary] = useState<SummaryResponse | null>(() => summaryCache.get(`${initialRoute.ticker}_annual`) || null)
  const [radarSummary, setRadarSummary] = useState<SummaryResponse | null>(() => summaryCache.get(`${initialRoute.ticker}_ttm`) || null)
  const [loadingSummary, setLoadingSummary] = useState<boolean>(!summaryCache.has(`${initialRoute.ticker}_annual`))
  const [summaryError, setSummaryError] = useState<string | null>(null)
  const [quote, setQuote] = useState<QuoteResponse | null>(() => quoteCache.get(initialRoute.ticker) || null)

  const [compact, setCompact] = useState<boolean>(true)
  const [cmdkOpen, setCmdkOpen] = useState<boolean>(false)
  const [shortcutsOpen, setShortcutsOpen] = useState<boolean>(false)
  const [tearSheetOpen, setTearSheetOpen] = useState<boolean>(false)

  // Minimalist Paco Coursey toast notification
  const [toastMessage, setToastMessage] = useState<string | null>(null)
  const toastTimeoutRef = useRef<number | null>(null)

  const showToast = useCallback((msg: string) => {
    if (toastTimeoutRef.current) {
      window.clearTimeout(toastTimeoutRef.current)
    }
    setToastMessage(msg)
    toastTimeoutRef.current = window.setTimeout(() => {
      setToastMessage(null)
      toastTimeoutRef.current = null
    }, 4000)
  }, [])

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

  // Live automatic stats and companies synchronization
  useEffect(() => {
    let ignore = false

    const fetchStats = () => {
      fetch(`/api/stats?_t=${Date.now()}`)
        .then((res) => (res.ok ? res.json() : null))
        .then((data: DatabaseStats | null) => {
          if (!ignore && data) {
            setStats((prev) => {
              if (
                !prev ||
                prev.total_companies !== data.total_companies ||
                prev.total_facts !== data.total_facts ||
                prev.total_filings !== data.total_filings
              ) {
                // If company count changed, re-fetch company list
                if (!prev || prev.total_companies !== data.total_companies) {
                  fetchCompanies()
                }
                return data
              }
              return prev
            })
          }
        })
        .catch(() => {})
    }

    const fetchCompanies = () => {
      fetch(`/api/companies?_t=${Date.now()}`)
        .then((res) => (res.ok ? res.json() : []))
        .then((data: Company[]) => {
          if (!ignore && data.length > 0) {
            setCompanies(data)
          }
        })
        .catch(() => {})
    }

    // Initial fetch
    fetchStats()
    fetchCompanies()

    // Poll automatically every 2.5 seconds so numbers update in real time as facts are synced
    const intervalId = window.setInterval(fetchStats, 2500)

    // Re-fetch immediately whenever tab or window regains focus
    const handleVisibilityChange = () => {
      if (!document.hidden) fetchStats()
    }
    const handleFocus = () => fetchStats()

    document.addEventListener('visibilitychange', handleVisibilityChange)
    window.addEventListener('focus', handleFocus)

    return () => {
      ignore = true
      window.clearInterval(intervalId)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('focus', handleFocus)
    }
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

  // Quote state synchronized with active ticker
  useEffect(() => {
    if (!currentTicker) return
    let ignore = false
    const norm = currentTicker.toUpperCase()
    if (quoteCache.has(norm)) {
      setQuote(quoteCache.get(norm)!)
    }
    prefetchQuote(norm).then((q) => {
      if (!ignore && q) setQuote(q)
    })
    return () => {
      ignore = true
    }
  }, [currentTicker])

  // Instant zero-latency ticker selection using in-memory prefetch cache
  const handleSelectTicker = useCallback((ticker: string) => {
    const norm = ticker.toUpperCase()
    const cached = summaryCache.get(`${norm}_${cadence}`) || summaryCache.get(`${norm}_annual`)
    if (cached) {
      setSummary(cached)
      setLoadingSummary(false)
      setSummaryError(null)
    }
    const cachedQuote = quoteCache.get(norm)
    if (cachedQuote) {
      setQuote(cachedQuote)
    }
    setCurrentTicker(norm)
  }, [cadence])

  // Fetch company summary when ticker or statement cadence changes (with in-memory cache)
  useEffect(() => {
    let ignore = false
    const cacheKey = `${currentTicker}_${cadence}`

    if (summaryCache.has(cacheKey)) {
      setSummary(summaryCache.get(cacheKey)!)
      if (cadence === 'ttm') setRadarSummary(summaryCache.get(cacheKey)!)
      setLoadingSummary(false)
      setSummaryError(null)
    } else if (summaryCache.has(`${currentTicker}_annual`)) {
      setSummary(summaryCache.get(`${currentTicker}_annual`)!)
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
          if (cadence === 'ttm') setRadarSummary(data)
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

  // Keep the forensic briefing on a consistent TTM basis regardless of the statement cadence.
  useEffect(() => {
    let ignore = false
    const cacheKey = `${currentTicker}_ttm`
    const cached = summaryCache.get(cacheKey)
    setRadarSummary(cached || null)

    if (cadence === 'ttm') {
      return () => {
        ignore = true
      }
    }

    fetch(`/api/company/${currentTicker}/summary?view=ttm`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data: SummaryResponse | null) => {
        if (!ignore && data) {
          summaryCache.set(cacheKey, data)
          setRadarSummary(data)
        }
      })
      .catch(() => {})

    return () => {
      ignore = true
    }
  }, [currentTicker, cadence])

  // Navigation handlers
  const handlePrevTicker = useCallback(() => {
    if (companies.length === 0) return
    const idx = companies.findIndex((c) => c.ticker === currentTicker)
    const prevIdx = idx > 0 ? idx - 1 : companies.length - 1
    handleSelectTicker(companies[prevIdx].ticker)
  }, [companies, currentTicker, handleSelectTicker])

  const handleNextTicker = useCallback(() => {
    if (companies.length === 0) return
    const idx = companies.findIndex((c) => c.ticker === currentTicker)
    const nextIdx = idx < companies.length - 1 ? idx + 1 : 0
    handleSelectTicker(companies[nextIdx].ticker)
  }, [companies, currentTicker, handleSelectTicker])

  // One-Click "Tear Sheet" Card Export Handler (⌘ + Shift + S)
  const handleTriggerTearSheet = useCallback(async () => {
    if (!summary) return
    setTearSheetOpen(true)
    try {
      const b = await renderTearSheetToBlob(summary, quote)
      if (navigator.clipboard && window.ClipboardItem) {
        await navigator.clipboard.write([
          new ClipboardItem({ 'image/png': b }),
        ])
        showToast(`Copied ${summary.ticker} Tear Sheet PNG (2x Retina) to clipboard! Ready to paste into X, Substack, or Slack.`)
      }
    } catch (err) {
      console.warn('Auto clipboard write on shortcut:', err)
    }
  }, [summary, quote, showToast])

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

      if ((e.metaKey || e.ctrlKey) && (e.key === '\\' || e.code === 'Backslash')) {
        e.preventDefault()
        setSplitView((value) => !value)
        return
      }

      // One-Click Tear Sheet Export: ⌘ + Shift + S or Ctrl + Shift + S
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && (e.key === 's' || e.key === 'S')) {
        e.preventDefault()
        handleTriggerTearSheet()
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
        setTearSheetOpen(false)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handlePrevTicker, handleNextTicker, handleCycleRainbowMode, handleTriggerTearSheet])

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
        onSelectTicker={handleSelectTicker}
        onOpenCmdk={() => setCmdkOpen(true)}
        onOpenShortcuts={() => setShortcutsOpen(true)}
        onOpenTearSheet={() => setTearSheetOpen(true)}
        splitView={splitView}
        onToggleSplitView={() => setSplitView((value) => !value)}
        darkMode={darkMode}
        onToggleTheme={() => setDarkMode(!darkMode)}
        rainbowMode={rainbowMode}
        onCycleRainbowMode={handleCycleRainbowMode}
      />

      {/* Main Container */}
      <main className={`flex-1 ${splitView ? 'max-w-[1800px]' : 'max-w-7xl'} w-full mx-auto px-4 sm:px-6 py-4 space-y-6`}>
        {splitView ? (
          <SplitWorkspace
            initialTicker={currentTicker}
            initialTab={activeTab}
            companies={companies}
            onExit={() => setSplitView(false)}
          />
        ) : (
        <>
        {/* Company Headline & 3-Column Editorial Grid */}
        <CompanyHeader
          summary={summary}
          onPrevTicker={handlePrevTicker}
          onNextTicker={handleNextTicker}
          onSelectTab={setActiveTab}
          isWatched={watchlist.includes(currentTicker)}
          onToggleWatchlist={() => toggleWatchlist(currentTicker)}
          onOpenTearSheet={() => setTearSheetOpen(true)}
        />

        <ForensicBriefing
          summary={radarSummary?.ticker === currentTicker ? radarSummary : null}
          quote={quote}
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
              <DCFModeler summary={summary} compact={compact} quote={quote} />
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
                  handleSelectTicker(t)
                  setActiveTab('statements')
                }}
              />
            )}

            {activeTab === 'compare' && (
              <PeerComparison
                currentTicker={currentTicker}
                companies={companies}
                onSelectTicker={(t) => {
                  handleSelectTicker(t)
                  setActiveTab('statements')
                }}
              />
            )}
          </div>
        )}
        </>
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
            <span>
              {stats ? `${stats.total_companies.toLocaleString()} Companies in SQLite` : 'SQLite Database'}
            </span>
            <span>•</span>
            <span>
              Press{' '}
              <kbd className="px-1 py-0.5 rounded bg-neutral-100 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800">
                ⌘K
              </kbd>{' '}
              to search
            </span>
            <span>•</span>
            <span>
              Press{' '}
              <kbd className="px-1 py-0.5 rounded bg-neutral-100 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800">
                ⌘⇧S
              </kbd>{' '}
              for Tear Sheet
            </span>
          </div>
        </div>
      </footer>

      {/* Command Palette */}
      <CommandMenu
        open={cmdkOpen}
        onOpenChange={setCmdkOpen}
        companies={companies}
        stats={stats}
        currentTicker={currentTicker}
        onSelectTicker={handleSelectTicker}
        activeTab={activeTab}
        onSelectTab={setActiveTab}
        darkMode={darkMode}
        onToggleTheme={() => setDarkMode(!darkMode)}
        onToggleCompact={() => setCompact(!compact)}
        rainbowMode={rainbowMode}
        onCycleRainbowMode={handleCycleRainbowMode}
        onOpenTearSheet={() => setTearSheetOpen(true)}
      />

      {/* Keyboard Shortcuts Modal */}
      <ShortcutsModal
        open={shortcutsOpen}
        onClose={() => setShortcutsOpen(false)}
      />

      {/* One-Click Tear Sheet Card Modal (⌘ + Shift + S) */}
      <TearSheetModal
        open={tearSheetOpen}
        onClose={() => setTearSheetOpen(false)}
        summary={summary}
        quote={quote}
        onToast={showToast}
      />

      {/* Minimalist Paco Coursey Editorial Toast Notification */}
      {toastMessage && (
        <div className="fixed bottom-6 right-6 z-50 animate-in fade-in slide-in-from-bottom-2 duration-150">
          <div className="flex items-center gap-2.5 px-4 py-3 rounded-xl bg-neutral-950 text-white border border-neutral-800 shadow-2xl font-mono text-xs max-w-md">
            <span className="w-2 h-2 rounded-full bg-emerald-400 shrink-0 animate-pulse" />
            <span className="font-sans text-neutral-200 text-xs">{toastMessage}</span>
            <button
              onClick={() => setToastMessage(null)}
              className="ml-auto text-neutral-500 hover:text-white p-0.5 transition-colors"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
