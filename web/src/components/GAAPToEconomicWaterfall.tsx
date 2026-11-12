import React, { useState, useMemo } from 'react'
import {
  Layers,
  ArrowRight,
  TrendingUp,
  Percent,
  Sparkles,
  Info,
  DollarSign,
  HelpCircle,
  Activity,
  CheckCircle2,
  Workflow,
  BarChart2,
} from 'lucide-react'
import { SummaryResponse, SummaryValue } from '../types'
import { formatCurrency, formatPercent } from '../lib/format'

interface GAAPToEconomicWaterfallProps {
  summary: SummaryResponse
  compact?: boolean
  className?: string
}

type BridgeViewMode = 'nopat' | 'invested_cap' | 'sankey'

export const GAAPToEconomicWaterfall: React.FC<GAAPToEconomicWaterfallProps> = ({
  summary,
  compact = false,
  className = '',
}) => {
  const { intangibles, periods } = summary
  const curr = intangibles.Currency || 'USD'

  const [viewMode, setViewMode] = useState<BridgeViewMode>('nopat')
  const [selectedPeriodIdx, setSelectedPeriodIdx] = useState<number>(0)
  const [taxRate, setTaxRate] = useState<number>(21.0)
  const [hoveredStepIndex, setHoveredStepIndex] = useState<number | null>(null)

  // Safe metric extraction helper for a specific period index
  const getMetricAt = (metricName: string, idx: number): number => {
    const m = summary.metrics.find((x) => x.Name === metricName)
    if (!m || !m.Values || m.Values.length <= idx || !m.Values[idx]?.Present) return 0
    return m.Values[idx].Value
  }

  const getIntangibleValAt = (arr: SummaryValue[] | undefined, idx: number): number => {
    if (!arr || arr.length <= idx || !arr[idx]?.Present) return 0
    return arr[idx].Value
  }

  // Current selected period values
  const rev = getMetricAt('Revenue', selectedPeriodIdx)
  const gaapEBIT = getMetricAt('Operating Income', selectedPeriodIdx)
  const rd = getMetricAt('Research & Development', selectedPeriodIdx)
  const sga = getMetricAt('Selling, General & Administrative', selectedPeriodIdx)
  const brandCapitalized = sga * 0.3
  const intangibleAmort = getIntangibleValAt(intangibles.IntangibleAmort, selectedPeriodIdx)

  // Adjusted EBIT from data or calculated
  const rawAdjEBIT = getIntangibleValAt(intangibles.AdjustedEBIT, selectedPeriodIdx)
  const adjEBIT = rawAdjEBIT !== 0 ? rawAdjEBIT : gaapEBIT + rd + brandCapitalized - intangibleAmort

  // Economic NOPAT
  const taxFactor = taxRate / 100
  const normalizedTax = Math.max(0, adjEBIT * taxFactor)
  const economicNOPAT = adjEBIT - normalizedTax

  // GAAP NOPAT
  const gaapNOPAT = Math.max(0, gaapEBIT * (1 - taxFactor))

  // Balance sheet metrics
  const equity = getMetricAt("Stockholders' Equity", selectedPeriodIdx)
  const debt = getMetricAt('Debt / Borrowings', selectedPeriodIdx)
  const cash = getMetricAt('Cash & Equivalents', selectedPeriodIdx)
  const gaapInvestedCap = Math.max(1, equity + debt - cash)

  const intangibleCap = getIntangibleValAt(intangibles.IntangibleCapital, selectedPeriodIdx)
  const knowledgeStock = intangibleCap > 0 ? intangibleCap * 0.65 : rd * 3.0 // Peters & Taylor ~65% Knowledge
  const brandStock = intangibleCap > 0 ? intangibleCap * 0.35 : brandCapitalized * 2.0 // ~35% Brand
  const rawAdjInvestedCap = getIntangibleValAt(intangibles.AdjustedInvestedCap, selectedPeriodIdx)
  const adjInvestedCap = rawAdjInvestedCap > 0 ? rawAdjInvestedCap : gaapInvestedCap + (intangibleCap > 0 ? intangibleCap : knowledgeStock + brandStock)

  // ROIC Metrics
  const gaapROIC = gaapInvestedCap > 0 ? (gaapNOPAT / gaapInvestedCap) * 100 : 0
  const economicROIC = adjInvestedCap > 0 ? (economicNOPAT / adjInvestedCap) * 100 : 0
  const roicDelta = economicROIC - gaapROIC

  // Shares and market cap for valuation multiples
  const shares =
    getMetricAt('Diluted Weighted Average Shares', selectedPeriodIdx) ||
    getMetricAt('Shares Outstanding', selectedPeriodIdx) ||
    1e9
  const netIncome = getMetricAt('Net Income', selectedPeriodIdx)

  // Implied enterprise value & market multiple transformation
  // Assume a synthetic EV baseline from latest or multiple if quote is external
  const syntheticEV = Math.max(adjInvestedCap * 2.5, gaapEBIT * 25.0)
  const gaapEV_EBIT = gaapEBIT > 0 ? syntheticEV / gaapEBIT : 35.0
  const gaapPE = netIncome > 0 ? syntheticEV / netIncome : 42.0
  const economicEV_NOPAT = economicNOPAT > 0 ? syntheticEV / economicNOPAT : 14.5
  const multipleCompression = Math.max(0, gaapPE - economicEV_NOPAT)

  // -------------------------------------------------------------
  // Bridge Steps Construction for Waterfalls
  // -------------------------------------------------------------
  interface WaterfallStep {
    name: string
    shortName: string
    delta: number
    cumulative: number
    isAnchor?: boolean
    isDeduction?: boolean
    color: string
    barFill: string
    description: string
  }

  // 1. Operating Income to Economic NOPAT Steps
  const nopatSteps: WaterfallStep[] = useMemo(() => {
    let cum = 0
    const s1: WaterfallStep = {
      name: 'Reported GAAP Operating Income (EBIT)',
      shortName: 'GAAP EBIT',
      delta: gaapEBIT,
      cumulative: gaapEBIT,
      isAnchor: true,
      color: '#3b82f6', // blue
      barFill: 'fill-blue-500 dark:fill-blue-400',
      description: 'Reported GAAP Operating Profit from Form 10-K prior to intangible adjustments.',
    }
    cum = gaapEBIT

    const s2: WaterfallStep = {
      name: '(+) 100% R&D Capitalization',
      shortName: '+ R&D Addback',
      delta: rd,
      cumulative: cum + rd,
      color: '#10b981', // emerald
      barFill: 'fill-emerald-500 dark:fill-emerald-400',
      description: 'Reverses GAAP expensing of R&D investments, capitalizing 100% into economic assets (Peters & Taylor 2017).',
    }
    cum += rd

    const s3: WaterfallStep = {
      name: '(+) 30% Brand SG&A Capitalization',
      shortName: '+ Brand Capital',
      delta: brandCapitalized,
      cumulative: cum + brandCapitalized,
      color: '#06b6d4', // cyan
      barFill: 'fill-cyan-500 dark:fill-cyan-400',
      description: 'Capitalizes 30% of SG&A representing customer acquisition and brand building expenditure.',
    }
    cum += brandCapitalized

    const s4: WaterfallStep = {
      name: '(-) Intangible Asset Amortization',
      shortName: '- Amortization',
      delta: -intangibleAmort,
      cumulative: cum - intangibleAmort,
      isDeduction: true,
      color: '#f43f5e', // rose
      barFill: 'fill-rose-500 dark:fill-rose-400',
      description: 'Economic straight-line decay of accumulated knowledge capital (5 years) and brand capital (3 years).',
    }
    cum -= intangibleAmort

    const s5: WaterfallStep = {
      name: '(=) Intangible-Adjusted EBIT',
      shortName: 'Adj. EBIT',
      delta: cum,
      cumulative: cum,
      isAnchor: true,
      color: '#8b5cf6', // purple
      barFill: 'fill-purple-500 dark:fill-purple-400',
      description: 'True pre-tax operating earnings generated by tangible and intangible assets combined.',
    }

    const s6: WaterfallStep = {
      name: `(-) Operating Taxes (${taxRate}%)`,
      shortName: '- Taxes',
      delta: -normalizedTax,
      cumulative: cum - normalizedTax,
      isDeduction: true,
      color: '#f59e0b', // amber
      barFill: 'fill-amber-500 dark:fill-amber-400',
      description: `Normalized corporate tax expense applied at statutory/effective ${taxRate}% rate.`,
    }
    cum -= normalizedTax

    const s7: WaterfallStep = {
      name: '(=) Economic NOPAT',
      shortName: 'Economic NOPAT',
      delta: cum,
      cumulative: cum,
      isAnchor: true,
      color: '#10b981', // vibrant emerald
      barFill: 'fill-emerald-500 dark:fill-emerald-400',
      description: 'True after-tax economic cash operating profit available to all capital providers.',
    }

    return [s1, s2, s3, s4, s5, s6, s7]
  }, [gaapEBIT, rd, brandCapitalized, intangibleAmort, normalizedTax, taxRate])

  // 2. Invested Capital Steps
  const investedCapSteps: WaterfallStep[] = useMemo(() => {
    let cum = 0
    const s1: WaterfallStep = {
      name: "Stockholders' Equity",
      shortName: 'Equity',
      delta: equity,
      cumulative: equity,
      isAnchor: true,
      color: '#64748b',
      barFill: 'fill-slate-500 dark:fill-slate-400',
      description: "Reported balance sheet book value of shareholders' equity.",
    }
    cum = equity

    const s2: WaterfallStep = {
      name: '(+) Debt & Borrowings',
      shortName: '+ Debt',
      delta: debt,
      cumulative: cum + debt,
      color: '#3b82f6',
      barFill: 'fill-blue-500 dark:fill-blue-400',
      description: 'Funded debt obligations contributing to total enterprise capital.',
    }
    cum += debt

    const s3: WaterfallStep = {
      name: '(-) Cash & Cash Equivalents',
      shortName: '- Cash',
      delta: -cash,
      cumulative: cum - cash,
      isDeduction: true,
      color: '#f59e0b',
      barFill: 'fill-amber-500 dark:fill-amber-400',
      description: 'Deduction of non-operating liquid cash buffer to isolate active invested capital.',
    }
    cum -= cash

    const s4: WaterfallStep = {
      name: '(=) Reported GAAP Invested Capital',
      shortName: 'GAAP Inv. Cap',
      delta: cum,
      cumulative: cum,
      isAnchor: true,
      color: '#6366f1',
      barFill: 'fill-indigo-500 dark:fill-indigo-400',
      description: 'Traditional tangible balance sheet invested capital under standard accounting.',
    }

    const s5: WaterfallStep = {
      name: '(+) Capitalized Knowledge Capital',
      shortName: '+ Knowledge Stock',
      delta: knowledgeStock,
      cumulative: cum + knowledgeStock,
      color: '#8b5cf6',
      barFill: 'fill-purple-500 dark:fill-purple-400',
      description: 'Cumulative unamortized balance of R&D investments accumulated over 5 years.',
    }
    cum += knowledgeStock

    const s6: WaterfallStep = {
      name: '(+) Capitalized Brand Capital',
      shortName: '+ Brand Stock',
      delta: brandStock,
      cumulative: cum + brandStock,
      color: '#06b6d4',
      barFill: 'fill-cyan-500 dark:fill-cyan-400',
      description: 'Cumulative unamortized balance of 30% SG&A brand capital accumulated over 3 years.',
    }
    cum += brandStock

    const s7: WaterfallStep = {
      name: '(=) Peters & Taylor Adjusted Invested Capital',
      shortName: 'Adj. Invested Cap',
      delta: cum,
      cumulative: cum,
      isAnchor: true,
      color: '#10b981',
      barFill: 'fill-emerald-500 dark:fill-emerald-400',
      description: 'Total economic balance sheet capital supporting the business, including intellectual property.',
    }

    return [s1, s2, s3, s4, s5, s6, s7]
  }, [equity, debt, cash, knowledgeStock, brandStock])

  const activeSteps = viewMode === 'invested_cap' ? investedCapSteps : nopatSteps

  // -------------------------------------------------------------
  // SVG Waterfall Coordinates Calculation
  // -------------------------------------------------------------
  const chartWidth = 780
  const chartHeight = 280
  const padLeft = 70
  const padRight = 30
  const padTop = 30
  const padBottom = 45
  const plotWidth = chartWidth - padLeft - padRight
  const plotHeight = chartHeight - padTop - padBottom

  // Find max cumulative value for Y scaling
  const maxWaterfallVal = Math.max(
    ...activeSteps.flatMap((s) => [Math.abs(s.cumulative), Math.abs(s.cumulative - s.delta), Math.abs(s.delta)])
  ) * 1.15 || 100

  const getY = (val: number) => {
    const clamped = Math.max(0, Math.min(maxWaterfallVal, val))
    return padTop + plotHeight - (clamped / maxWaterfallVal) * plotHeight
  }

  const stepWidth = plotWidth / activeSteps.length
  const barW = Math.min(60, stepWidth * 0.72)

  // 5 clean ticks on Y axis
  const yTicks = [0, 0.25, 0.5, 0.75, 1.0].map((frac) => frac * maxWaterfallVal)

  return (
    <div className={`space-y-6 font-sans ${className}`}>
      {/* Editorial Header Banner */}
      <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/70 backdrop-blur-md p-4 rounded-xl space-y-3">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <span className="font-mono text-xs font-semibold uppercase tracking-wider text-neutral-950 dark:text-white">
                GAAP-to-Economic ROIC & Earnings Bridge
              </span>
              <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20">
                Peters & Taylor (2017)
              </span>
            </div>
            <p className="text-xs text-neutral-500 font-sans mt-0.5 max-w-2xl">
              Visualizes how capitalizing off-balance-sheet R&D and Brand customer acquisition reverses accounting distortion, bridging reported GAAP operating income directly into economic NOPAT and true ROIC.
            </p>
          </div>

          {/* Period Selector Pills */}
          {periods && periods.length > 0 && (
            <div className="flex items-center gap-1.5 self-start sm:self-auto font-mono text-xs">
              <span className="text-[10px] text-neutral-400 font-sans mr-1">Fiscal Year:</span>
              <div className="flex items-center rounded-lg border border-neutral-200 dark:border-white/10 p-0.5 bg-neutral-50 dark:bg-neutral-900">
                {periods.slice(0, 5).map((p, idx) => (
                  <button
                    key={p.Year}
                    onClick={() => setSelectedPeriodIdx(idx)}
                    className={`px-2 py-0.5 rounded text-[11px] transition-colors ${
                      selectedPeriodIdx === idx
                        ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                        : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
                    }`}
                  >
                    FY {p.Year}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* View Switcher Controls Strip */}
        <div className="flex flex-wrap items-center justify-between gap-3 pt-2 border-t border-neutral-200/80 dark:border-white/10">
          <div className="flex items-center gap-2">
            {[
              { id: 'nopat', label: '1. Economic NOPAT Waterfall', icon: Activity },
              { id: 'invested_cap', label: '2. Invested Capital Bridge', icon: Layers },
              { id: 'sankey', label: '3. Interactive Sankey Flow', icon: Workflow },
            ].map((tab) => {
              const Icon = tab.icon
              return (
                <button
                  key={tab.id}
                  onClick={() => setViewMode(tab.id as BridgeViewMode)}
                  className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-mono transition-colors ${
                    viewMode === tab.id
                      ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold shadow-xs'
                      : 'border border-neutral-200 dark:border-white/10 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-800'
                  }`}
                >
                  <Icon className="w-3.5 h-3.5" />
                  <span>{tab.label}</span>
                </button>
              )
            })}
          </div>

          {/* Tax Rate Interactive Slider */}
          <div className="flex items-center gap-2 font-mono text-xs text-neutral-500">
            <span className="text-[11px] font-sans">Effective Tax Rate:</span>
            <input
              type="range"
              min="10"
              max="35"
              step="0.5"
              value={taxRate}
              onChange={(e) => setTaxRate(parseFloat(e.target.value))}
              className="w-20 accent-neutral-900 dark:accent-white cursor-pointer"
            />
            <span className="font-semibold text-neutral-900 dark:text-white tabular-nums w-10">
              {taxRate.toFixed(1)}%
            </span>
          </div>
        </div>
      </div>

      {/* ========================================================= */}
      {/* The "AHA MOMENT" Institutional Valuation Arbitrage Card  */}
      {/* ========================================================= */}
      <div className="border border-neutral-200/80 dark:border-white/10 bg-gradient-to-r from-neutral-50/80 via-white to-purple-50/20 dark:from-[#030611]/90 dark:via-neutral-950 dark:to-purple-950/20 rounded-xl p-4 sm:p-5 shadow-xs relative overflow-hidden backdrop-blur-md">
        <div className="flex flex-col lg:flex-row items-stretch justify-between gap-6">
          {/* Column A: GAAP Accounting Lens */}
          <div className="flex-1 space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono uppercase tracking-wider text-neutral-500 font-semibold">
                GAAP Accounting Lens (Reported)
              </span>
              <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20">
                Expensed Intangibles
              </span>
            </div>

            <div className="grid grid-cols-2 gap-3 pt-1 font-mono">
              <div className="p-3 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-white/70 dark:bg-neutral-900/40">
                <span className="text-[10px] text-neutral-400 font-sans block uppercase">Reported P/E</span>
                <span className="text-xl font-bold text-rose-600 dark:text-rose-400 tabular-nums">
                  {gaapPE > 0 ? `${gaapPE.toFixed(1)}x` : '—'}
                </span>
                <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                  Apparent "Expensive" Multiple
                </span>
              </div>

              <div className="p-3 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-white/70 dark:bg-neutral-900/40">
                <span className="text-[10px] text-neutral-400 font-sans block uppercase">Reported GAAP ROIC</span>
                <span className="text-xl font-bold text-neutral-800 dark:text-neutral-200 tabular-nums">
                  {gaapROIC.toFixed(1)}%
                </span>
                <span className="text-[10px] text-neutral-400 font-sans block mt-0.5">
                  Tangible Capital Only
                </span>
              </div>
            </div>

            <p className="text-[11px] text-neutral-500 font-sans pt-1 leading-relaxed">
              GAAP treats R&D and customer acquisition as ephemeral costs rather than multi-year compounding assets, depressing reported income and making great businesses look artificially overpriced.
            </p>
          </div>

          {/* Center Column: The Economic Transformation Bridge */}
          <div className="flex flex-col items-center justify-center px-2 py-4 lg:py-0 border-y lg:border-y-0 lg:border-x border-neutral-200 dark:border-neutral-800 space-y-2">
            <span className="text-[10px] font-mono uppercase tracking-widest text-purple-600 dark:text-purple-400 font-semibold text-center">
              Intangibles Arbitrage
            </span>
            <div className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400 font-mono text-base font-bold">
              <span>-{multipleCompression.toFixed(1)}x</span>
              <span className="text-xs font-normal">Multiple Compression</span>
            </div>
            <div className="p-1 rounded-full bg-purple-500/10 text-purple-500">
              <ArrowRight className="w-5 h-5 hidden lg:block" />
              <Activity className="w-5 h-5 lg:hidden" />
            </div>
            <span className="text-[10px] font-mono text-neutral-400 text-center">
              Capitalized R&D + Brand Stock
            </span>
          </div>

          {/* Column B: Peters & Taylor Economic Lens */}
          <div className="flex-1 space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono uppercase tracking-wider text-purple-600 dark:text-purple-400 font-semibold">
                Economic Reality Lens (Peters & Taylor)
              </span>
              <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
                Capitalized Assets
              </span>
            </div>

            <div className="grid grid-cols-2 gap-3 pt-1 font-mono">
              <div className="p-3 rounded-lg border border-emerald-500/30 bg-emerald-500/[0.05] dark:bg-emerald-500/[0.08]">
                <span className="text-[10px] text-emerald-600 dark:text-emerald-400 font-sans block uppercase font-medium">
                  True EV / Economic NOPAT
                </span>
                <span className="text-xl font-bold text-emerald-600 dark:text-emerald-400 tabular-nums">
                  {economicEV_NOPAT.toFixed(1)}x
                </span>
                <span className="text-[10px] text-emerald-700/70 dark:text-emerald-300/70 font-sans block mt-0.5 font-medium">
                  Real Economic Cash Multiple
                </span>
              </div>

              <div className="p-3 rounded-lg border border-purple-500/30 bg-purple-500/[0.05] dark:bg-purple-500/[0.08]">
                <span className="text-[10px] text-purple-600 dark:text-purple-400 font-sans block uppercase font-medium">
                  Adjusted Economic ROIC
                </span>
                <span className="text-xl font-bold text-purple-600 dark:text-purple-400 tabular-nums">
                  {economicROIC.toFixed(1)}%
                </span>
                <span className="text-[10px] text-purple-700/70 dark:text-purple-300/70 font-sans block mt-0.5">
                  Spread: {roicDelta >= 0 ? '+' : ''}{roicDelta.toFixed(1)}% pts
                </span>
              </div>
            </div>

            <p className="text-[11px] text-neutral-500 font-sans pt-1 leading-relaxed">
              When software codebases, drug pipelines, and brands are recognized as economic capital, apparent 40x P/E multiples compress into compelling ~14x cash earnings valuations.
            </p>
          </div>
        </div>
      </div>

      {/* ========================================================= */}
      {/* Interactive Waterfall Visualization or Sankey Flow        */}
      {/* ========================================================= */}
      <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/70 backdrop-blur-md rounded-xl p-4 sm:p-5 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-neutral-200/80 dark:border-white/10 pb-3">
          <div>
            <span className="font-mono text-xs font-semibold uppercase tracking-wider text-neutral-900 dark:text-white">
              {viewMode === 'nopat'
                ? 'Operating Income to Economic NOPAT Bridge'
                : viewMode === 'invested_cap'
                ? 'Invested Capital Intangible Stock Bridge'
                : 'Interactive Knowledge & Brand Flow Sankey'}
            </span>
            <span className="text-neutral-400 text-xs font-mono ml-2">
              (FY {periods?.[selectedPeriodIdx]?.Year || 'Latest'})
            </span>
          </div>

          <div className="flex items-center gap-3 text-xs font-mono text-neutral-500">
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-sm bg-emerald-500" />
              <span>Capitalization Addback</span>
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-sm bg-rose-500" />
              <span>Amortization / Taxes</span>
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-sm bg-neutral-900 dark:bg-white" />
              <span>Anchor Totals</span>
            </span>
          </div>
        </div>

        {/* 1 & 2. Interactive SVG Waterfall Chart */}
        {viewMode !== 'sankey' && (
          <div className="overflow-x-auto select-none pt-2">
            <svg
              viewBox={`0 0 ${chartWidth} ${chartHeight}`}
              className="w-full h-auto min-w-[700px] font-mono text-xs"
            >
              {/* Y Axis Grid Lines & Ticks */}
              {yTicks.map((tick, i) => {
                const y = getY(tick)
                return (
                  <g key={i}>
                    <line
                      x1={padLeft}
                      y1={y}
                      x2={chartWidth - padRight}
                      y2={y}
                      stroke="currentColor"
                      strokeDasharray="2 3"
                      strokeWidth="1"
                      className="text-neutral-200 dark:text-neutral-800"
                    />
                    <text
                      x={padLeft - 10}
                      y={y + 3.5}
                      textAnchor="end"
                      className="fill-neutral-400 dark:fill-neutral-500 text-[10px] tabular-nums"
                    >
                      {formatCurrency(tick, true, curr)}
                    </text>
                  </g>
                )
              })}

              {/* Waterfall Steps */}
              {activeSteps.map((step, idx) => {
                const x = padLeft + idx * stepWidth + (stepWidth - barW) / 2
                const isHovered = hoveredStepIndex === idx

                let barTop = 0
                let barBottom = 0

                if (step.isAnchor) {
                  // Anchors rise from zero to cumulative
                  barTop = getY(step.cumulative)
                  barBottom = getY(0)
                } else if (step.delta >= 0) {
                  // Addback: rises from prior cumulative to current cumulative
                  barTop = getY(step.cumulative)
                  barBottom = getY(step.cumulative - step.delta)
                } else {
                  // Deduction: drops from prior cumulative to current cumulative
                  barTop = getY(step.cumulative - step.delta)
                  barBottom = getY(step.cumulative)
                }

                const barH = Math.max(3, barBottom - barTop)

                // Connector line to next bar
                const nextStep = activeSteps[idx + 1]
                const connX1 = x + barW
                const connX2 = padLeft + (idx + 1) * stepWidth + (stepWidth - barW) / 2
                const connY = getY(step.cumulative)

                return (
                  <g
                    key={idx}
                    className="cursor-pointer transition-opacity"
                    onMouseEnter={() => setHoveredStepIndex(idx)}
                    onMouseLeave={() => setHoveredStepIndex(null)}
                  >
                    {/* Hover Column Highlight */}
                    <rect
                      x={padLeft + idx * stepWidth}
                      y={padTop}
                      width={stepWidth}
                      height={plotHeight}
                      fill={isHovered ? 'currentColor' : 'transparent'}
                      className="text-neutral-100/60 dark:text-white/[0.02] transition-colors"
                    />

                    {/* Connecting Dashed Line to Next Bar */}
                    {nextStep && (
                      <line
                        x1={connX1}
                        y1={connY}
                        x2={connX2}
                        y2={connY}
                        stroke="currentColor"
                        strokeDasharray="2 2"
                        strokeWidth="1"
                        className="text-neutral-400 dark:text-neutral-600"
                      />
                    )}

                    {/* Step Floating Bar */}
                    <rect
                      x={x}
                      y={barTop}
                      width={barW}
                      height={barH}
                      rx={3}
                      fill={step.color}
                      fillOpacity={isHovered ? 0.95 : 0.8}
                      stroke={step.color}
                      strokeWidth={isHovered ? 1.5 : 1}
                      className="transition-all duration-150"
                    />

                    {/* Value Label Above Bar */}
                    <text
                      x={x + barW / 2}
                      y={barTop - 6}
                      textAnchor="middle"
                      className={`text-[10px] font-mono font-semibold tabular-nums ${
                        step.isAnchor
                          ? 'fill-neutral-950 dark:fill-white font-bold'
                          : step.delta >= 0
                          ? 'fill-emerald-600 dark:fill-emerald-400'
                          : 'fill-rose-600 dark:fill-rose-400'
                      }`}
                    >
                      {step.isAnchor
                        ? formatCurrency(step.cumulative, true, curr)
                        : `${step.delta >= 0 ? '+' : ''}${formatCurrency(step.delta, true, curr)}`}
                    </text>

                    {/* Bottom Step Name Label */}
                    <text
                      x={x + barW / 2}
                      y={padTop + plotHeight + 16}
                      textAnchor="middle"
                      className="text-[10px] font-sans font-medium fill-neutral-700 dark:fill-neutral-300"
                    >
                      {step.shortName}
                    </text>
                    <text
                      x={x + barW / 2}
                      y={padTop + plotHeight + 28}
                      textAnchor="middle"
                      className="text-[9px] font-mono fill-neutral-400 dark:fill-neutral-500 tabular-nums"
                    >
                      {rev > 0 ? `${((Math.abs(step.delta) / rev) * 100).toFixed(1)}% rev` : ''}
                    </text>
                  </g>
                )
              })}
            </svg>
          </div>
        )}

        {/* 3. Interactive Sankey Flow Diagram */}
        {viewMode === 'sankey' && (
          <div className="overflow-x-auto select-none pt-2">
            <svg
              viewBox="0 0 780 240"
              className="w-full h-auto min-w-[700px] font-mono text-xs"
            >
              <defs>
                <linearGradient id="flowRD" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#10b981" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#8b5cf6" stopOpacity="0.4" />
                </linearGradient>
                <linearGradient id="flowEBIT" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#3b82f6" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#8b5cf6" stopOpacity="0.4" />
                </linearGradient>
                <linearGradient id="flowBrand" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#06b6d4" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#8b5cf6" stopOpacity="0.4" />
                </linearGradient>
                <linearGradient id="flowNOPAT" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#8b5cf6" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#10b981" stopOpacity="0.4" />
                </linearGradient>
                <linearGradient id="flowAmort" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#8b5cf6" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#f43f5e" stopOpacity="0.4" />
                </linearGradient>
                <linearGradient id="flowTax" x1="0" y1="0" x2="1" y2="0">
                  <stop offset="0%" stopColor="#8b5cf6" stopOpacity="0.4" />
                  <stop offset="100%" stopColor="#f59e0b" stopOpacity="0.4" />
                </linearGradient>
              </defs>

              {/* Inflow Nodes (Left) */}
              {/* GAAP EBIT Node */}
              <g transform="translate(40, 30)">
                <rect x="0" y="0" width="130" height="42" rx="6" fill="#3b82f6" fillOpacity="0.15" stroke="#3b82f6" />
                <text x="12" y="18" className="text-[11px] font-sans font-medium fill-neutral-900 dark:fill-white">GAAP EBIT</text>
                <text x="12" y="32" className="text-[11px] font-mono font-bold fill-blue-600 dark:fill-blue-400 tabular-nums">
                  {formatCurrency(gaapEBIT, true, curr)}
                </text>
              </g>

              {/* Capitalized R&D Node */}
              <g transform="translate(40, 95)">
                <rect x="0" y="0" width="130" height="42" rx="6" fill="#10b981" fillOpacity="0.15" stroke="#10b981" />
                <text x="12" y="18" className="text-[11px] font-sans font-medium fill-neutral-900 dark:fill-white">R&D Capitalized</text>
                <text x="12" y="32" className="text-[11px] font-mono font-bold fill-emerald-600 dark:fill-emerald-400 tabular-nums">
                  +{formatCurrency(rd, true, curr)}
                </text>
              </g>

              {/* Capitalized Brand SG&A Node */}
              <g transform="translate(40, 160)">
                <rect x="0" y="0" width="130" height="42" rx="6" fill="#06b6d4" fillOpacity="0.15" stroke="#06b6d4" />
                <text x="12" y="18" className="text-[11px] font-sans font-medium fill-neutral-900 dark:fill-white">Brand SG&A (30%)</text>
                <text x="12" y="32" className="text-[11px] font-mono font-bold fill-cyan-600 dark:fill-cyan-400 tabular-nums">
                  +{formatCurrency(brandCapitalized, true, curr)}
                </text>
              </g>

              {/* Curved Flow Stream: Inflows into Adjusted EBIT */}
              <path d="M 170 51 C 240 51, 270 95, 330 95" fill="none" stroke="url(#flowEBIT)" strokeWidth="16" />
              <path d="M 170 116 C 240 116, 260 116, 330 116" fill="none" stroke="url(#flowRD)" strokeWidth="14" />
              <path d="M 170 181 C 240 181, 270 137, 330 137" fill="none" stroke="url(#flowBrand)" strokeWidth="12" />

              {/* Center Node: Adjusted EBIT */}
              <g transform="translate(330, 80)">
                <rect x="0" y="0" width="150" height="72" rx="8" fill="#8b5cf6" fillOpacity="0.2" stroke="#8b5cf6" strokeWidth="1.5" />
                <text x="16" y="24" className="text-[11px] font-sans font-semibold fill-neutral-950 dark:fill-white uppercase tracking-wider">
                  Intangible EBIT
                </text>
                <text x="16" y="44" className="text-base font-mono font-bold fill-purple-600 dark:fill-purple-400 tabular-nums">
                  {formatCurrency(adjEBIT, true, curr)}
                </text>
                <text x="16" y="60" className="text-[10px] font-mono fill-neutral-400">
                  True Gross Economic Yield
                </text>
              </g>

              {/* Outflow Stream: Adjusted EBIT to Outflows */}
              <path d="M 480 95 C 540 95, 560 51, 620 51" fill="none" stroke="url(#flowAmort)" strokeWidth="12" />
              <path d="M 480 116 C 540 116, 560 116, 620 116" fill="none" stroke="url(#flowTax)" strokeWidth="10" />
              <path d="M 480 137 C 540 137, 560 181, 620 181" fill="none" stroke="url(#flowNOPAT)" strokeWidth="18" />

              {/* Outflow Nodes (Right) */}
              {/* Intangible Amortization Decay Node */}
              <g transform="translate(620, 30)">
                <rect x="0" y="0" width="130" height="42" rx="6" fill="#f43f5e" fillOpacity="0.15" stroke="#f43f5e" />
                <text x="12" y="18" className="text-[11px] font-sans font-medium fill-neutral-900 dark:fill-white">Amortization Decay</text>
                <text x="12" y="32" className="text-[11px] font-mono font-bold fill-rose-600 dark:fill-rose-400 tabular-nums">
                  -{formatCurrency(intangibleAmort, true, curr)}
                </text>
              </g>

              {/* Taxes Node */}
              <g transform="translate(620, 95)">
                <rect x="0" y="0" width="130" height="42" rx="6" fill="#f59e0b" fillOpacity="0.15" stroke="#f59e0b" />
                <text x="12" y="18" className="text-[11px] font-sans font-medium fill-neutral-900 dark:fill-white">Normalized Tax</text>
                <text x="12" y="32" className="text-[11px] font-mono font-bold fill-amber-600 dark:fill-amber-400 tabular-nums">
                  -{formatCurrency(normalizedTax, true, curr)}
                </text>
              </g>

              {/* Economic NOPAT Node */}
              <g transform="translate(620, 160)">
                <rect x="0" y="0" width="130" height="46" rx="6" fill="#10b981" fillOpacity="0.2" stroke="#10b981" strokeWidth="2" />
                <text x="12" y="18" className="text-[11px] font-sans font-bold fill-neutral-950 dark:fill-white uppercase">
                  Economic NOPAT
                </text>
                <text x="12" y="35" className="text-sm font-mono font-bold fill-emerald-600 dark:fill-emerald-400 tabular-nums">
                  {formatCurrency(economicNOPAT, true, curr)}
                </text>
              </g>
            </svg>
          </div>
        )}

        {/* Dynamic Detail Callout on Hover */}
        {hoveredStepIndex !== null && activeSteps[hoveredStepIndex] && (
          <div className="p-3 rounded-lg border border-neutral-200 dark:border-white/10 bg-neutral-50/80 dark:bg-white/[0.04] text-xs font-mono flex items-center justify-between gap-4">
            <div>
              <span className="font-semibold text-neutral-900 dark:text-white">
                {activeSteps[hoveredStepIndex].name}
              </span>
              <p className="text-neutral-500 font-sans text-[11px] mt-0.5">
                {activeSteps[hoveredStepIndex].description}
              </p>
            </div>
            <div className="text-right whitespace-nowrap">
              <span className="text-neutral-400 text-[10px] block font-sans">Step Impact</span>
              <span
                className={`font-bold tabular-nums text-sm ${
                  activeSteps[hoveredStepIndex].isAnchor
                    ? 'text-neutral-950 dark:text-white'
                    : activeSteps[hoveredStepIndex].delta >= 0
                    ? 'text-emerald-600 dark:text-emerald-400'
                    : 'text-rose-600 dark:text-rose-400'
                }`}
              >
                {activeSteps[hoveredStepIndex].isAnchor
                  ? formatCurrency(activeSteps[hoveredStepIndex].cumulative, false, curr)
                  : `${activeSteps[hoveredStepIndex].delta >= 0 ? '+' : ''}${formatCurrency(activeSteps[hoveredStepIndex].delta, false, curr)}`}
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
