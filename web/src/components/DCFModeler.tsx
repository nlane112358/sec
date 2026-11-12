import React, { useState, useEffect, useCallback, useMemo } from 'react'
import {
  Sliders,
  RefreshCw,
  AlertCircle,
  TrendingUp,
  Percent,
  Link,
  Check,
  DollarSign,
  ArrowRight,
  ShieldCheck,
  BarChart3,
  Play,
  Bookmark,
  Trash2,
  Plus,
} from 'lucide-react'
import { SummaryResponse, DCFResult, ReverseDCFResult, QuoteResponse } from '../types'
import { formatCurrency, formatShares, formatPercent } from '../lib/format'

interface DCFModelerProps {
  summary: SummaryResponse
  compact: boolean
}

type ValuationSubTab = 'forward' | 'reverse' | 'simulation'

const MONTE_CARLO_TRIALS = 10_000

export const DCFModeler: React.FC<DCFModelerProps> = ({ summary, compact }) => {
  const [subTab, setSubTab] = useState<ValuationSubTab>('forward')
  const [copiedLink, setCopiedLink] = useState(false)

  // -------------------------------------------------------------
  // Forward DCF State
  // -------------------------------------------------------------
  const [dcfResult, setDcfResult] = useState<DCFResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [growthRate, setGrowthRate] = useState<number | null>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.has('growth') ? parseFloat(p.get('growth')!) : null
  })
  const [discountRate, setDiscountRate] = useState<number | null>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.has('discount') ? parseFloat(p.get('discount')!) : null
  })
  const [terminalRate, setTerminalRate] = useState<number>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.has('terminal') ? parseFloat(p.get('terminal')!) : 2.5
  })
  const [years, setYears] = useState<number>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.has('years') ? parseInt(p.get('years')!, 10) : 5
  })
  const [mode, setMode] = useState<'fcf' | 'revenue'>(() => {
    const p = new URLSearchParams(window.location.search)
    return (p.get('mode') as 'fcf' | 'revenue') || 'fcf'
  })
  const [capexMode, setCapexMode] = useState<'raw' | 'normalized'>('raw')
  const [growthProfile, setGrowthProfile] = useState<'constant' | 'fade'>('constant')

  // Saved scenarios in localStorage
  const scenarioStorageKey = `sec_dcf_scenarios_${summary.ticker}`
  const [savedScenarios, setSavedScenarios] = useState<Array<{
    name: string
    growth: number
    discount: number
    terminal: number
    years: number
    mode: 'fcf' | 'revenue'
    growthProfile: 'constant' | 'fade'
  }>>(() => {
    try {
      const saved = localStorage.getItem(scenarioStorageKey)
      return saved ? JSON.parse(saved) : []
    } catch {
      return []
    }
  })

  const handleSaveScenario = () => {
    const name = window.prompt('Enter scenario name (e.g. Bull Case, Conservative, AI Upside):')
    if (!name || !name.trim()) return
    const newScenario = {
      name: name.trim(),
      growth: growthRate ?? 10,
      discount: discountRate ?? 8.5,
      terminal: terminalRate,
      years,
      mode,
      growthProfile,
    }
    const updated = [...savedScenarios.filter((s) => s.name !== newScenario.name), newScenario]
    setSavedScenarios(updated)
    try {
      localStorage.setItem(scenarioStorageKey, JSON.stringify(updated))
    } catch {}
  }

  const handleLoadScenario = (sc: (typeof savedScenarios)[0]) => {
    setGrowthRate(sc.growth)
    setDiscountRate(sc.discount)
    setTerminalRate(sc.terminal)
    setYears(sc.years)
    setMode(sc.mode)
    setGrowthProfile(sc.growthProfile || 'constant')
  }

  const handleDeleteScenario = (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    const updated = savedScenarios.filter((s) => s.name !== name)
    setSavedScenarios(updated)
    try {
      localStorage.setItem(scenarioStorageKey, JSON.stringify(updated))
    } catch {}
  }

  // -------------------------------------------------------------
  // Reverse DCF State
  // -------------------------------------------------------------
  const [reverseResult, setReverseResult] = useState<ReverseDCFResult | null>(null)
  const [loadingReverse, setLoadingReverse] = useState(false)
  const [reverseError, setReverseError] = useState<string | null>(null)

  const [sharePrice, setSharePrice] = useState<number>(150)
  const [reverseMode, setReverseMode] = useState<'owner' | 'fcf' | 'revenue'>('owner')
  const [reverseTerminal, setReverseTerminal] = useState<number>(2.5)
  const [reverseYears, setReverseYears] = useState<number>(5)

  // -------------------------------------------------------------
  // Scenarios & Monte Carlo State
  // -------------------------------------------------------------
  const [bearGrowthDelta, setBearGrowthDelta] = useState<number>(-5.0)
  const [bearDiscountDelta, setBearDiscountDelta] = useState<number>(1.5)
  const [bearWeight, setBearWeight] = useState<number>(25)

  const [baseWeight, setBaseWeight] = useState<number>(50)

  const [bullGrowthDelta, setBullGrowthDelta] = useState<number>(5.0)
  const [bullDiscountDelta, setBullDiscountDelta] = useState<number>(-0.75)
  const [bullWeight, setBullWeight] = useState<number>(25)

  const [monteCarloResults, setMonteCarloResults] = useState<number[]>([])

  // Extract latest shares count for price <-> market cap calculations
  const sharesMetric = summary.metrics.find(
    (m) =>
      m.Name === 'Shares Outstanding' || m.Name === 'Diluted Weighted Average Shares'
  )
  const latestShares =
    sharesMetric && sharesMetric.Values.length > 0 && sharesMetric.Values[0].Present
      ? sharesMetric.Values[0].Value
      : 1e9

  // Fetch live market quote to seed share price
  useEffect(() => {
    if (!summary?.ticker) return
    let ignore = false
    fetch(`/api/company/${summary.ticker}/quote`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data: QuoteResponse | null) => {
        if (!ignore && data && data.price > 0) {
          setSharePrice(parseFloat(data.price.toFixed(2)))
        }
      })
      .catch(() => {})
    return () => {
      ignore = true
    }
  }, [summary?.ticker])

  // Pure DCF Per-Share Valuation Helper with Growth Fading support
  const calculatePerShareValue = useCallback(
    (
      fcf: number,
      cash: number,
      debt: number,
      shares: number,
      gPct: number,
      rPct: number,
      tPct: number,
      nYears: number
    ): number => {
      if (shares <= 0 || fcf <= 0) return 0
      const g = gPct / 100
      const r = rPct / 100
      const t = tPct / 100
      if (r <= t) return 0

      let pvExplicit = 0
      let currentCF = fcf
      for (let y = 1; y <= nYears; y++) {
        const stepG = growthProfile === 'fade' ? g - ((y - 1) / nYears) * (g - t) : g
        currentCF = currentCF * (1 + stepG)
        pvExplicit += currentCF / Math.pow(1 + r, y)
      }

      const tv = (currentCF * (1 + t)) / (r - t)
      const pvTV = tv / Math.pow(1 + r, nYears)
      const ev = pvExplicit + pvTV
      const eq = ev + cash - debt
      const perShare = eq / shares
      return isFinite(perShare) && perShare > 0 ? perShare : 0
    },
    [growthProfile]
  )

  // Dynamic per-share value that reacts to growthProfile and slider adjustments
  const displayPerShareValue = useMemo(() => {
    if (!dcfResult || dcfResult.BaseFCF <= 0) return dcfResult?.ValuePerShare || 0
    if (growthProfile === 'constant') return dcfResult.ValuePerShare
    const fcf = dcfResult.BaseFCF
    const cash = dcfResult.Cash
    const debt = dcfResult.Debt
    const shares = dcfResult.SharesOutstanding
    const g = growthRate ?? dcfResult.GrowthRate * 100
    const r = discountRate ?? dcfResult.DiscountRate * 100
    const t = terminalRate
    const n = years
    const val = calculatePerShareValue(fcf, cash, debt, shares, g, r, t, n)
    return val > 0 ? val : dcfResult.ValuePerShare
  }, [dcfResult, growthProfile, growthRate, discountRate, terminalRate, years, calculatePerShareValue])

  // Monte Carlo Simulation Runner
  const runMonteCarlo = useCallback(() => {
    if (!dcfResult || dcfResult.BaseFCF <= 0) return
    const fcf = dcfResult.BaseFCF
    const cash = dcfResult.Cash
    const debt = dcfResult.Debt
    const shares = dcfResult.SharesOutstanding
    const t = terminalRate
    const nYears = years

    const mG = growthRate ?? (dcfResult.GrowthRate * 100)
    const sG = 3.5
    const mR = discountRate ?? (dcfResult.DiscountRate * 100)
    const sR = 1.0

    const runs: number[] = []
    for (let i = 0; i < MONTE_CARLO_TRIALS; i++) {
      const u1 = Math.max(1e-6, Math.random())
      const u2 = Math.random()
      const z1 = Math.sqrt(-2.0 * Math.log(u1)) * Math.cos(2.0 * Math.PI * u2)

      const gSim = mG + z1 * sG
      let rSim = mR + (Math.random() - 0.5) * 2 * sR
      if (rSim <= t + 0.5) rSim = t + 0.5

      const val = calculatePerShareValue(fcf, cash, debt, shares, gSim, rSim, t, nYears)
      if (val > 0) runs.push(val)
    }

    runs.sort((a, b) => a - b)
    setMonteCarloResults(runs)
  }, [dcfResult, growthRate, discountRate, terminalRate, years, calculatePerShareValue])

  // Run Monte Carlo when entering simulation tab or when dcfResult arrives
  useEffect(() => {
    if (dcfResult && subTab === 'simulation' && monteCarloResults.length === 0) {
      runMonteCarlo()
    }
  }, [subTab, dcfResult, monteCarloResults.length, runMonteCarlo])

  // Fetch Forward DCF
  useEffect(() => {
    let ignore = false
    const fetchDCF = async () => {
      setLoading(true)
      setError(null)
      try {
        const params = new URLSearchParams()
        if (growthRate !== null) params.set('growth', growthRate.toString())
        if (discountRate !== null) params.set('discount', discountRate.toString())
        params.set('terminal', terminalRate.toString())
        params.set('years', years.toString())
        params.set('mode', mode)
        params.set('capex_mode', capexMode)

        const res = await fetch(`/api/company/${summary.ticker}/dcf?${params.toString()}`)
        if (!res.ok) {
          const errData = await res.json().catch(() => ({}))
          throw new Error(errData.error || `HTTP error ${res.status}`)
        }
        const data: DCFResult = await res.json()
        if (!ignore) {
          setDcfResult(data)
          if (growthRate === null) {
            setGrowthRate(parseFloat((data.GrowthRate * 100).toFixed(1)))
          }
          if (discountRate === null) {
            setDiscountRate(parseFloat((data.DiscountRate * 100).toFixed(2)))
          }
          // Set initial default share price to the DCF per share price if default
          if (sharePrice === 150 && data.ValuePerShare > 0) {
            setSharePrice(parseFloat(data.ValuePerShare.toFixed(2)))
          }
        }
      } catch (err: any) {
        if (!ignore) {
          setError(err.message || 'Failed to calculate DCF')
        }
      } finally {
        if (!ignore) setLoading(false)
      }
    }

    fetchDCF()
    return () => {
      ignore = true
    }
  }, [summary.ticker, growthRate, discountRate, terminalRate, years, mode, capexMode])

  // Fetch Reverse DCF
  useEffect(() => {
    let ignore = false
    const fetchReverse = async () => {
      if (sharePrice <= 0) return
      setLoadingReverse(true)
      setReverseError(null)
      try {
        const marketCap = sharePrice * latestShares
        const params = new URLSearchParams({
          market_cap: marketCap.toString(),
          mode: reverseMode,
          terminal: reverseTerminal.toString(),
          years: reverseYears.toString(),
        })

        const res = await fetch(
          `/api/company/${summary.ticker}/reverse-dcf?${params.toString()}`
        )
        if (!res.ok) {
          const errData = await res.json().catch(() => ({}))
          throw new Error(errData.error || `HTTP error ${res.status}`)
        }
        const data: ReverseDCFResult = await res.json()
        if (!ignore) {
          setReverseResult(data)
        }
      } catch (err: any) {
        if (!ignore) {
          setReverseError(err.message || 'Failed to calculate reverse DCF')
        }
      } finally {
        if (!ignore) setLoadingReverse(false)
      }
    }

    fetchReverse()
    return () => {
      ignore = true
    }
  }, [summary.ticker, sharePrice, reverseMode, reverseTerminal, reverseYears, latestShares])

  const resetToDefaults = () => {
    setGrowthRate(null)
    setDiscountRate(null)
    setTerminalRate(2.5)
    setYears(5)
    setMode('fcf')
    setCapexMode('raw')
  }

  const copyShareableLink = () => {
    const params = new URLSearchParams()
    if (growthRate !== null) params.set('growth', growthRate.toString())
    if (discountRate !== null) params.set('discount', discountRate.toString())
    params.set('terminal', terminalRate.toString())
    params.set('years', years.toString())
    params.set('mode', mode)

    const url = `${window.location.origin}${window.location.pathname}#${summary.ticker}?${params.toString()}`
    navigator.clipboard.writeText(url)
    setCopiedLink(true)
    setTimeout(() => setCopiedLink(false), 2000)
  }

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* Sub-tab Navigation Strip */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-neutral-200 dark:border-neutral-800 text-xs font-mono">
        <div className="flex items-center gap-6">
          {[
            { id: 'forward', label: '1. Forward DCF' },
            { id: 'reverse', label: '2. Reverse DCF (Expectations)' },
            { id: 'simulation', label: '3. Scenarios & Monte Carlo' },
          ].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setSubTab(tab.id as any)}
              className={`pb-2.5 text-xs font-mono uppercase tracking-wider transition-colors border-b-2 -mb-px ${
                subTab === tab.id
                  ? 'border-neutral-900 dark:border-white text-neutral-950 dark:text-white font-semibold'
                  : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-300'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-2 pb-2">
          <button
            onClick={copyShareableLink}
            className="flex items-center gap-1.5 px-2.5 py-1 border border-neutral-300 dark:border-neutral-800 bg-white dark:bg-neutral-950 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
            title="Copy shareable link with parameters"
          >
            {copiedLink ? (
              <Check className="w-3.5 h-3.5 text-emerald-500" />
            ) : (
              <Link className="w-3.5 h-3.5" />
            )}
            <span>{copiedLink ? 'Link Copied' : 'Share Model'}</span>
          </button>

          {subTab === 'forward' && (
            <button
              onClick={resetToDefaults}
              className="flex items-center gap-1.5 px-2.5 py-1 border border-neutral-300 dark:border-neutral-800 bg-white dark:bg-neutral-950 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
            >
              <RefreshCw className={`w-3 h-3 ${loading ? 'animate-spin' : ''}`} />
              <span>Reset</span>
            </button>
          )}
        </div>
      </div>

      {/* ========================================================= */}
      {/* FORWARD DCF VIEW */}
      {/* ========================================================= */}
      {subTab === 'forward' && (
        <div className="space-y-4">
          {error && (
            <div className="p-3 border border-rose-300 dark:border-rose-900/40 bg-rose-50 dark:bg-rose-950/20 text-xs text-rose-600 dark:text-rose-300 flex items-center gap-2 font-mono">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Saved Scenarios Tray */}
          <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 border border-neutral-200 dark:border-neutral-800 bg-white/50 dark:bg-neutral-950/50 rounded-lg text-xs font-mono">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans mr-1 flex items-center gap-1">
                <Bookmark className="w-3 h-3 text-neutral-500" />
                Scenarios:
              </span>
              <button
                onClick={resetToDefaults}
                className="px-2 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 bg-neutral-100 dark:bg-neutral-800 text-neutral-800 dark:text-neutral-200 hover:bg-neutral-200 dark:hover:bg-neutral-700 text-[11px] transition-colors"
              >
                Consensus Baseline
              </button>
              {savedScenarios.map((sc) => (
                <div
                  key={sc.name}
                  onClick={() => handleLoadScenario(sc)}
                  className="group inline-flex items-center gap-1 px-2 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 text-neutral-700 dark:text-neutral-300 hover:border-neutral-400 dark:hover:border-neutral-500 cursor-pointer text-[11px] transition-colors"
                >
                  <span>
                    {sc.name} ({sc.growth}%)
                  </span>
                  <button
                    onClick={(e) => handleDeleteScenario(sc.name, e)}
                    className="opacity-0 group-hover:opacity-100 text-neutral-400 hover:text-rose-500 transition-opacity p-0.5"
                    title="Delete scenario"
                  >
                    <Trash2 className="w-2.5 h-2.5" />
                  </button>
                </div>
              ))}
            </div>

            <button
              onClick={handleSaveScenario}
              className="flex items-center gap-1 px-2 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-[11px] transition-colors"
            >
              <Plus className="w-3 h-3" />
              <span>Save Scenario</span>
            </button>
          </div>

          {/* Continuous Valuation Sheet: Drivers, Bridge, Projections & Grid */}
          <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-xl overflow-hidden">
            <div className="grid grid-cols-1 lg:grid-cols-12 divide-y lg:divide-y-0 lg:divide-x divide-neutral-200 dark:divide-neutral-800">
              {/* Left Column: Valuation Drivers & Assumptions (col-span-5) */}
              <div className="lg:col-span-5 flex flex-col justify-between">
                <div>
                  <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex items-center justify-between">
                    <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 font-medium">
                      Valuation Drivers
                    </span>
                    <div className="inline-flex border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 font-mono text-[11px]">
                      <button
                        onClick={() => setMode('fcf')}
                        className={`px-2.5 py-0.5 transition-colors ${
                          mode === 'fcf'
                            ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                            : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
                        }`}
                      >
                        FCF Growth
                      </button>
                      <button
                        onClick={() => setMode('revenue')}
                        className={`px-2.5 py-0.5 transition-colors ${
                          mode === 'revenue'
                            ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                            : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
                        }`}
                      >
                        Revenue FCFF
                      </button>
                    </div>
                  </div>

                  <div className="p-4 space-y-4 font-mono text-xs">
                    {/* Growth Rate Slider */}
                    <div className="space-y-1.5">
                      <div className="flex justify-between items-center">
                        <span className="text-neutral-600 dark:text-neutral-400">
                          {mode === 'revenue' ? 'Revenue Growth CAGR' : 'Annual Forecast FCF Growth'}
                        </span>
                        <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                          {growthRate !== null ? `${growthRate.toFixed(1)}%` : '—'}
                        </span>
                      </div>
                      <input
                        type="range"
                        min="-10"
                        max="40"
                        step="0.5"
                        value={growthRate ?? 10}
                        onChange={(e) => setGrowthRate(parseFloat(e.target.value))}
                        className="w-full h-1 bg-neutral-200 dark:bg-neutral-800 appearance-none cursor-pointer accent-neutral-900 dark:accent-neutral-100"
                      />
                      <div className="flex justify-between text-[10px] text-neutral-400 font-mono">
                        <span>-10%</span>
                        {dcfResult?.HasHistoricalFCFCAGR && mode === 'fcf' && (
                          <span className="text-neutral-600 dark:text-neutral-400">
                            Hist 3Y FCF: {(dcfResult.HistoricalFCFCAGR * 100).toFixed(1)}%
                          </span>
                        )}
                        {dcfResult?.HasHistoricalRevenueCAGR && mode === 'revenue' && (
                          <span className="text-neutral-600 dark:text-neutral-400">
                            Hist 3Y Rev: {(dcfResult.HistoricalRevenueCAGR * 100).toFixed(1)}%
                          </span>
                        )}
                        <span>+40%</span>
                      </div>
                    </div>

                    {/* Growth Profile: Flat vs Fade */}
                    <div className="flex items-center justify-between text-xs pt-1 border-t border-neutral-100 dark:border-neutral-900">
                      <span className="text-neutral-600 dark:text-neutral-400 font-sans">Growth Decay Profile</span>
                      <div className="inline-flex border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 font-mono text-[10px]">
                        <button
                          onClick={() => setGrowthProfile('constant')}
                          className={`px-2 py-0.5 transition-colors ${
                            growthProfile === 'constant'
                              ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                              : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
                          }`}
                        >
                          Flat {growthRate ?? 10}%
                        </button>
                        <button
                          onClick={() => setGrowthProfile('fade')}
                          className={`px-2 py-0.5 transition-colors ${
                            growthProfile === 'fade'
                              ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                              : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
                          }`}
                          title={`Fade linearly from ${growthRate ?? 10}% down to terminal rate ${terminalRate}%`}
                        >
                          Fade to {terminalRate}%
                        </button>
                      </div>
                    </div>

                    {/* Discount Rate / WACC Slider */}
                    <div className="space-y-1.5">
                      <div className="flex justify-between items-center">
                        <span className="text-neutral-600 dark:text-neutral-400">Discount Rate (WACC)</span>
                        <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                          {discountRate !== null ? `${discountRate.toFixed(2)}%` : '—'}
                        </span>
                      </div>
                      <input
                        type="range"
                        min="5"
                        max="18"
                        step="0.25"
                        value={discountRate ?? 8.5}
                        onChange={(e) => setDiscountRate(parseFloat(e.target.value))}
                        className="w-full h-1 bg-neutral-200 dark:bg-neutral-800 appearance-none cursor-pointer accent-neutral-900 dark:accent-neutral-100"
                      />
                      <div className="flex justify-between text-[10px] text-neutral-400 font-mono">
                        <span>5.0%</span>
                        {dcfResult?.WACC && (
                          <span className="text-neutral-600 dark:text-neutral-400">
                            Calculated WACC: {(dcfResult.WACC.Rate * 100).toFixed(2)}%
                          </span>
                        )}
                        <span>18.0%</span>
                      </div>
                    </div>

                    {/* Terminal Growth Rate */}
                    <div className="space-y-1.5">
                      <div className="flex justify-between items-center">
                        <span className="text-neutral-600 dark:text-neutral-400">Perpetual Terminal Growth</span>
                        <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                          {terminalRate.toFixed(1)}%
                        </span>
                      </div>
                      <input
                        type="range"
                        min="1.0"
                        max="4.0"
                        step="0.1"
                        value={terminalRate}
                        onChange={(e) => setTerminalRate(parseFloat(e.target.value))}
                        className="w-full h-1 bg-neutral-200 dark:bg-neutral-800 appearance-none cursor-pointer accent-neutral-900 dark:accent-neutral-100"
                      />
                      <div className="flex justify-between text-[10px] text-neutral-400 font-mono">
                        <span>1.0% (GDP floor)</span>
                        <span>2.5% default</span>
                        <span>4.0%</span>
                      </div>
                    </div>

                    {/* Forecast Horizon - Connected Segmented Control */}
                    <div className="space-y-1.5 pt-1">
                      <span className="text-[11px] text-neutral-600 dark:text-neutral-400 uppercase tracking-wider block">
                        Forecast Horizon
                      </span>
                      <div className="grid grid-cols-4 border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 text-center font-mono text-xs">
                        {[3, 5, 7, 10].map((yr) => (
                          <button
                            key={yr}
                            onClick={() => setYears(yr)}
                            className={`py-1 transition-colors ${
                              years === yr
                                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                                : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
                            }`}
                          >
                            {yr}Y
                          </button>
                        ))}
                      </div>
                    </div>

                    {/* CapEx Mode - Connected Segmented Control */}
                    <div className="space-y-1.5 pt-1">
                      <span className="text-[11px] text-neutral-600 dark:text-neutral-400 uppercase tracking-wider block">
                        CapEx Reinvestment Rule
                      </span>
                      <div className="grid grid-cols-2 border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 text-center font-mono text-xs">
                        <button
                          onClick={() => setCapexMode('raw')}
                          className={`py-1 transition-colors ${
                            capexMode === 'raw'
                              ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                              : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
                          }`}
                        >
                          Raw (Growth Fade)
                        </button>
                        <button
                          onClick={() => setCapexMode('normalized')}
                          className={`py-1 transition-colors ${
                            capexMode === 'normalized'
                              ? 'bg-neutral-900 text-white dark:bg-neutral-800 text-white font-semibold'
                              : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
                          }`}
                        >
                          Normalized to D&A
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              {/* Right Column: Valuation Output & EV to Equity Bridge (7 cols) */}
              <div className="lg:col-span-7 flex flex-col justify-between">
                <div>
                  <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex items-center justify-between text-xs font-mono">
                    <span className="text-[11px] uppercase tracking-wider text-neutral-500 font-medium">
                      Valuation Output & Equity Bridge
                    </span>
                    <span className="text-neutral-500">
                      Base FCF (FY {dcfResult?.BaseYear || '2026'}):{' '}
                      <span className="text-neutral-900 dark:text-white font-medium">
                        {formatCurrency(dcfResult?.BaseFCF, true, dcfResult?.Currency)}
                      </span>
                    </span>
                  </div>

                  {/* Intrinsic Value Hero Strip */}
                  <div className="p-4 border-b border-neutral-200 dark:border-neutral-800 flex flex-col sm:flex-row sm:items-baseline justify-between gap-2">
                    <div>
                      <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 block">
                        Intrinsic Value per Share
                      </span>
                      <div className="flex items-baseline gap-2 mt-1">
                        <span className="text-3xl font-mono font-bold tracking-tight text-neutral-950 dark:text-white">
                          {dcfResult ? `$${displayPerShareValue.toFixed(2)}` : 'Calculating...'}
                        </span>
                        <span className="text-xs text-neutral-400 font-mono">USD</span>
                        {growthProfile === 'fade' && (
                          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-indigo-50 dark:bg-indigo-950/40 text-indigo-600 dark:text-indigo-400 border border-indigo-200 dark:border-indigo-800">
                            Faded
                          </span>
                        )}
                      </div>
                    </div>
                    {dcfResult && sharePrice > 0 && (
                      <div className="text-left sm:text-right font-mono text-xs">
                        <div className="text-neutral-500 text-[11px]">Current Market Price</div>
                        <div className="font-semibold text-neutral-900 dark:text-white">
                          ${sharePrice.toFixed(2)}{' '}
                          <span
                            className={
                              displayPerShareValue >= sharePrice
                                ? 'text-emerald-600 dark:text-emerald-400 font-normal'
                                : 'text-rose-600 dark:text-rose-400 font-normal'
                            }
                          >
                            ({displayPerShareValue >= sharePrice ? '+' : ''}
                            {(((displayPerShareValue - sharePrice) / sharePrice) * 100).toFixed(1)}%)
                          </span>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* EV to Equity Bridge Table */}
                  <table className="w-full text-left font-mono text-xs divide-y divide-neutral-100 dark:divide-neutral-900">
                    <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400">
                          PV of Explicit Cash Flows ({years}Y)
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          {dcfResult && dcfResult.EnterpriseValue > 0
                            ? `${((dcfResult.PVExplicitCashFlows / dcfResult.EnterpriseValue) * 100).toFixed(1)}% of EV`
                            : ''}
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-neutral-900 dark:text-white">
                          {formatCurrency(dcfResult?.PVExplicitCashFlows, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400">
                          PV of Perpetual Terminal Value ({terminalRate.toFixed(1)}% growth)
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          {dcfResult && dcfResult.EnterpriseValue > 0
                            ? `${((dcfResult.PVTerminalValue / dcfResult.EnterpriseValue) * 100).toFixed(1)}% of EV`
                            : ''}
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-neutral-900 dark:text-white">
                          {formatCurrency(dcfResult?.PVTerminalValue, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                      <tr className="bg-neutral-50/40 dark:bg-neutral-900/20 font-semibold">
                        <td className="py-2 px-4 text-neutral-950 dark:text-white">
                          Enterprise Value (EV)
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          100.0%
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-950 dark:text-white">
                          {formatCurrency(dcfResult?.EnterpriseValue, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-emerald-600 dark:text-emerald-400">
                          (+) Cash & Cash Equivalents
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-400 text-[11px]">
                          Balance Sheet
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-emerald-600 dark:text-emerald-400">
                          +{formatCurrency(dcfResult?.Cash, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-rose-600 dark:text-rose-400">
                          (-) Total Debt ({dcfResult?.DebtSource || 'SEC Reported'})
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-400 text-[11px]">
                          Gross Debt
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-rose-600 dark:text-rose-400">
                          -{formatCurrency(dcfResult?.Debt, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                      <tr className="bg-neutral-50/60 dark:bg-neutral-900/40 font-semibold border-t border-neutral-200 dark:border-neutral-800">
                        <td className="py-2.5 px-4 text-neutral-950 dark:text-white">
                          (=) Implied Equity Value
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          {formatShares(dcfResult?.SharesOutstanding)} shares
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-950 dark:text-white">
                          {formatCurrency(dcfResult?.EquityValue, compact, dcfResult?.Currency)}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            </div>

            {/* Middle Section: Cash Flow Forecast Schedule */}
            {dcfResult && dcfResult.Projections && dcfResult.Projections.length > 0 && (
              <div className="border-t border-neutral-200 dark:border-neutral-800">
                <div className="px-4 py-2 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex justify-between items-center text-xs font-mono">
                  <span className="text-[11px] uppercase tracking-wider text-neutral-500 font-medium">
                    Cash Flow Forecast Schedule ({years} Years)
                  </span>
                  <span className="text-neutral-500 text-[11px]">
                    Discount Factor: (1 + WACC)^{'{t}'} • WACC = {(dcfResult.DiscountRate * 100).toFixed(2)}%
                  </span>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs font-mono">
                    <thead>
                      <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-500 text-[11px] uppercase">
                        <th className="py-2 px-4 font-normal">Line Item</th>
                        {dcfResult.Projections.map((p) => (
                          <th key={p.Year} className="py-2 px-4 font-normal text-right tabular-nums">
                            Year {p.Year}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                      <tr className="hover:bg-neutral-50/40 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-neutral-700 dark:text-neutral-300 font-medium">
                          Forecast Free Cash Flow
                        </td>
                        {dcfResult.Projections.map((p) => (
                          <td
                            key={p.Year}
                            className="py-2 px-4 text-right tabular-nums text-neutral-950 dark:text-white"
                          >
                            {formatCurrency(p.FreeCashFlow, compact, dcfResult.Currency)}
                          </td>
                        ))}
                      </tr>
                      <tr className="hover:bg-neutral-50/40 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-neutral-500">
                          Present Value (PV Discounted)
                        </td>
                        {dcfResult.Projections.map((p) => (
                          <td
                            key={p.Year}
                            className="py-2 px-4 text-right tabular-nums text-neutral-600 dark:text-neutral-400 font-medium"
                          >
                            {formatCurrency(p.PresentValue, compact, dcfResult.Currency)}
                          </td>
                        ))}
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {/* Bottom Section: Solid Two-Way Valuation Sensitivity Grid */}
            {dcfResult && dcfResult.Sensitivity && (
              <div className="border-t border-neutral-200 dark:border-neutral-800">
                <div className="px-4 py-2 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex justify-between items-center text-xs font-mono">
                  <div>
                    <span className="text-[11px] uppercase tracking-wider text-neutral-500 font-medium">
                      Two-Way Sensitivity Matrix
                    </span>
                    <span className="text-neutral-400 ml-2 text-[10px]">
                      (Discount Rate vs. Perpetual Terminal Growth Rate)
                    </span>
                  </div>
                  <span className="text-[11px] text-neutral-500 font-mono">
                    Click cell to adopt parameters
                  </span>
                </div>

                <div className="overflow-x-auto p-4">
                  <table className="w-full text-center text-xs font-mono border-collapse border border-neutral-200 dark:border-neutral-800">
                    <thead>
                      <tr className="bg-neutral-50/60 dark:bg-neutral-900/40">
                        <th className="py-2 px-3 text-left text-neutral-500 font-normal border border-neutral-200 dark:border-neutral-800 text-[11px] uppercase">
                          Discount Rate \ Terminal Growth
                        </th>
                        {dcfResult.Sensitivity.TerminalGrowthRates.map((tr) => (
                          <th
                            key={tr}
                            className="py-2 px-3 font-medium text-neutral-800 dark:text-neutral-200 border border-neutral-200 dark:border-neutral-800"
                          >
                            {(tr * 100).toFixed(1)}%
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {dcfResult.Sensitivity.DiscountRates.map((dr, rowIdx) => {
                        const isCurrentRow = Math.abs(dr - dcfResult.DiscountRate) < 0.001
                        return (
                          <tr key={dr}>
                            <td className="py-2 px-3 text-left text-neutral-600 dark:text-neutral-400 font-medium border border-neutral-200 dark:border-neutral-800 bg-neutral-50/30 dark:bg-neutral-900/20">
                              {(dr * 100).toFixed(2)}%
                            </td>
                            {dcfResult.Sensitivity.TerminalGrowthRates.map((tr, colIdx) => {
                              const val = dcfResult.Sensitivity.PerShare[rowIdx][colIdx]
                              const isCurrentCell =
                                isCurrentRow && Math.abs(tr - dcfResult.TerminalRate) < 0.001

                              return (
                                <td
                                  key={tr}
                                  onClick={() => {
                                    setDiscountRate(parseFloat((dr * 100).toFixed(2)))
                                    setTerminalRate(parseFloat((tr * 100).toFixed(1)))
                                  }}
                                  className={`py-2 px-3 text-right tabular-nums text-xs border border-neutral-200 dark:border-neutral-800 cursor-pointer transition-colors ${
                                    isCurrentCell
                                      ? 'bg-neutral-950 text-white dark:bg-white dark:text-black font-bold'
                                      : 'hover:bg-neutral-100 dark:hover:bg-neutral-900 text-neutral-800 dark:text-neutral-200'
                                  }`}
                                  title={`Adopt Discount ${(dr * 100).toFixed(2)}% & Terminal ${(tr * 100).toFixed(1)}%`}
                                >
                                  ${val.toFixed(2)}
                                </td>
                              )
                            })}
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ========================================================= */}
      {/* REVERSE DCF (EXPECTATIONS ARBITRAGE) VIEW */}
      {/* ========================================================= */}
      {subTab === 'reverse' && (
        <div className="space-y-6 animate-in fade-in duration-150">
          {reverseError && (
            <div className="p-3 border border-rose-300 dark:border-rose-900/40 bg-rose-50 dark:bg-rose-950/20 text-xs text-rose-600 dark:text-rose-300 flex items-center gap-2 font-mono">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>{reverseError}</span>
            </div>
          )}

          {/* Continuous Expectations Workstation */}
          <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md">
            {/* Header Strip */}
            <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs font-mono">
              <div>
                <span className="font-semibold text-neutral-950 dark:text-white uppercase tracking-wider text-[11px] block">
                  Reverse DCF • Market Expectations Arbitrage
                </span>
                <span className="text-neutral-500 text-[11px] font-sans">
                  Inverts valuation to solve for the implied cash flow CAGR priced into current capitalization vs. historical run-rate.
                </span>
              </div>
              <div className="text-[11px] text-neutral-400 font-mono">
                Solving: Enterprise Value = PV(Explicit) + PV(Terminal)
              </div>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-12 divide-y lg:divide-y-0 lg:divide-x divide-neutral-200 dark:divide-neutral-800">
              {/* Left Column: Market Valuation Input (5 cols) */}
              <div className="lg:col-span-5 p-4 space-y-4 font-mono text-xs">
                <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 font-medium block border-b border-neutral-200 dark:border-neutral-800 pb-2">
                  Market Pricing & Driver Inputs
                </span>

                {/* Stock Price Input */}
                <div className="space-y-1.5">
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-neutral-600 dark:text-neutral-400">Current Share Price ($)</span>
                    <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                      ${sharePrice.toFixed(2)}
                    </span>
                  </div>
                  <div className="relative">
                    <DollarSign className="w-4 h-4 text-neutral-400 absolute left-3 top-2" />
                    <input
                      type="number"
                      step="1"
                      min="1"
                      value={sharePrice}
                      onChange={(e) => setSharePrice(Math.max(1, parseFloat(e.target.value) || 1))}
                      className="w-full bg-white dark:bg-neutral-950 border border-neutral-300 dark:border-neutral-800 pl-8 pr-3 py-1.5 text-xs text-neutral-900 dark:text-neutral-100 font-mono outline-none focus:border-neutral-500"
                    />
                  </div>
                  <div className="flex justify-between text-[11px] text-neutral-400 font-mono pt-0.5">
                    <span>Implied Market Cap:</span>
                    <span className="text-neutral-900 dark:text-white font-medium">
                      {formatCurrency(sharePrice * latestShares, true, 'USD')}
                    </span>
                  </div>
                </div>

                {/* Reverse Mode Selector */}
                <div className="space-y-1.5 pt-1">
                  <span className="text-[11px] text-neutral-600 dark:text-neutral-400 uppercase tracking-wider block">
                    Cash Flow Engine
                  </span>
                  <div className="grid grid-cols-3 border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 text-center font-mono text-xs">
                    {[
                      { id: 'owner', label: 'Owner FCF' },
                      { id: 'fcf', label: 'Raw FCF' },
                      { id: 'revenue', label: 'Rev FCFF' },
                    ].map((m) => (
                      <button
                        key={m.id}
                        onClick={() => setReverseMode(m.id as any)}
                        className={`py-1 transition-colors text-[11px] ${
                          reverseMode === m.id
                            ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                            : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
                        }`}
                      >
                        {m.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Terminal Rate Slider */}
                <div className="space-y-1.5">
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-neutral-600 dark:text-neutral-400">Terminal Growth Rate</span>
                    <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                      {reverseTerminal.toFixed(1)}%
                    </span>
                  </div>
                  <input
                    type="range"
                    min="1.0"
                    max="4.0"
                    step="0.1"
                    value={reverseTerminal}
                    onChange={(e) => setReverseTerminal(parseFloat(e.target.value))}
                    className="w-full h-1 bg-neutral-200 dark:bg-neutral-800 appearance-none cursor-pointer accent-neutral-900 dark:accent-neutral-100"
                  />
                  <div className="flex justify-between text-[10px] text-neutral-400 font-mono">
                    <span>1.0%</span>
                    <span>2.5% default</span>
                    <span>4.0%</span>
                  </div>
                </div>

                {/* Forecast Years */}
                <div className="space-y-1.5 pt-1">
                  <span className="text-[11px] text-neutral-600 dark:text-neutral-400 uppercase tracking-wider block">
                    Forecast Horizon
                  </span>
                  <div className="grid grid-cols-4 border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 text-center font-mono text-xs">
                    {[3, 5, 7, 10].map((yr) => (
                      <button
                        key={yr}
                        onClick={() => setReverseYears(yr)}
                        className={`py-1 transition-colors ${
                          reverseYears === yr
                            ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                            : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
                        }`}
                      >
                        {yr}Y
                      </button>
                    ))}
                  </div>
                </div>
              </div>

              {/* Right Column: Implied Growth & Expectations Schedule (7 cols) */}
              <div className="lg:col-span-7 flex flex-col justify-between">
                <div>
                  <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex items-center justify-between text-xs font-mono">
                    <span className="text-[11px] uppercase tracking-wider text-neutral-500 font-medium">
                      Market Expectations & Spread Schedule
                    </span>
                    <span className="text-neutral-500">
                      Calculated WACC:{' '}
                      <span className="text-neutral-900 dark:text-white font-medium">
                        {reverseResult ? `${(reverseResult.WACC * 100).toFixed(2)}%` : '—'}
                      </span>
                    </span>
                  </div>

                  {/* Headline Implied Growth Strip */}
                  <div className="p-4 border-b border-neutral-200 dark:border-neutral-800 flex flex-col sm:flex-row sm:items-baseline justify-between gap-2">
                    <div>
                      <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 block">
                        Market-Implied Annual Growth Rate
                      </span>
                      <div className="flex items-baseline gap-2 mt-1">
                        <span className="text-3xl font-mono font-bold tracking-tight text-neutral-950 dark:text-white">
                          {reverseResult
                            ? `${(reverseResult.ImpliedGrowth * 100).toFixed(1)}%`
                            : 'Solving...'}
                        </span>
                        <span className="text-xs text-neutral-400 font-mono">/ yr required CAGR</span>
                      </div>
                      <span className="text-[11px] text-neutral-500 font-sans block mt-1">
                        Compounded cash flow growth over next {reverseYears} years required to justify ${sharePrice.toFixed(2)}.
                      </span>
                    </div>
                  </div>

                  {/* Expectations Schedule Table */}
                  <table className="w-full text-left font-mono text-xs divide-y divide-neutral-100 dark:divide-neutral-900">
                    <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2.5 px-4 text-neutral-600 dark:text-neutral-400">
                          Historical 3-Year Benchmark CAGR
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          Trailing 3Y Run-Rate
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums font-semibold text-neutral-900 dark:text-white">
                          {reverseResult && reverseResult.HasHistoricalCAGR
                            ? `${(reverseResult.HistoricalCAGR * 100).toFixed(1)}%`
                            : '—'}
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2.5 px-4 text-neutral-600 dark:text-neutral-400">
                          Expectation Alpha Spread (Market vs Historical)
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          Implied - Historical
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-bold ${
                            reverseResult && reverseResult.AlphaSpread >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-amber-600 dark:text-amber-400'
                          }`}
                        >
                          {reverseResult
                            ? `${reverseResult.AlphaSpread >= 0 ? '+' : ''}${(reverseResult.AlphaSpread * 100).toFixed(1)}%`
                            : '—'}
                        </td>
                      </tr>

                      <tr className="bg-neutral-50/40 dark:bg-neutral-900/20 font-semibold">
                        <td className="py-2 px-4 text-neutral-950 dark:text-white">
                          Target Enterprise Value
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          Market Cap + Net Debt
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-950 dark:text-white">
                          {formatCurrency(reverseResult?.EnterpriseValue, compact, reverseResult?.Unit)}
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400">
                          Target Market Capitalization
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-400 text-[11px]">
                          {formatShares(latestShares)} shares
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-neutral-900 dark:text-white">
                          {formatCurrency(reverseResult?.MarketCap, compact, reverseResult?.Unit)}
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-rose-600 dark:text-rose-400">
                          (+) Reported Gross Debt
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-400 text-[11px]">
                          SEC Balance Sheet
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-rose-600 dark:text-rose-400">
                          +{formatCurrency(reverseResult?.Debt, compact, reverseResult?.Unit)}
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2 px-4 text-emerald-600 dark:text-emerald-400">
                          (-) Cash & Equivalents
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums text-neutral-400 text-[11px]">
                          Liquid Reserves
                        </td>
                        <td className="py-2 px-4 text-right tabular-nums font-medium text-emerald-600 dark:text-emerald-400">
                          -{formatCurrency(reverseResult?.Cash, compact, reverseResult?.Unit)}
                        </td>
                      </tr>

                      <tr className="bg-neutral-50/60 dark:bg-neutral-900/40 border-t border-neutral-200 dark:border-neutral-800">
                        <td className="py-2.5 px-4 text-neutral-950 dark:text-white font-medium">
                          Intrinsic Value at Historical Run-Rate
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          {reverseResult && reverseResult.HasHistoricalCAGR
                            ? `At ${(reverseResult.HistoricalCAGR * 100).toFixed(1)}% growth`
                            : ''}
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums font-semibold text-neutral-950 dark:text-white">
                          {formatCurrency(reverseResult?.IntrinsicAtHistCAGR, compact, reverseResult?.Unit)}
                        </td>
                      </tr>

                      <tr className="bg-neutral-50/60 dark:bg-neutral-900/40 font-semibold border-t border-neutral-200 dark:border-neutral-800">
                        <td className="py-2.5 px-4 text-neutral-950 dark:text-white">
                          Margin of Safety vs Current Price
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-500 text-[11px]">
                          Discount / Premium
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-bold ${
                            reverseResult && reverseResult.MarginOfSafety >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-rose-600 dark:text-rose-400'
                          }`}
                        >
                          {reverseResult ? formatPercent(reverseResult.MarginOfSafety) : '—'}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ========================================================= */}
      {/* SCENARIOS & MONTE CARLO SIMULATION VIEW */}
      {/* ========================================================= */}
      {subTab === 'simulation' && (
        <div className="space-y-6 animate-in fade-in duration-150">
          {/* Part 1: Continuous Multi-Scenario Valuation Grid */}
          {(() => {
            const baseG = growthRate ?? (dcfResult?.GrowthRate ? dcfResult.GrowthRate * 100 : 10)
            const baseR = discountRate ?? (dcfResult?.DiscountRate ? dcfResult.DiscountRate * 100 : 8.5)
            const baseT = terminalRate
            const baseFCF = dcfResult?.BaseFCF || 0
            const cash = dcfResult?.Cash || 0
            const debt = dcfResult?.Debt || 0
            const shares = dcfResult?.SharesOutstanding || latestShares

            const bearG = baseG + bearGrowthDelta
            const bearR = baseR + bearDiscountDelta
            const bearT = Math.max(1.0, baseT - 1.0)
            const bearVal = calculatePerShareValue(baseFCF, cash, debt, shares, bearG, bearR, bearT, years)
            const bearSpread = sharePrice > 0 ? ((bearVal - sharePrice) / sharePrice) * 100 : 0

            const baseVal = calculatePerShareValue(baseFCF, cash, debt, shares, baseG, baseR, baseT, years)
            const baseSpread = sharePrice > 0 ? ((baseVal - sharePrice) / sharePrice) * 100 : 0

            const bullG = baseG + bullGrowthDelta
            const bullR = Math.max(3.5, baseR + bullDiscountDelta)
            const bullT = baseT + 0.5
            const bullVal = calculatePerShareValue(baseFCF, cash, debt, shares, bullG, bullR, bullT, years)
            const bullSpread = sharePrice > 0 ? ((bullVal - sharePrice) / sharePrice) * 100 : 0

            const wSum = bearWeight + baseWeight + bullWeight
            const weightedVal =
              wSum > 0
                ? (bearVal * bearWeight + baseVal * baseWeight + bullVal * bullWeight) / wSum
                : baseVal
            const weightedSpread = sharePrice > 0 ? ((weightedVal - sharePrice) / sharePrice) * 100 : 0

            const weightedG =
              wSum > 0
                ? (bearG * bearWeight + baseG * baseWeight + bullG * bullWeight) / wSum
                : baseG
            const weightedR =
              wSum > 0
                ? (bearR * bearWeight + baseR * baseWeight + bullR * bullWeight) / wSum
                : baseR
            const weightedT =
              wSum > 0
                ? (bearT * bearWeight + baseT * baseWeight + bullT * bullWeight) / wSum
                : baseT

            return (
              <div className="border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-950">
                {/* Section Header */}
                <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                  <div>
                    <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 font-medium">
                      1. Probability-Weighted Scenario Matrix
                    </span>
                    <p className="text-[11px] text-neutral-400 font-sans mt-0.5">
                      Tri-scenario framework (Bear, Base, Bull) with weighted outcome calculation.
                    </p>
                  </div>
                  <div className="flex items-center gap-2 font-mono text-xs">
                    <span className="text-neutral-500 text-[11px]">Market Reference:</span>
                    <span className="font-semibold text-neutral-900 dark:text-white tabular-nums">
                      ${sharePrice.toFixed(2)}
                    </span>
                  </div>
                </div>

                {/* Scenario Comparison Table */}
                <div className="overflow-x-auto">
                  <table className="w-full text-left font-mono text-xs border-collapse">
                    <thead>
                      <tr className="bg-neutral-50/40 dark:bg-neutral-900/20 border-b border-neutral-200 dark:border-neutral-800 text-[11px] text-neutral-500 uppercase tracking-wider">
                        <th className="py-2.5 px-4 font-medium text-neutral-700 dark:text-neutral-300">
                          Model Parameter & Output
                        </th>
                        <th className="py-2.5 px-4 text-right font-medium">
                          <span className="text-neutral-900 dark:text-white">Bear</span>
                          <span className="text-neutral-400 font-normal ml-1">({bearWeight}%)</span>
                        </th>
                        <th className="py-2.5 px-4 text-right font-medium">
                          <span className="text-neutral-900 dark:text-white">Base</span>
                          <span className="text-neutral-400 font-normal ml-1">({baseWeight}%)</span>
                        </th>
                        <th className="py-2.5 px-4 text-right font-medium">
                          <span className="text-neutral-900 dark:text-white">Bull</span>
                          <span className="text-neutral-400 font-normal ml-1">({bullWeight}%)</span>
                        </th>
                        <th className="py-2.5 px-4 text-right font-medium bg-neutral-100/60 dark:bg-neutral-900/60 text-neutral-950 dark:text-white">
                          <span>Weighted Target</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300">
                          FCF Growth CAGR ({years}Y)
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bearG.toFixed(1)}%{' '}
                          <span className="text-[10px] text-neutral-400">
                            ({bearGrowthDelta > 0 ? '+' : ''}{bearGrowthDelta}%)
                          </span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                          {baseG.toFixed(1)}% <span className="text-[10px] text-neutral-400">(Base)</span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bullG.toFixed(1)}%{' '}
                          <span className="text-[10px] text-neutral-400">
                            (+{bullGrowthDelta}%)
                          </span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium bg-neutral-100/30 dark:bg-neutral-900/30">
                          {weightedG.toFixed(1)}%
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300">
                          Discount Rate (WACC)
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bearR.toFixed(2)}%{' '}
                          <span className="text-[10px] text-neutral-400">
                            (+{bearDiscountDelta}%)
                          </span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                          {baseR.toFixed(2)}% <span className="text-[10px] text-neutral-400">(WACC)</span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bullR.toFixed(2)}%{' '}
                          <span className="text-[10px] text-neutral-400">
                            ({bullDiscountDelta}%)
                          </span>
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium bg-neutral-100/30 dark:bg-neutral-900/30">
                          {weightedR.toFixed(2)}%
                        </td>
                      </tr>

                      <tr className="hover:bg-neutral-50/50 dark:hover:bg-neutral-900/30">
                        <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300">
                          Perpetual Terminal Growth
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bearT.toFixed(1)}%
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                          {baseT.toFixed(1)}%
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white">
                          {bullT.toFixed(1)}%
                        </td>
                        <td className="py-2.5 px-4 text-right tabular-nums text-neutral-900 dark:text-white font-medium bg-neutral-100/30 dark:bg-neutral-900/30">
                          {weightedT.toFixed(1)}%
                        </td>
                      </tr>

                      {/* Section Separator: Valuation Output */}
                      <tr className="bg-neutral-50/80 dark:bg-neutral-900/50 border-t-2 border-neutral-200 dark:border-neutral-800">
                        <td className="py-3 px-4 font-semibold text-neutral-950 dark:text-white">
                          Implied Intrinsic Value / Share
                        </td>
                        <td className="py-3 px-4 text-right tabular-nums font-semibold text-neutral-900 dark:text-white">
                          ${bearVal.toFixed(2)}
                        </td>
                        <td className="py-3 px-4 text-right tabular-nums font-semibold text-neutral-900 dark:text-white">
                          ${baseVal.toFixed(2)}
                        </td>
                        <td className="py-3 px-4 text-right tabular-nums font-semibold text-neutral-900 dark:text-white">
                          ${bullVal.toFixed(2)}
                        </td>
                        <td className="py-3 px-4 text-right tabular-nums text-base font-bold text-neutral-950 dark:text-white bg-neutral-100/80 dark:bg-neutral-900/80">
                          ${weightedVal.toFixed(2)}
                        </td>
                      </tr>

                      <tr className="bg-neutral-50/40 dark:bg-neutral-900/20 border-t border-neutral-200 dark:border-neutral-800">
                        <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300">
                          Spread vs Market (${sharePrice.toFixed(2)})
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-medium ${
                            bearSpread >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-rose-600 dark:text-rose-400'
                          }`}
                        >
                          {bearSpread >= 0 ? '+' : ''}
                          {bearSpread.toFixed(1)}%
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-medium ${
                            baseSpread >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-rose-600 dark:text-rose-400'
                          }`}
                        >
                          {baseSpread >= 0 ? '+' : ''}
                          {baseSpread.toFixed(1)}%
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-medium ${
                            bullSpread >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-rose-600 dark:text-rose-400'
                          }`}
                        >
                          {bullSpread >= 0 ? '+' : ''}
                          {bullSpread.toFixed(1)}%
                        </td>
                        <td
                          className={`py-2.5 px-4 text-right tabular-nums font-bold bg-neutral-100/50 dark:bg-neutral-900/50 ${
                            weightedSpread >= 0
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-rose-600 dark:text-rose-400'
                          }`}
                        >
                          {weightedSpread >= 0 ? '+' : ''}
                          {weightedSpread.toFixed(1)}%
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                {/* Diagnostic strip at bottom */}
                <div className="px-4 py-2 bg-neutral-50/40 dark:bg-neutral-900/20 border-t border-neutral-200 dark:border-neutral-800 flex flex-wrap items-center justify-between gap-3 text-[11px] font-mono text-neutral-500">
                  <div className="flex items-center gap-4">
                    <span>
                      Weights:{' '}
                      <strong className="text-neutral-800 dark:text-neutral-200">
                        {bearWeight}% Bear / {baseWeight}% Base / {bullWeight}% Bull
                      </strong>
                    </span>
                    <span>•</span>
                    <span>
                      Expected Return Spread:{' '}
                      <strong
                        className={
                          weightedSpread >= 0
                            ? 'text-emerald-600 dark:text-emerald-400'
                            : 'text-rose-600 dark:text-rose-400'
                        }
                      >
                        {weightedSpread >= 0 ? '+' : ''}
                        {weightedSpread.toFixed(1)}% Margin of Safety
                      </strong>
                    </span>
                  </div>
                  <div className="text-neutral-400 text-[10px]">
                    Probability-weighted linear expectation across discrete macro regimes
                  </div>
                </div>
              </div>
            )
          })()}

          {/* Part 2: Monte Carlo Stochastic Distribution */}
          <div className="border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-950">
            {/* Header banner */}
            <div className="px-4 py-2.5 bg-neutral-50/60 dark:bg-neutral-900/40 border-b border-neutral-200 dark:border-neutral-800 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div>
                <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 font-medium">
                  2. Monte Carlo Stochastic Distribution ({MONTE_CARLO_TRIALS.toLocaleString()} Iterations)
                </span>
                <p className="text-[11px] text-neutral-400 font-sans mt-0.5">
                  Gaussian perturbation over growth rate (σ = 3.5%) and cost of capital (uniform ±1.0%).
                </p>
              </div>

              <button
                onClick={runMonteCarlo}
                className="flex items-center gap-1.5 px-2.5 py-1 text-xs font-mono border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-800 dark:text-neutral-200 transition-colors self-start sm:self-auto"
              >
                <Play className="w-3 h-3 text-neutral-600 dark:text-neutral-400 fill-neutral-600 dark:fill-neutral-400" />
                <span>Re-run {MONTE_CARLO_TRIALS.toLocaleString()} Trials</span>
              </button>
            </div>

            {monteCarloResults.length > 0 && (() => {
              const sorted = monteCarloResults
              const p10 = sorted[Math.floor(sorted.length * 0.10)]
              const p50 = sorted[Math.floor(sorted.length * 0.50)]
              const p90 = sorted[Math.floor(sorted.length * 0.90)]
              const probUpside = (sorted.filter((v) => v > sharePrice).length / sorted.length) * 100

              // 24 bins for SVG histogram
              const minVal = sorted[Math.floor(sorted.length * 0.02)]
              const maxVal = sorted[Math.floor(sorted.length * 0.98)]
              const range = Math.max(1, maxVal - minVal)
              const numBins = 24
              const binWidth = range / numBins
              const bins = Array.from({ length: numBins }, () => 0)

              sorted.forEach((v) => {
                if (v >= minVal && v <= maxVal) {
                  const idx = Math.min(numBins - 1, Math.floor((v - minVal) / binWidth))
                  bins[idx]++
                }
              })
              const maxBin = Math.max(1, ...bins)

              return (
                <div>
                  {/* Connected Metric Strip: 4 cells with hairline dividers */}
                  <div className="grid grid-cols-2 sm:grid-cols-4 divide-y sm:divide-y-0 sm:divide-x divide-neutral-200 dark:divide-neutral-800 border-b border-neutral-200 dark:border-neutral-800 font-mono text-xs">
                    <div className="p-3.5 bg-white dark:bg-neutral-950">
                      <span className="text-[10px] text-neutral-500 uppercase tracking-wider block font-sans">
                        P10 (Bear Floor)
                      </span>
                      <div className="text-lg font-bold tabular-nums text-neutral-900 dark:text-white mt-1">
                        ${p10.toFixed(2)}
                      </div>
                      <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                        10% probability below
                      </span>
                    </div>

                    <div className="p-3.5 bg-white dark:bg-neutral-950">
                      <span className="text-[10px] text-neutral-500 uppercase tracking-wider block font-sans">
                        P50 (Median Value)
                      </span>
                      <div className="text-lg font-bold tabular-nums text-neutral-950 dark:text-white mt-1">
                        ${p50.toFixed(2)}
                      </div>
                      <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                        50th percentile outcome
                      </span>
                    </div>

                    <div className="p-3.5 bg-white dark:bg-neutral-950">
                      <span className="text-[10px] text-neutral-500 uppercase tracking-wider block font-sans">
                        P90 (Bull Target)
                      </span>
                      <div className="text-lg font-bold tabular-nums text-neutral-900 dark:text-white mt-1">
                        ${p90.toFixed(2)}
                      </div>
                      <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                        90% probability below
                      </span>
                    </div>

                    <div className="p-3.5 bg-white dark:bg-neutral-950">
                      <span className="text-[10px] text-neutral-500 uppercase tracking-wider block font-sans">
                        Probability of Upside
                      </span>
                      <div
                        className={`text-lg font-bold tabular-nums mt-1 ${
                          probUpside >= 50
                            ? 'text-emerald-600 dark:text-emerald-400'
                            : 'text-amber-600 dark:text-amber-400'
                        }`}
                      >
                        {probUpside.toFixed(1)}%
                      </div>
                      <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                        P(Fair Value &gt; ${sharePrice.toFixed(2)})
                      </span>
                    </div>
                  </div>

                  {/* Histogram Area */}
                  <div className="p-4 space-y-3 bg-white dark:bg-neutral-950">
                    <div className="flex items-center justify-between text-xs font-mono text-neutral-500">
                      <span className="text-[11px] uppercase tracking-wider font-medium text-neutral-600 dark:text-neutral-400">
                        Valuation Probability Density ($/share)
                      </span>
                      <div className="flex items-center gap-4 text-[11px]">
                        <span className="flex items-center gap-1.5">
                          <span className="w-2.5 h-2.5 bg-emerald-600 dark:bg-emerald-500" />
                          <span>Above Market (${sharePrice.toFixed(2)})</span>
                        </span>
                        <span className="flex items-center gap-1.5">
                          <span className="w-2.5 h-2.5 bg-neutral-300 dark:bg-neutral-700" />
                          <span>Below Market</span>
                        </span>
                      </div>
                    </div>

                    {/* Histogram Render: Flat top bars, tight hairline styling */}
                    <div className="h-44 w-full relative flex items-end gap-1 pt-6 pb-2 border-b border-neutral-200 dark:border-neutral-800">
                      {bins.map((count, idx) => {
                        const binCenter = minVal + (idx + 0.5) * binWidth
                        const heightPct = (count / maxBin) * 100
                        const isAboveMarket = binCenter >= sharePrice

                        return (
                          <div
                            key={idx}
                            className="flex-1 flex flex-col items-center justify-end h-full group relative"
                          >
                            <div
                              style={{ height: `${Math.max(4, heightPct)}%` }}
                              className={`w-full transition-all ${
                                isAboveMarket
                                  ? 'bg-emerald-600/80 dark:bg-emerald-500/80 group-hover:bg-emerald-500'
                                  : 'bg-neutral-300 dark:bg-neutral-700 group-hover:bg-neutral-400 dark:group-hover:bg-neutral-600'
                              }`}
                            />
                            {/* Hover tooltip */}
                            <div className="opacity-0 group-hover:opacity-100 pointer-events-none absolute -top-8 bg-neutral-950 text-white dark:bg-white dark:text-black text-[10px] font-mono px-1.5 py-0.5 border border-neutral-700 dark:border-neutral-300 whitespace-nowrap z-10 transition-opacity">
                              ${binCenter.toFixed(0)} • {count} runs
                            </div>
                          </div>
                        )
                      })}
                    </div>

                    {/* Bottom X-Axis labels */}
                    <div className="flex justify-between text-[11px] font-mono text-neutral-400 pt-1">
                      <span>${minVal.toFixed(0)} (Min)</span>
                      <span>P10: ${p10.toFixed(0)}</span>
                      <span className="font-semibold text-neutral-900 dark:text-white">
                        Median: ${p50.toFixed(0)}
                      </span>
                      <span>P90: ${p90.toFixed(0)}</span>
                      <span>${maxVal.toFixed(0)} (Max)</span>
                    </div>
                  </div>
                </div>
              )
            })()}
          </div>
        </div>
      )}
    </div>
  )
}
