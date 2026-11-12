import React, { useState, useMemo, useEffect } from 'react'
import {
  Search,
  ArrowRight,
  Filter,
  ArrowUpDown,
  Building,
  CheckCircle,
  HelpCircle,
  Layers,
  Download,
  Sliders,
  Globe,
  Database,
  Sparkles,
  Copy,
  Check,
} from 'lucide-react'
import { Company, RankedCompany, RankResult, ModernGAAPCandidate, UniverseItem } from '../types'
import { formatPercent, formatCurrency } from '../lib/format'
import { prefetchOnHover, prefetchSummary } from '../lib/cache'

interface UniverseScreenerProps {
  companies: Company[]
  currentTicker: string
  onSelectTicker: (ticker: string) => void
}

type SortField =
  | 'score'
  | 'roic'
  | 'fcf_margin'
  | 'revenue_cagr'
  | 'share_reduction'
  | 'revenue'

type FilterPreset = 'all' | 'quality' | 'roic' | 'cannibals' | 'fcf'

export const UniverseScreener: React.FC<UniverseScreenerProps> = ({
  companies,
  currentTicker,
  onSelectTicker,
}) => {
  const [viewMode, setViewMode] = useState<'ranked' | 'scatter' | 'gaap-arbitrage' | 'directory' | 'universes'>('ranked')
  const [rankedData, setRankedData] = useState<RankResult | null>(null)
  const [loadingRank, setLoadingRank] = useState<boolean>(false)
  const [rankError, setRankError] = useState<string | null>(null)
  const [hoveredScatterTicker, setHoveredScatterTicker] = useState<string | null>(null)

  // Universes State
  const [universes, setUniverses] = useState<UniverseItem[]>([])
  const [loadingUniverses, setLoadingUniverses] = useState<boolean>(false)
  const [copiedSyncCmd, setCopiedSyncCmd] = useState<string | null>(null)

  // Modern GAAP Arbitrage State
  const [gaapCandidates, setGaapCandidates] = useState<ModernGAAPCandidate[]>([])
  const [loadingGAAP, setLoadingGAAP] = useState<boolean>(false)
  const [gaapError, setGaapError] = useState<string | null>(null)
  const [gaapSortField, setGaapSortField] = useState<keyof ModernGAAPCandidate>('roic_delta')
  const [gaapSortAsc, setGaapSortAsc] = useState<boolean>(false)

  const [search, setSearch] = useState('')
  const [selectedSector, setSelectedSector] = useState<string>('ALL')
  const [preset, setPreset] = useState<FilterPreset>('all')
  const [sortField, setSortField] = useState<SortField>('score')
  const [sortAsc, setSortAsc] = useState<boolean>(false)

  // Quantitative Range Sliders
  const [minScore, setMinScore] = useState<number>(0)
  const [minROIC, setMinROIC] = useState<number>(-5)
  const [minRevCAGR, setMinRevCAGR] = useState<number>(-10)
  const [showAdvancedFilters, setShowAdvancedFilters] = useState<boolean>(false)

  const exportScreenerCSV = () => {
    const headers = [
      'Rank',
      'Score',
      'Ticker',
      'Company',
      'Sector',
      'ROIC %',
      'FCF Margin %',
      'Rev 5Y CAGR %',
      'Buyback Yield %',
      'Revenue ($)',
    ]
    const rows = [headers]
    filteredRanked.forEach((c, idx) => {
      rows.push([
        (idx + 1).toString(),
        c.score.toFixed(1),
        c.ticker,
        `"${c.company.replace(/"/g, '""')}"`,
        `"${c.sector}"`,
        c.has_roic ? (c.roic * 100).toFixed(2) : '',
        (c.fcf_margin * 100).toFixed(2),
        (c.revenue_cagr * 100).toFixed(2),
        c.has_share_trend ? (c.share_reduction * 100).toFixed(2) : '',
        c.revenue.toString(),
      ])
    })
    const csvContent =
      'data:text/csv;charset=utf-8,' +
      rows.map((r) => r.join(',')).join('\n')
    const encoded = encodeURI(csvContent)
    const link = document.createElement('a')
    link.href = encoded
    link.download = `sec_screener_${preset}_${new Date().toISOString().split('T')[0]}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  // Fetch ranking data
  useEffect(() => {
    let ignore = false
    setLoadingRank(true)
    fetch('/api/screener/rank')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP error ${res.status}`)
        return res.json()
      })
      .then((data: RankResult) => {
        if (!ignore) {
          setRankedData(data)
        }
      })
      .catch((err) => {
        if (!ignore) setRankError(err.message || 'Failed to load ranking')
      })
      .finally(() => {
        if (!ignore) setLoadingRank(false)
      })

    return () => {
      ignore = true
    }
  }, [])

  // Fetch GAAP Arbitrage data
  useEffect(() => {
    if (viewMode === 'gaap-arbitrage' && gaapCandidates.length === 0 && !loadingGAAP) {
      let ignore = false
      setLoadingGAAP(true)
      fetch('/api/screener/gaap-arbitrage')
        .then((res) => {
          if (!res.ok) throw new Error(`HTTP error ${res.status}`)
          return res.json()
        })
        .then((data: { candidates: ModernGAAPCandidate[] }) => {
          if (!ignore && data?.candidates) {
            setGaapCandidates(data.candidates)
          }
        })
        .catch((err) => {
          if (!ignore) setGaapError(err.message || 'Failed to load GAAP Arbitrage screener')
        })
        .finally(() => {
          if (!ignore) setLoadingGAAP(false)
        })
      return () => {
        ignore = true
      }
    }
  }, [viewMode, gaapCandidates.length, loadingGAAP])

  // Fetch Universes data
  useEffect(() => {
    if (viewMode === 'universes' && universes.length === 0 && !loadingUniverses) {
      let ignore = false
      setLoadingUniverses(true)
      fetch('/api/universes')
        .then((res) => (res.ok ? res.json() : []))
        .then((data: UniverseItem[]) => {
          if (!ignore) setUniverses(data)
        })
        .catch(() => {})
        .finally(() => {
          if (!ignore) setLoadingUniverses(false)
        })
      return () => {
        ignore = true
      }
    }
  }, [viewMode, universes.length, loadingUniverses])

  // Sectors list
  const sectorOptions = useMemo(() => {
    if (!rankedData) return []
    const set = new Set<string>()
    rankedData.companies.forEach((c) => {
      if (c.sector) set.add(c.sector)
    })
    return Array.from(set).sort()
  }, [rankedData])

  // SIC list for directory
  const sicOptions = useMemo(() => {
    const map = new Map<string, string>()
    companies.forEach((c) => {
      if (c.sic_description) {
        map.set(c.sic, c.sic_description)
      }
    })
    return Array.from(map.entries()).sort((a, b) => a[1].localeCompare(b[1]))
  }, [companies])

  // Filter & Sort Ranked Companies
  const filteredRanked = useMemo(() => {
    if (!rankedData) return []
    let list = rankedData.companies.filter((c) => {
      // Sector filter
      if (selectedSector !== 'ALL' && c.sector !== selectedSector) {
        return false
      }
      // Preset filter
      if (preset === 'quality' && c.score < 75) return false
      if (preset === 'roic' && (!c.has_roic || c.roic < 0.15)) return false
      if (preset === 'cannibals' && (!c.has_share_trend || c.share_reduction <= 0))
        return false
      if (preset === 'fcf' && c.fcf_margin < 0.15) return false

      // Text search
      if (search.trim()) {
        const q = search.toLowerCase()
        return (
          c.ticker.toLowerCase().includes(q) ||
          c.company.toLowerCase().includes(q) ||
          c.sector.toLowerCase().includes(q) ||
          c.peer_group.toLowerCase().includes(q)
        )
      }
      return true
    })

    // Advanced quantitative range filters
    if (minScore > 0) {
      list = list.filter((c) => c.score >= minScore)
    }
    if (minROIC > -5) {
      list = list.filter((c) => c.has_roic && c.roic * 100 >= minROIC)
    }
    if (minRevCAGR > -10) {
      list = list.filter((c) => c.revenue_cagr * 100 >= minRevCAGR)
    }

    // Sort
    list.sort((a, b) => {
      let va = 0
      let vb = 0
      switch (sortField) {
        case 'score':
          va = a.score
          vb = b.score
          break
        case 'roic':
          va = a.has_roic ? a.roic : -999
          vb = b.has_roic ? b.roic : -999
          break
        case 'fcf_margin':
          va = a.fcf_margin
          vb = b.fcf_margin
          break
        case 'revenue_cagr':
          va = a.revenue_cagr
          vb = b.revenue_cagr
          break
        case 'share_reduction':
          va = a.has_share_trend ? a.share_reduction : -999
          vb = b.has_share_trend ? b.share_reduction : -999
          break
        case 'revenue':
          va = a.revenue
          vb = b.revenue
          break
      }
      return sortAsc ? va - vb : vb - va
    })

    return list
  }, [rankedData, selectedSector, preset, search, sortField, sortAsc, minScore, minROIC, minRevCAGR])

  // Directory filter
  const filteredDirectory = useMemo(() => {
    return companies.filter((c) => {
      if (selectedSector !== 'ALL' && c.sic !== selectedSector) {
        return false
      }
      if (search.trim()) {
        const q = search.toLowerCase()
        return (
          c.ticker.toLowerCase().includes(q) ||
          c.name.toLowerCase().includes(q) ||
          c.sic_description.toLowerCase().includes(q) ||
          c.cik.includes(q)
        )
      }
      return true
    })
  }, [companies, search, selectedSector])

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortAsc(!sortAsc)
    } else {
      setSortField(field)
      setSortAsc(false)
    }
  }

  const scoreBadgeColor = (score: number) => {
    if (score >= 80)
      return 'border border-neutral-900 dark:border-white text-neutral-950 dark:text-white font-semibold'
    if (score >= 70)
      return 'border border-neutral-400 dark:border-neutral-600 text-neutral-800 dark:text-neutral-200'
    return 'border border-neutral-200 dark:border-neutral-800 text-neutral-500'
  }

  // Filter & Sort Modern GAAP Arbitrage Candidates
  const filteredGAAP = useMemo(() => {
    let list = gaapCandidates.filter((c) => {
      if (search.trim()) {
        const q = search.toLowerCase()
        return (
          c.ticker.toLowerCase().includes(q) ||
          c.company.toLowerCase().includes(q) ||
          c.sic_description.toLowerCase().includes(q) ||
          c.arbitrage_signal.toLowerCase().includes(q)
        )
      }
      return true
    })

    list.sort((a, b) => {
      let va = (a as any)[gaapSortField] ?? 0
      let vb = (b as any)[gaapSortField] ?? 0
      if (typeof va === 'string') {
        return gaapSortAsc ? va.localeCompare(vb) : vb.localeCompare(va)
      }
      return gaapSortAsc ? va - vb : vb - va
    })

    return list
  }, [gaapCandidates, search, gaapSortField, gaapSortAsc])

  const handleGAAPSort = (field: keyof ModernGAAPCandidate) => {
    if (gaapSortField === field) {
      setGaapSortAsc(!gaapSortAsc)
    } else {
      setGaapSortField(field)
      setGaapSortAsc(false)
    }
  }

  return (
    <div className="space-y-4 animate-in fade-in duration-150 font-sans">
      {/* Header & Mode Switcher */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pt-1">
        <div>
          <h2 className="text-sm font-medium text-neutral-950 dark:text-white">
            SEC EDGAR Fundamentals & Quality Universe
          </h2>
          <p className="text-[11px] text-neutral-500 dark:text-neutral-400 font-sans mt-0.5">
            {companies.length} companies synced • Standardized multi-factor ranking and knowledge asset capitalization.
          </p>
        </div>

        {/* View Mode Segment Toggle */}
        <div className="flex items-center gap-1 font-mono text-xs overflow-x-auto pb-1 sm:pb-0 scrollbar-none">
          <button
            onClick={() => setViewMode('ranked')}
            className={`px-3 py-1.5 rounded transition-colors whitespace-nowrap ${
              viewMode === 'ranked'
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white border border-transparent hover:border-neutral-200 dark:hover:border-neutral-800'
            }`}
          >
            Compounder Rank
          </button>

          <button
            onClick={() => setViewMode('scatter')}
            className={`px-3 py-1.5 rounded transition-colors whitespace-nowrap ${
              viewMode === 'scatter'
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white border border-transparent hover:border-neutral-200 dark:border-neutral-800'
            }`}
          >
            Quality Matrix
          </button>

          <button
            onClick={() => setViewMode('gaap-arbitrage')}
            className={`px-3 py-1.5 rounded transition-colors whitespace-nowrap ${
              viewMode === 'gaap-arbitrage'
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white border border-transparent hover:border-neutral-200 dark:hover:border-neutral-800'
            }`}
          >
            Modern GAAP Arbitrage
          </button>

          <button
            onClick={() => setViewMode('directory')}
            className={`px-3 py-1.5 rounded transition-colors whitespace-nowrap ${
              viewMode === 'directory'
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white border border-transparent hover:border-neutral-200 dark:border-neutral-800'
            }`}
          >
            SEC Directory
          </button>

          <button
            onClick={() => setViewMode('universes')}
            className={`px-3 py-1.5 rounded transition-colors whitespace-nowrap ${
              viewMode === 'universes'
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white border border-transparent hover:border-neutral-200 dark:hover:border-neutral-800'
            }`}
          >
            Universes (3,000+)
          </button>
        </div>
      </div>

      {/* Filter Toolbar */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-3 text-xs font-mono py-1">
        {/* Preset Filter Pills & Controls (Ranked Mode Only) */}
        {viewMode === 'ranked' && (
          <div className="flex items-center gap-1.5 overflow-x-auto pb-1 md:pb-0 scrollbar-none">
            {[
              { id: 'all', label: 'All Ranked' },
              { id: 'quality', label: 'Score ≥ 75' },
              { id: 'roic', label: 'ROIC ≥ 15%' },
              { id: 'cannibals', label: 'Buybacks' },
              { id: 'fcf', label: 'FCF Margin ≥ 15%' },
            ].map((p) => (
              <button
                key={p.id}
                onClick={() => setPreset(p.id as FilterPreset)}
                className={`px-2 py-1 rounded transition-colors border ${
                  preset === p.id
                    ? 'border-neutral-900 dark:border-white bg-neutral-900 text-white dark:bg-white dark:text-black font-medium'
                    : 'border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
                }`}
              >
                {p.label}
              </button>
            ))}

            <div className="h-4 w-px bg-neutral-200 dark:bg-neutral-800 mx-1 hidden sm:block" />

            <button
              onClick={() => setShowAdvancedFilters(!showAdvancedFilters)}
              className={`flex items-center gap-1 px-2 py-1 rounded transition-colors border ${
                showAdvancedFilters || minScore > 0 || minROIC > -5 || minRevCAGR > -10
                  ? 'border-neutral-900 dark:border-white bg-neutral-100 dark:bg-neutral-800 text-neutral-900 dark:text-white font-medium'
                  : 'border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
              }`}
              title="Toggle Quantitative Range Filters"
            >
              <Sliders className="w-3 h-3" />
              <span>Filters</span>
              {(minScore > 0 || minROIC > -5 || minRevCAGR > -10) && (
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
              )}
            </button>

            <button
              onClick={exportScreenerCSV}
              className="flex items-center gap-1 px-2 py-1 rounded transition-colors border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white hover:border-neutral-300 dark:hover:border-neutral-700"
              title="Export Current Screener View to CSV"
            >
              <Download className="w-3 h-3" />
              <span>CSV</span>
            </button>
          </div>
        )}

        <div className="flex items-center gap-2 self-start md:self-auto w-full md:w-auto">
          {/* Sector Selector */}
          <div className="relative">
            <select
              value={selectedSector}
              onChange={(e) => setSelectedSector(e.target.value)}
              className="bg-white dark:bg-neutral-900/80 border border-neutral-200 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300 rounded-lg px-2.5 py-1.5 text-xs font-mono outline-none focus:border-neutral-500 appearance-none pr-8 cursor-pointer"
            >
              <option value="ALL">All Sectors</option>
              {viewMode === 'ranked'
                ? sectorOptions.map((sec) => (
                    <option key={sec} value={sec}>
                      {sec}
                    </option>
                  ))
                : sicOptions.map(([sic, desc]) => (
                    <option key={sic} value={sic}>
                      {desc}
                    </option>
                  ))}
            </select>
            <Filter className="w-3 h-3 text-neutral-400 absolute right-2.5 top-2.5 pointer-events-none" />
          </div>

          {/* Search Box */}
          <div className="relative min-w-[200px] flex-1 md:flex-initial">
            <Search className="w-3.5 h-3.5 text-neutral-400 absolute left-2.5 top-2.5" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search ticker or name..."
              className="w-full bg-white dark:bg-neutral-900/80 border border-neutral-200 dark:border-neutral-800 rounded-lg pl-8 pr-3 py-1.5 text-xs text-neutral-900 dark:text-neutral-200 placeholder:text-neutral-400 outline-none focus:border-neutral-500 font-mono"
            />
          </div>
        </div>
      </div>

      {/* Advanced Quantitative Filters Drawer */}
      {viewMode === 'ranked' && showAdvancedFilters && (
        <div className="p-3.5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50/80 dark:bg-neutral-900/50 backdrop-blur-sm grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs font-mono animate-in fade-in duration-150">
          <div>
            <div className="flex justify-between items-center text-neutral-600 dark:text-neutral-400 mb-1">
              <span>Min Quality Score</span>
              <span className="font-semibold text-neutral-900 dark:text-white">
                {minScore > 0 ? minScore : 'Off'}
              </span>
            </div>
            <input
              type="range"
              min="0"
              max="90"
              step="5"
              value={minScore}
              onChange={(e) => setMinScore(Number(e.target.value))}
              className="w-full accent-neutral-900 dark:accent-white cursor-pointer"
            />
            <div className="flex justify-between text-[10px] text-neutral-400">
              <span>0</span>
              <span>50</span>
              <span>90</span>
            </div>
          </div>

          <div>
            <div className="flex justify-between items-center text-neutral-600 dark:text-neutral-400 mb-1">
              <span>Min ROIC (%)</span>
              <span className="font-semibold text-neutral-900 dark:text-white">
                {minROIC > -5 ? `${minROIC}%` : 'Off'}
              </span>
            </div>
            <input
              type="range"
              min="-5"
              max="40"
              step="5"
              value={minROIC}
              onChange={(e) => setMinROIC(Number(e.target.value))}
              className="w-full accent-neutral-900 dark:accent-white cursor-pointer"
            />
            <div className="flex justify-between text-[10px] text-neutral-400">
              <span>Any</span>
              <span>15%</span>
              <span>40%</span>
            </div>
          </div>

          <div>
            <div className="flex justify-between items-center text-neutral-600 dark:text-neutral-400 mb-1">
              <span>Min 5Y Rev CAGR (%)</span>
              <span className="font-semibold text-neutral-900 dark:text-white">
                {minRevCAGR > -10 ? `${minRevCAGR}%` : 'Off'}
              </span>
            </div>
            <input
              type="range"
              min="-10"
              max="30"
              step="5"
              value={minRevCAGR}
              onChange={(e) => setMinRevCAGR(Number(e.target.value))}
              className="w-full accent-neutral-900 dark:accent-white cursor-pointer"
            />
            <div className="flex justify-between text-[10px] text-neutral-400">
              <span>Any</span>
              <span>10%</span>
              <span>30%</span>
            </div>
          </div>

          {(minScore > 0 || minROIC > -5 || minRevCAGR > -10) && (
            <div className="sm:col-span-3 flex justify-end pt-1">
              <button
                onClick={() => {
                  setMinScore(0)
                  setMinROIC(-5)
                  setMinRevCAGR(-10)
                }}
                className="text-[11px] text-neutral-500 hover:text-rose-500 transition-colors underline underline-offset-2"
              >
                Reset Range Filters
              </button>
            </div>
          )}
        </div>
      )}

      {/* ========================================================= */}
      {/* RANKED VIEW TABLE */}
      {/* ========================================================= */}
      {viewMode === 'ranked' && (
        <div className="rounded-xl border border-neutral-200 dark:border-neutral-800/80 bg-neutral-50/50 dark:bg-neutral-950/40 overflow-hidden">
          <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800/80 bg-neutral-100/50 dark:bg-neutral-900/40 flex justify-between items-center text-xs font-mono">
            <span className="text-neutral-900 dark:text-white font-medium">
              Showing {filteredRanked.length} Ranked Compounders
            </span>
            <span className="text-neutral-500 text-[11px]">
              Click any column header to sort • Click row to inspect
            </span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono">
              <thead>
                <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-500 bg-neutral-100/30 dark:bg-neutral-900/20">
                  <th className="py-2.5 px-4 font-normal w-12 text-center">Rank</th>
                  <th
                    className="py-2.5 px-4 font-normal cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('score')}
                  >
                    <div className="flex items-center gap-1">
                      <span>Score</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                  <th className="py-2.5 px-4 font-normal">Company</th>
                  <th className="py-2.5 px-4 font-normal">Sector</th>
                  <th
                    className="py-2.5 px-4 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('roic')}
                  >
                    <div className="flex items-center justify-end gap-1">
                      <span>ROIC</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                  <th
                    className="py-2.5 px-4 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('fcf_margin')}
                  >
                    <div className="flex items-center justify-end gap-1">
                      <span>FCF Margin</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                  <th
                    className="py-2.5 px-4 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('revenue_cagr')}
                  >
                    <div className="flex items-center justify-end gap-1">
                      <span>Rev 5Y CAGR</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                  <th
                    className="py-2.5 px-4 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('share_reduction')}
                  >
                    <div className="flex items-center justify-end gap-1">
                      <span>Buyback Yield</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                  <th
                    className="py-2.5 px-4 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                    onClick={() => handleSort('revenue')}
                  >
                    <div className="flex items-center justify-end gap-1">
                      <span>Revenue</span>
                      <ArrowUpDown className="w-3 h-3" />
                    </div>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-200 dark:divide-neutral-900">
                {loadingRank && filteredRanked.length === 0 && (
                  <tr>
                    <td colSpan={9} className="py-12 text-center text-neutral-500 font-mono">
                      Computing 5-year multi-factor ranks across {companies.length || 'all'} companies...
                    </td>
                  </tr>
                )}

                {filteredRanked.map((c, idx) => {
                  const isCurrent = c.ticker === currentTicker
                  return (
                    <tr
                      key={c.ticker}
                      onClick={() => onSelectTicker(c.ticker)}
                      {...prefetchOnHover(c.ticker)}
                      className={`hover:bg-neutral-100/80 dark:hover:bg-neutral-900/60 cursor-pointer transition-colors ${
                        isCurrent
                          ? 'bg-neutral-100/90 dark:bg-neutral-900/80 font-medium'
                          : ''
                      }`}
                    >
                      <td className="py-2.5 px-4 text-center text-neutral-400">
                        {idx + 1}
                      </td>

                      <td className="py-2.5 px-4">
                        <span
                          className={`inline-block px-2 py-0.5 rounded text-xs font-semibold border ${scoreBadgeColor(
                            c.score
                          )}`}
                        >
                          {c.score.toFixed(1)}
                        </span>
                      </td>

                      <td className="py-2.5 px-4">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-neutral-900 dark:text-white">
                            {c.ticker}
                          </span>
                          <span className="text-neutral-500 dark:text-neutral-400 truncate max-w-[180px] font-sans text-xs">
                            {c.company}
                          </span>
                        </div>
                      </td>

                      <td className="py-2.5 px-4 text-neutral-500 text-[11px]">
                        {c.sector}
                      </td>

                      <td className="py-2.5 px-4 text-right tabular-nums">
                        {c.has_roic ? (
                          <span
                            className={
                              c.roic >= 0.15
                                ? 'text-emerald-600 dark:text-emerald-400 font-medium'
                                : 'text-neutral-700 dark:text-neutral-300'
                            }
                          >
                            {formatPercent(c.roic)}
                          </span>
                        ) : (
                          <span className="text-neutral-400">—</span>
                        )}
                      </td>

                      <td className="py-2.5 px-4 text-right tabular-nums">
                        <span
                          className={
                            c.fcf_margin >= 0.15
                              ? 'text-emerald-600 dark:text-emerald-400 font-medium'
                              : 'text-neutral-700 dark:text-neutral-300'
                          }
                        >
                          {formatPercent(c.fcf_margin)}
                        </span>
                      </td>

                      <td className="py-2.5 px-4 text-right tabular-nums">
                        <span
                          className={
                            c.revenue_cagr >= 0.1
                              ? 'text-blue-600 dark:text-blue-400'
                              : 'text-neutral-700 dark:text-neutral-300'
                          }
                        >
                          {formatPercent(c.revenue_cagr)}
                        </span>
                      </td>

                      <td className="py-2.5 px-4 text-right tabular-nums">
                        {c.has_share_trend ? (
                          <span
                            className={
                              c.share_reduction > 0
                                ? 'text-emerald-600 dark:text-emerald-400'
                                : 'text-neutral-500'
                            }
                          >
                            {c.share_reduction > 0 ? '+' : ''}
                            {formatPercent(c.share_reduction)}
                          </span>
                        ) : (
                          <span className="text-neutral-400">—</span>
                        )}
                      </td>

                      <td className="py-2.5 px-4 text-right tabular-nums text-neutral-700 dark:text-neutral-300">
                        {formatCurrency(c.revenue, true, c.currency)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ========================================================= */}
      {/* QUALITY MATRIX SCATTER PLOT VIEW                          */}
      {/* ========================================================= */}
      {viewMode === 'scatter' && (() => {
        const validCompanies = filteredRanked.filter((c) => c.has_roic && c.roic >= -0.05 && c.roic <= 0.55)
        const svgW = 820
        const svgH = 420
        const pad = { top: 30, right: 40, bottom: 45, left: 55 }
        const innerW = svgW - pad.left - pad.right
        const innerH = svgH - pad.top - pad.bottom

        const minROIC = -0.05
        const maxROIC = 0.50
        const rangeROIC = maxROIC - minROIC

        const minScore = 0
        const maxScore = 100
        const rangeScore = maxScore - minScore

        const getX = (roic: number) => pad.left + Math.max(0, Math.min(innerW, ((roic - minROIC) / rangeROIC) * innerW))
        const getY = (score: number) => pad.top + innerH * (1 - (score - minScore) / rangeScore)

        const hoveredCompany = hoveredScatterTicker ? validCompanies.find((c) => c.ticker === hoveredScatterTicker) : null

        return (
          <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-xl p-4 sm:p-6 space-y-4 font-mono text-xs">
            {/* Header info */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-neutral-200 dark:border-neutral-800 pb-3">
              <div>
                <span className="font-sans font-medium text-neutral-950 dark:text-white text-sm block">
                  Institutional Quality Matrix: ROIC vs. Buffett Compounder Score
                </span>
                <span className="text-neutral-500 font-sans text-xs">
                  {validCompanies.length} companies plotted • Bubble size = Revenue • Click bubble to inspect
                </span>
              </div>
              {hoveredCompany && (
                <div className="text-[11px] bg-neutral-100 dark:bg-neutral-800/80 px-3 py-1 rounded border border-neutral-200 dark:border-neutral-700 font-mono">
                  <strong className="text-neutral-950 dark:text-white">{hoveredCompany.ticker}</strong> ({hoveredCompany.company}):{' '}
                  Score <strong className="text-neutral-950 dark:text-white">{hoveredCompany.score.toFixed(1)}</strong> • ROIC{' '}
                  <strong className="text-emerald-600 dark:text-emerald-400">{formatPercent(hoveredCompany.roic)}</strong> • FCF Margin{' '}
                  {formatPercent(hoveredCompany.fcf_margin)}
                </div>
              )}
            </div>

            {/* SVG Plot */}
            <div className="w-full overflow-x-auto">
              <svg viewBox={`0 0 ${svgW} ${svgH}`} className="w-full h-80 sm:h-96 select-none">
                {/* Quadrant Background Shading */}
                <rect
                  x={getX(0.12)}
                  y={pad.top}
                  width={svgW - pad.right - getX(0.12)}
                  height={getY(50) - pad.top}
                  className="fill-emerald-500/[0.04]"
                />
                <text
                  x={svgW - pad.right - 10}
                  y={pad.top + 18}
                  textAnchor="end"
                  className="text-[10px] font-sans fill-emerald-600/70 dark:fill-emerald-400/70 font-medium"
                >
                  Quality Compounder Elite (ROIC &gt; 12%, Score &gt; 50)
                </text>

                {/* Grid Lines */}
                {[0, 25, 50, 75, 100].map((sc) => {
                  const y = getY(sc)
                  return (
                    <g key={sc}>
                      <line
                        x1={pad.left}
                        y1={y}
                        x2={svgW - pad.right}
                        y2={y}
                        stroke="currentColor"
                        className={sc === 50 ? 'text-neutral-400 dark:text-neutral-600' : 'text-neutral-200 dark:text-neutral-800/80'}
                        strokeDasharray={sc === 50 ? '3,3' : '2,3'}
                      />
                      <text x={pad.left - 8} y={y + 3} textAnchor="end" className="text-[10px] fill-neutral-400 font-mono">
                        {sc}
                      </text>
                    </g>
                  )
                })}

                {[-0.05, 0, 0.1, 0.2, 0.3, 0.4, 0.5].map((r) => {
                  const x = getX(r)
                  return (
                    <g key={r}>
                      <line
                        x1={x}
                        y1={pad.top}
                        x2={x}
                        y2={svgH - pad.bottom}
                        stroke="currentColor"
                        className={r === 0.12 ? 'text-neutral-400 dark:text-neutral-600' : 'text-neutral-200 dark:text-neutral-800/80'}
                        strokeDasharray={r === 0.12 ? '3,3' : '2,3'}
                      />
                      <text x={x} y={svgH - pad.bottom + 15} textAnchor="middle" className="text-[10px] fill-neutral-400 font-mono">
                        {(r * 100).toFixed(0)}%
                      </text>
                    </g>
                  )
                })}

                {/* Axis Labels */}
                <text
                  x={svgW / 2}
                  y={svgH - 8}
                  textAnchor="middle"
                  className="text-[11px] font-mono fill-neutral-500 uppercase tracking-wider"
                >
                  Peters &amp; Taylor Intangibles-Adjusted ROIC (X-Axis)
                </text>
                <text
                  transform={`rotate(-90)`}
                  x={-svgH / 2}
                  y={16}
                  textAnchor="middle"
                  className="text-[11px] font-mono fill-neutral-500 uppercase tracking-wider"
                >
                  Buffett Quality Score (0–100)
                </text>

                {/* Company Bubbles */}
                {validCompanies.map((c) => {
                  const cx = getX(c.roic)
                  const cy = getY(c.score)
                  const isCurrent = c.ticker === currentTicker
                  const isHovered = hoveredScatterTicker === c.ticker
                  const r = Math.max(3.5, Math.min(10, Math.sqrt((c.revenue || 1e9) / 1e9) * 1.5))

                  return (
                    <g
                      key={c.ticker}
                      className="cursor-pointer"
                      onMouseEnter={() => {
                        setHoveredScatterTicker(c.ticker)
                        prefetchSummary(c.ticker)
                      }}
                      onMouseLeave={() => setHoveredScatterTicker(null)}
                      onClick={() => onSelectTicker(c.ticker)}
                    >
                      {isCurrent && (
                        <circle
                          cx={cx}
                          cy={cy}
                          r={r + 5}
                          className="fill-none stroke-indigo-500 stroke-2 animate-ping opacity-60"
                        />
                      )}
                      <circle
                        cx={cx}
                        cy={cy}
                        r={isHovered ? r + 3 : isCurrent ? r + 2 : r}
                        className={`transition-all duration-150 ${
                          isCurrent
                            ? 'fill-indigo-600 dark:fill-indigo-400 stroke-white dark:stroke-black stroke-2'
                            : c.score >= 80
                            ? 'fill-emerald-500 dark:fill-emerald-400 opacity-80 hover:opacity-100'
                            : c.score >= 60
                            ? 'fill-blue-500 dark:fill-blue-400 opacity-70 hover:opacity-100'
                            : 'fill-neutral-400 dark:fill-neutral-600 opacity-50 hover:opacity-100'
                        }`}
                      />
                      {(isHovered || isCurrent || c.score >= 85) && (
                        <text
                          x={cx}
                          y={cy - r - 4}
                          textAnchor="middle"
                          className={`text-[10px] font-mono font-semibold pointer-events-none ${
                            isCurrent
                              ? 'fill-indigo-600 dark:fill-indigo-400'
                              : 'fill-neutral-900 dark:fill-white'
                          }`}
                        >
                          {c.ticker}
                        </text>
                      )}
                    </g>
                  )
                })}
              </svg>
            </div>
          </div>
        )
      })()}

      {/* ========================================================= */}
      {/* MODERN GAAP ARBITRAGE VIEW TABLE */}
      {/* ========================================================= */}
      {viewMode === 'gaap-arbitrage' && (
        <div className="space-y-3">
          {/* Section Header */}
          <div className="border border-neutral-200 dark:border-neutral-800 p-4 bg-neutral-50/50 dark:bg-neutral-950 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs font-mono">
            <div>
              <span className="font-semibold text-neutral-900 dark:text-white uppercase tracking-wider text-[11px] block">
                Modern GAAP Arbitrage Candidates
              </span>
              <p className="text-neutral-500 font-sans mt-0.5 text-xs">
                Companies where R&D and brand expensing under GAAP depresses reported earnings while compounding true economic capital.
              </p>
            </div>
            <div className="text-[11px] text-neutral-400 font-mono">
              Peters & Taylor Model
            </div>
          </div>

          {loadingGAAP ? (
            <div className="py-24 text-center font-mono text-xs text-neutral-400">
              Evaluating companies across SQLite database...
            </div>
          ) : gaapError ? (
            <div className="p-4 border border-rose-300 dark:border-rose-900/40 bg-rose-50 dark:bg-rose-950/20 text-xs text-rose-700 dark:text-rose-300 font-mono">
              {gaapError}
            </div>
          ) : (
            <div className="border border-neutral-200 dark:border-neutral-800 overflow-hidden bg-white dark:bg-black">
              <div className="px-4 py-2 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-950 flex justify-between items-center text-xs font-mono">
                <span className="text-neutral-900 dark:text-white font-medium">
                  Showing {filteredGAAP.length} Arbitrage Candidates
                </span>
                <span className="text-neutral-400 text-[11px]">
                  Click row to inspect company
                </span>
              </div>

              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs font-mono">
                  <thead>
                    <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-400 bg-neutral-50/50 dark:bg-neutral-950">
                      <th className="py-2 px-3 font-normal w-10 text-center">#</th>
                      <th className="py-2 px-3 font-normal">Arbitrage Signal</th>
                      <th className="py-2 px-3 font-normal">Company</th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('roic_delta')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>ROIC Δ Spread</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('adjusted_roic')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>Adj. ROIC</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('adjusted_ebit_margin')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>Adj. EBIT %</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('intangible_capital')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>Intangible Cap</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('adjusted_owner_earnings')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>Owner Earnings</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                      <th
                        className="py-2 px-3 font-normal text-right cursor-pointer hover:text-neutral-900 dark:hover:text-white transition-colors"
                        onClick={() => handleGAAPSort('arbitrage_score')}
                      >
                        <div className="flex items-center justify-end gap-1">
                          <span>Score</span>
                          <ArrowUpDown className="w-3 h-3" />
                        </div>
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                    {filteredGAAP.map((c, idx) => {
                      const isCurrent = c.ticker === currentTicker

                      return (
                        <tr
                          key={c.ticker}
                          onClick={() => onSelectTicker(c.ticker)}
                          {...prefetchOnHover(c.ticker)}
                          className={`cursor-pointer transition-colors ${
                            isCurrent
                              ? 'bg-neutral-100 dark:bg-neutral-900 font-medium'
                              : 'hover:bg-neutral-50 dark:hover:bg-neutral-950'
                          }`}
                        >
                          <td className="py-2 px-3 text-center text-neutral-400">
                            {idx + 1}
                          </td>
                          <td className="py-2 px-3">
                            <span className="px-1.5 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 text-[10px] font-mono whitespace-nowrap text-neutral-700 dark:text-neutral-300">
                              {c.arbitrage_signal}
                            </span>
                          </td>
                          <td className="py-2 px-3">
                            <div className="flex items-center gap-1.5">
                              <span className="font-semibold text-neutral-950 dark:text-white font-mono">
                                {c.ticker}
                              </span>
                              <span className="text-neutral-500 font-sans text-xs truncate max-w-[160px]">
                                {c.company}
                              </span>
                            </div>
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums">
                            <span className="text-emerald-600 dark:text-emerald-400 font-semibold">
                              +{(c.roic_delta * 100).toFixed(1)}%
                            </span>
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums">
                            <span className="font-medium text-neutral-900 dark:text-white">
                              {formatPercent(c.adjusted_roic)}
                            </span>
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums">
                            <span className="text-neutral-700 dark:text-neutral-300">
                              {formatPercent(c.adjusted_ebit_margin)}
                            </span>
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums text-neutral-700 dark:text-neutral-300">
                            {formatCurrency(c.intangible_capital, true, c.currency)}
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums text-neutral-700 dark:text-neutral-300">
                            {formatCurrency(c.adjusted_owner_earnings, true, c.currency)}
                          </td>
                          <td className="py-2 px-3 text-right tabular-nums">
                            <span className="font-semibold text-neutral-900 dark:text-white">
                              {c.arbitrage_score.toFixed(1)}
                            </span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ========================================================= */}
      {/* DIRECTORY VIEW TABLE */}
      {/* ========================================================= */}
      {viewMode === 'directory' && (
        <div className="rounded-xl border border-neutral-200 dark:border-neutral-800/80 bg-neutral-50/50 dark:bg-neutral-950/40 overflow-hidden">
          <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800/80 bg-neutral-100/50 dark:bg-neutral-900/40 flex justify-between items-center text-xs font-mono">
            <span className="text-neutral-900 dark:text-white font-medium">
              Showing {filteredDirectory.length} of {companies.length} Synced Companies
            </span>
            <span className="text-neutral-500">Click row to inspect</span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono">
              <thead>
                <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-500 bg-neutral-100/30 dark:bg-neutral-900/20">
                  <th className="py-2.5 px-4 font-normal w-24">Ticker</th>
                  <th className="py-2.5 px-4 font-normal">Company Name</th>
                  <th className="py-2.5 px-4 font-normal">CIK</th>
                  <th className="py-2.5 px-4 font-normal">SIC Industry</th>
                  <th className="py-2.5 px-4 font-normal text-right">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-200 dark:divide-neutral-900">
                {filteredDirectory.slice(0, 100).map((c) => {
                  const isCurrent = c.ticker === currentTicker
                  return (
                    <tr
                      key={c.ticker}
                      onClick={() => onSelectTicker(c.ticker)}
                      {...prefetchOnHover(c.ticker)}
                      className={`hover:bg-neutral-100/80 dark:hover:bg-neutral-900/60 cursor-pointer transition-colors ${
                        isCurrent
                          ? 'bg-neutral-100/90 dark:bg-neutral-900/80 font-medium'
                          : ''
                      }`}
                    >
                      <td className="py-2.5 px-4">
                        <span className="font-semibold text-neutral-900 dark:text-white">
                          {c.ticker}
                        </span>
                      </td>
                      <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-200 font-sans">
                        {c.name}
                      </td>
                      <td className="py-2.5 px-4 text-neutral-500">{c.cik}</td>
                      <td className="py-2.5 px-4 text-neutral-500 font-sans text-[11px]">
                        {c.sic_description || `SIC ${c.sic}`}
                      </td>
                      <td className="py-2.5 px-4 text-right">
                        <button
                          onClick={(e) => {
                            e.stopPropagation()
                            onSelectTicker(c.ticker)
                          }}
                          className="inline-flex items-center gap-1 text-xs text-neutral-500 hover:text-neutral-900 dark:hover:text-white transition-colors"
                        >
                          <span>Inspect</span>
                          <ArrowRight className="w-3 h-3" />
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ========================================================= */}
      {/* UNIVERSES VIEW (3,000+ EQUITIES COVERAGE) */}
      {/* ========================================================= */}
      {viewMode === 'universes' && (
        <div className="space-y-6">
          {/* Header Banner */}
          <div className="p-5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/30">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <Database className="w-4 h-4 text-neutral-900 dark:text-white" />
                  <h3 className="text-sm font-semibold text-neutral-900 dark:text-white">
                    Expanded Coverage: Russell 2000, MidCaps & Foreign ADRs
                  </h3>
                </div>
                <p className="text-xs text-neutral-600 dark:text-neutral-400 font-sans leading-relaxed max-w-3xl">
                  Fundamental stock pickers generate the highest alpha in small-caps, undercovered mid-caps, and international compounders.
                  The database supports plug-and-play universe ingestion with automatic rate-limiting compliance (8.5 req/sec SEC EDGAR threshold).
                </p>
              </div>
              <div className="text-xs font-mono px-3 py-2 rounded-lg bg-white dark:bg-neutral-950 border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 whitespace-nowrap self-start md:self-auto">
                <span className="font-semibold text-neutral-900 dark:text-white">{companies.length}</span> Active Synced Filers
              </div>
            </div>
          </div>

          {loadingUniverses ? (
            <div className="p-12 text-center text-xs font-mono text-neutral-500">
              Loading universe catalog...
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {universes.map((u) => {
                const pct = u.total > 0 ? Math.min(100, Math.round((u.synced / u.total) * 100)) : 0
                const syncCmd = `./sec sync --universe ${u.name} --skip-existing`
                const isCopied = copiedSyncCmd === u.name

                return (
                  <div
                    key={u.name}
                    className="p-5 rounded-xl border border-neutral-200 dark:border-neutral-800/80 bg-neutral-50/50 dark:bg-neutral-950/40 hover:border-neutral-300 dark:hover:border-neutral-700 transition-all flex flex-col justify-between"
                  >
                    <div>
                      <div className="flex items-start justify-between gap-2 mb-2">
                        <h4 className="text-sm font-medium text-neutral-900 dark:text-white capitalize">
                          {u.name.replace(/_/g, ' ')}
                        </h4>
                        <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-neutral-200/60 dark:bg-neutral-800 text-neutral-600 dark:text-neutral-300">
                          {u.filename}
                        </span>
                      </div>

                      <p className="text-xs text-neutral-500 dark:text-neutral-400 font-sans mb-4 min-h-[36px]">
                        {u.description}
                      </p>

                      {/* Coverage Progress */}
                      <div className="space-y-1.5 mb-4">
                        <div className="flex justify-between text-xs font-mono">
                          <span className="text-neutral-500">Coverage</span>
                          <span className="text-neutral-900 dark:text-white font-medium">
                            {u.synced} / {u.total} ({pct}%)
                          </span>
                        </div>
                        <div className="w-full h-1.5 bg-neutral-200 dark:bg-neutral-800 rounded-full overflow-hidden">
                          <div
                            className={`h-full transition-all duration-500 ${
                              pct === 100
                                ? 'bg-emerald-500'
                                : pct > 0
                                ? 'bg-neutral-900 dark:bg-white'
                                : 'bg-transparent'
                            }`}
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                      </div>
                    </div>

                    {/* Sync Action */}
                    <div className="pt-3 border-t border-neutral-200/80 dark:border-neutral-800/80">
                      <div className="flex items-center justify-between text-[11px] font-mono mb-1.5">
                        <span className="text-neutral-400">Sync Command</span>
                        <button
                          onClick={() => {
                            navigator.clipboard.writeText(syncCmd)
                            setCopiedSyncCmd(u.name)
                            setTimeout(() => setCopiedSyncCmd(null), 2000)
                          }}
                          className="text-neutral-500 hover:text-neutral-900 dark:hover:text-white flex items-center gap-1 transition-colors"
                        >
                          {isCopied ? (
                            <>
                              <Check className="w-3 h-3 text-emerald-500" />
                              <span className="text-emerald-600 dark:text-emerald-400">Copied!</span>
                            </>
                          ) : (
                            <>
                              <Copy className="w-3 h-3" />
                              <span>Copy CLI</span>
                            </>
                          )}
                        </button>
                      </div>
                      <div className="p-2 rounded bg-neutral-100 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 font-mono text-[11px] text-neutral-700 dark:text-neutral-300 select-all overflow-x-auto whitespace-nowrap">
                        {syncCmd}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* Quick instructions */}
          <div className="p-4 rounded-xl border border-dashed border-neutral-200 dark:border-neutral-800 text-xs font-mono text-neutral-500 space-y-2">
            <div className="font-semibold text-neutral-700 dark:text-neutral-300 flex items-center gap-1.5">
              <Sparkles className="w-3.5 h-3.5 text-neutral-500" />
              <span>Alpha Pipeline Tips</span>
            </div>
            <p className="font-sans text-neutral-600 dark:text-neutral-400 leading-relaxed">
              Use <code className="font-mono text-[11px] px-1 py-0.5 bg-neutral-100 dark:bg-neutral-900 rounded">--skip-existing</code> when running multiple sync jobs to instantly bypass tickers already parsed into <code className="font-mono text-[11px] px-1 py-0.5 bg-neutral-100 dark:bg-neutral-900 rounded">data/sec.db</code>. Run <code className="font-mono text-[11px] px-1 py-0.5 bg-neutral-100 dark:bg-neutral-900 rounded">./sec universe</code> from your terminal at any time to inspect coverage stats across all universe files.
            </p>
          </div>
        </div>
      )}
    </div>
  )
}
