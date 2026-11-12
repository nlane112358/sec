import React, { useState } from 'react'
import { BarChart3, TrendingUp, PieChart, Layers, ArrowUpRight } from 'lucide-react'
import { SummaryResponse, SummaryValue } from '../types'
import { formatCurrency, formatPercent } from '../lib/format'

interface FinancialChartsProps {
  summary: SummaryResponse
  compact: boolean
}

type ChartTab = 'trends' | 'margins' | 'capital_allocation' | 'capital_structure'

export const FinancialCharts: React.FC<FinancialChartsProps> = ({ summary, compact }) => {
  const [activeTab, setActiveTab] = useState<ChartTab>('trends')
  const [hoveredYearIndex, setHoveredYearIndex] = useState<number | null>(null)

  const periods = summary.periods || []
  if (periods.length === 0) return null

  // Chronological order (oldest to newest for charts: index 4, 3, 2, 1, 0)
  const chronoPeriods = [...periods].reverse()
  const n = periods.length

  const getChronologicalValues = (metricName: string): (number | null)[] => {
    const m = summary.metrics.find((x) => x.Name === metricName)
    if (!m) return Array(n).fill(null)
    const reversed = [...m.Values].reverse()
    return reversed.map((v) => (v && v.Present ? v.Value : null))
  }

  const revenues = getChronologicalValues('Revenue')
  const operatingIncomes = getChronologicalValues('Operating Income')
  const netIncomes = getChronologicalValues('Net Income')
  const fcfs = getChronologicalValues('Free Cash Flow')
  const ocfs = getChronologicalValues('Operating Cash Flow')
  const capexs = getChronologicalValues('Capital Expenditure')
  const rds = getChronologicalValues('Research & Development')
  const sbcs = getChronologicalValues('Stock-Based Compensation')
  const cashes = getChronologicalValues('Cash & Equivalents')
  const debts = getChronologicalValues('Debt / Borrowings')
  const equities = getChronologicalValues("Stockholders' Equity")

  // Intangible Adjusted ROIC
  const adjROICs = summary.intangibles.AdjustedROIC
    ? [...summary.intangibles.AdjustedROIC].reverse().map((v) => (v && v.Present ? v.Value : null))
    : Array(n).fill(null)

  // Operating Margins
  const opMargins = revenues.map((rev, idx) => {
    const op = operatingIncomes[idx]
    if (rev !== null && op !== null && rev > 0) return (op / rev) * 100
    return null
  })

  // FCF Margins
  const fcfMargins = revenues.map((rev, idx) => {
    const f = fcfs[idx]
    if (rev !== null && f !== null && rev > 0) return (f / rev) * 100
    return null
  })

  // Chart dimensions for SVG
  const chartWidth = 720
  const chartHeight = 220
  const padding = { top: 20, right: 30, bottom: 35, left: 60 }
  const innerWidth = chartWidth - padding.left - padding.right
  const innerHeight = chartHeight - padding.top - padding.bottom

  return (
    <div className="border border-neutral-200 dark:border-neutral-800 bg-white/70 dark:bg-neutral-950/70 backdrop-blur-sm rounded-xl overflow-hidden shadow-xs transition-colors">
      {/* Chart Navigation Toolbar */}
      <div className="px-4 py-2.5 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/30 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs font-mono">
        <div className="flex items-center gap-1 sm:gap-2">
          <span className="text-[10px] uppercase tracking-wider text-neutral-400 font-sans font-medium mr-1 hidden sm:inline">
            Visual Analytics:
          </span>
          {[
            { id: 'trends', label: 'Revenue & Cash Generation', icon: BarChart3 },
            { id: 'margins', label: 'Margins & ROIC Trend', icon: TrendingUp },
            { id: 'capital_allocation', label: 'Capital Allocation', icon: PieChart },
            { id: 'capital_structure', label: 'Balance Sheet Footprint', icon: Layers },
          ].map((tab) => {
            const Icon = tab.icon
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id as ChartTab)}
                className={`flex items-center gap-1.5 px-2.5 py-1 rounded transition-colors text-[11px] ${
                  activeTab === tab.id
                    ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                    : 'text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-800'
                }`}
              >
                <Icon className="w-3 h-3" />
                <span>{tab.label}</span>
              </button>
            )
          })}
        </div>

        <div className="text-[11px] text-neutral-500 font-sans italic hidden md:block">
          5-Year Standardized SEC Facts • Linear Scale
        </div>
      </div>

      {/* Main Chart Body */}
      <div className="p-4 sm:p-5">
        {/* ========================================================= */}
        {/* TAB 1: REVENUE & CASH GENERATION TRENDS                   */}
        {/* ========================================================= */}
        {activeTab === 'trends' && (() => {
          // Find max value for scaling
          const allVals = [...revenues, ...operatingIncomes, ...fcfs].filter(
            (v): v is number => v !== null
          )
          const maxVal = Math.max(1, ...allVals)
          const minVal = Math.min(0, ...allVals)
          const range = maxVal - minVal || 1

          const colWidth = innerWidth / n
          const barGroupWidth = Math.min(colWidth * 0.7, 72)
          const barWidth = barGroupWidth / 3

          return (
            <div>
              <div className="flex flex-wrap items-center justify-between gap-4 mb-3 text-xs font-mono">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-indigo-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">Revenue</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-emerald-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">Operating Income</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-teal-400" />
                    <span className="text-neutral-700 dark:text-neutral-300">Free Cash Flow</span>
                  </div>
                </div>

                {hoveredYearIndex !== null && (
                  <div className="text-[11px] text-neutral-600 dark:text-neutral-400">
                    <strong className="text-neutral-950 dark:text-white">
                      FY {chronoPeriods[hoveredYearIndex]?.Year}
                    </strong>
                    : Rev {formatCurrency(revenues[hoveredYearIndex] || 0, true)} • EBIT{' '}
                    {formatCurrency(operatingIncomes[hoveredYearIndex] || 0, true)} • FCF{' '}
                    {formatCurrency(fcfs[hoveredYearIndex] || 0, true)}
                  </div>
                )}
              </div>

              <div className="w-full overflow-x-auto">
                <svg
                  viewBox={`0 0 ${chartWidth} ${chartHeight}`}
                  className="w-full h-48 sm:h-56 select-none"
                >
                  {/* Grid Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((pct, i) => {
                    const y = padding.top + innerHeight * (1 - pct)
                    const val = minVal + range * pct
                    return (
                      <g key={i}>
                        <line
                          x1={padding.left}
                          y1={y}
                          x2={chartWidth - padding.right}
                          y2={y}
                          stroke="currentColor"
                          className="text-neutral-200 dark:text-neutral-800/80"
                          strokeDasharray="2,3"
                        />
                        <text
                          x={padding.left - 8}
                          y={y + 3}
                          textAnchor="end"
                          className="text-[10px] font-mono fill-neutral-400"
                        >
                          {formatCurrency(val, true)}
                        </text>
                      </g>
                    )
                  })}

                  {/* Bars per Year */}
                  {chronoPeriods.map((p, idx) => {
                    const centerX = padding.left + colWidth * idx + colWidth / 2
                    const groupLeft = centerX - barGroupWidth / 2

                    const rev = revenues[idx] || 0
                    const op = operatingIncomes[idx] || 0
                    const fcf = fcfs[idx] || 0

                    const zeroY = padding.top + innerHeight * (1 - (0 - minVal) / range)

                    const getY = (v: number) => padding.top + innerHeight * (1 - (v - minVal) / range)
                    const getH = (v: number) => Math.max(2, Math.abs(getY(v) - zeroY))

                    const isHovered = hoveredYearIndex === idx

                    return (
                      <g
                        key={p.Year}
                        className="cursor-pointer transition-opacity"
                        onMouseEnter={() => setHoveredYearIndex(idx)}
                        onMouseLeave={() => setHoveredYearIndex(null)}
                      >
                        {/* Background hover highlight */}
                        {isHovered && (
                          <rect
                            x={padding.left + colWidth * idx}
                            y={padding.top}
                            width={colWidth}
                            height={innerHeight}
                            className="fill-neutral-500/5 dark:fill-neutral-400/5"
                          />
                        )}

                        {/* Revenue Bar */}
                        <rect
                          x={groupLeft}
                          y={rev >= 0 ? getY(rev) : zeroY}
                          width={barWidth - 2}
                          height={getH(rev)}
                          rx={2}
                          className="fill-indigo-500 hover:fill-indigo-400 transition-colors"
                        />

                        {/* Operating Income Bar */}
                        <rect
                          x={groupLeft + barWidth}
                          y={op >= 0 ? getY(op) : zeroY}
                          width={barWidth - 2}
                          height={getH(op)}
                          rx={2}
                          className="fill-emerald-500 hover:fill-emerald-400 transition-colors"
                        />

                        {/* Free Cash Flow Bar */}
                        <rect
                          x={groupLeft + barWidth * 2}
                          y={fcf >= 0 ? getY(fcf) : zeroY}
                          width={barWidth - 2}
                          height={getH(fcf)}
                          rx={2}
                          className="fill-teal-400 hover:fill-teal-300 transition-colors"
                        />

                        {/* Year Label */}
                        <text
                          x={centerX}
                          y={chartHeight - 10}
                          textAnchor="middle"
                          className={`text-[11px] font-mono transition-colors ${
                            isHovered
                              ? 'fill-neutral-950 dark:fill-white font-semibold'
                              : 'fill-neutral-500'
                          }`}
                        >
                          FY {p.Year}
                        </text>
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>
          )
        })()}

        {/* ========================================================= */}
        {/* TAB 2: MARGINS & ROIC TREND LINES                         */}
        {/* ========================================================= */}
        {activeTab === 'margins' && (() => {
          const allMargins = [...opMargins, ...fcfMargins, ...adjROICs].filter(
            (v): v is number => v !== null
          )
          const maxMargin = Math.max(25, Math.ceil((Math.max(...allMargins) + 5) / 5) * 5)
          const minMargin = Math.min(0, Math.floor((Math.min(...allMargins) - 5) / 5) * 5)
          const range = maxMargin - minMargin || 1

          const getX = (idx: number) => padding.left + (innerWidth / (n - 1 || 1)) * idx
          const getY = (val: number) => padding.top + innerHeight * (1 - (val - minMargin) / range)

          const makePoints = (arr: (number | null)[]) => {
            return arr
              .map((v, i) => (v !== null ? `${getX(i).toFixed(1)},${getY(v).toFixed(1)}` : null))
              .filter(Boolean)
              .join(' ')
          }

          return (
            <div>
              <div className="flex flex-wrap items-center justify-between gap-4 mb-3 text-xs font-mono">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-emerald-500 inline-block" />
                    <span className="text-neutral-700 dark:text-neutral-300">Operating Margin %</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-teal-400 inline-block" />
                    <span className="text-neutral-700 dark:text-neutral-300">FCF Margin %</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-purple-500 inline-block" />
                    <span className="text-neutral-700 dark:text-neutral-300">Intangible-Adjusted ROIC</span>
                  </div>
                </div>

                {hoveredYearIndex !== null && (
                  <div className="text-[11px] text-neutral-600 dark:text-neutral-400">
                    <strong className="text-neutral-950 dark:text-white">
                      FY {chronoPeriods[hoveredYearIndex]?.Year}
                    </strong>
                    : Op Margin {opMargins[hoveredYearIndex]?.toFixed(1) ?? '—'}% • FCF Margin{' '}
                    {fcfMargins[hoveredYearIndex]?.toFixed(1) ?? '—'}% • Adj ROIC{' '}
                    {adjROICs[hoveredYearIndex] !== null
                      ? formatPercent(adjROICs[hoveredYearIndex]!)
                      : '—'}
                  </div>
                )}
              </div>

              <div className="w-full overflow-x-auto">
                <svg
                  viewBox={`0 0 ${chartWidth} ${chartHeight}`}
                  className="w-full h-48 sm:h-56 select-none"
                >
                  {/* Grid Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((pct, i) => {
                    const y = padding.top + innerHeight * (1 - pct)
                    const val = minMargin + range * pct
                    return (
                      <g key={i}>
                        <line
                          x1={padding.left}
                          y1={y}
                          x2={chartWidth - padding.right}
                          y2={y}
                          stroke="currentColor"
                          className="text-neutral-200 dark:text-neutral-800/80"
                          strokeDasharray="2,3"
                        />
                        <text
                          x={padding.left - 8}
                          y={y + 3}
                          textAnchor="end"
                          className="text-[10px] font-mono fill-neutral-400"
                        >
                          {val.toFixed(0)}%
                        </text>
                      </g>
                    )
                  })}

                  {/* Polylines */}
                  <polyline
                    fill="none"
                    stroke="#10b981"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    points={makePoints(opMargins)}
                  />

                  <polyline
                    fill="none"
                    stroke="#2dd4bf"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    points={makePoints(fcfMargins)}
                  />

                  <polyline
                    fill="none"
                    stroke="#a855f7"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    points={makePoints(adjROICs.map((v) => (v !== null ? v * 100 : null)))}
                  />

                  {/* Data Points and Hover Dots */}
                  {chronoPeriods.map((p, idx) => {
                    const x = getX(idx)
                    const isHovered = hoveredYearIndex === idx

                    const opM = opMargins[idx]
                    const fcfM = fcfMargins[idx]
                    const roicVal = adjROICs[idx] !== null ? adjROICs[idx]! * 100 : null

                    return (
                      <g
                        key={p.Year}
                        className="cursor-pointer"
                        onMouseEnter={() => setHoveredYearIndex(idx)}
                        onMouseLeave={() => setHoveredYearIndex(null)}
                      >
                        {isHovered && (
                          <line
                            x1={x}
                            y1={padding.top}
                            x2={x}
                            y2={chartHeight - padding.bottom}
                            stroke="currentColor"
                            className="text-neutral-400 dark:text-neutral-600"
                            strokeDasharray="2,2"
                          />
                        )}

                        {opM !== null && (
                          <circle
                            cx={x}
                            cy={getY(opM)}
                            r={isHovered ? 5 : 3.5}
                            className="fill-emerald-500 stroke-white dark:stroke-neutral-950 stroke-2"
                          />
                        )}

                        {fcfM !== null && (
                          <circle
                            cx={x}
                            cy={getY(fcfM)}
                            r={isHovered ? 5 : 3.5}
                            className="fill-teal-400 stroke-white dark:stroke-neutral-950 stroke-2"
                          />
                        )}

                        {roicVal !== null && (
                          <circle
                            cx={x}
                            cy={getY(roicVal)}
                            r={isHovered ? 5 : 3.5}
                            className="fill-purple-500 stroke-white dark:stroke-neutral-950 stroke-2"
                          />
                        )}

                        <text
                          x={x}
                          y={chartHeight - 10}
                          textAnchor="middle"
                          className={`text-[11px] font-mono transition-colors ${
                            isHovered
                              ? 'fill-neutral-950 dark:fill-white font-semibold'
                              : 'fill-neutral-500'
                          }`}
                        >
                          FY {p.Year}
                        </text>
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>
          )
        })()}

        {/* ========================================================= */}
        {/* TAB 3: CAPITAL ALLOCATION BREAKDOWN                       */}
        {/* ========================================================= */}
        {activeTab === 'capital_allocation' && (() => {
          const sums = chronoPeriods.map((_, idx) => {
            const cap = Math.abs(capexs[idx] || 0)
            const rd = Math.abs(rds[idx] || 0)
            const sbc = Math.abs(sbcs[idx] || 0)
            const fcf = Math.max(0, fcfs[idx] || 0)
            return cap + rd + sbc + fcf
          })
          const maxSum = Math.max(1, ...sums)

          const colWidth = innerWidth / n
          const barW = Math.min(colWidth * 0.55, 60)

          return (
            <div>
              <div className="flex flex-wrap items-center justify-between gap-4 mb-3 text-xs font-mono">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-amber-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">CapEx (Reinvestment)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-blue-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">R&D (Knowledge Capital)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-teal-400" />
                    <span className="text-neutral-700 dark:text-neutral-300">Free Cash Flow Generated</span>
                  </div>
                </div>

                {hoveredYearIndex !== null && (
                  <div className="text-[11px] text-neutral-600 dark:text-neutral-400">
                    <strong className="text-neutral-950 dark:text-white">
                      FY {chronoPeriods[hoveredYearIndex]?.Year}
                    </strong>
                    : CapEx {formatCurrency(capexs[hoveredYearIndex] || 0, true)} • R&D{' '}
                    {formatCurrency(rds[hoveredYearIndex] || 0, true)} • SBC{' '}
                    {formatCurrency(sbcs[hoveredYearIndex] || 0, true)}
                  </div>
                )}
              </div>

              <div className="w-full overflow-x-auto">
                <svg
                  viewBox={`0 0 ${chartWidth} ${chartHeight}`}
                  className="w-full h-48 sm:h-56 select-none"
                >
                  {/* Grid Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((pct, i) => {
                    const y = padding.top + innerHeight * (1 - pct)
                    const val = maxSum * pct
                    return (
                      <g key={i}>
                        <line
                          x1={padding.left}
                          y1={y}
                          x2={chartWidth - padding.right}
                          y2={y}
                          stroke="currentColor"
                          className="text-neutral-200 dark:text-neutral-800/80"
                          strokeDasharray="2,3"
                        />
                        <text
                          x={padding.left - 8}
                          y={y + 3}
                          textAnchor="end"
                          className="text-[10px] font-mono fill-neutral-400"
                        >
                          {formatCurrency(val, true)}
                        </text>
                      </g>
                    )
                  })}

                  {/* Stacked Bars */}
                  {chronoPeriods.map((p, idx) => {
                    const centerX = padding.left + colWidth * idx + colWidth / 2
                    const x = centerX - barW / 2

                    const capVal = Math.abs(capexs[idx] || 0)
                    const rdVal = Math.abs(rds[idx] || 0)
                    const fcfVal = Math.max(0, fcfs[idx] || 0)

                    const hCap = (capVal / maxSum) * innerHeight
                    const hRD = (rdVal / maxSum) * innerHeight
                    const hFCF = (fcfVal / maxSum) * innerHeight

                    let currentY = padding.top + innerHeight

                    const isHovered = hoveredYearIndex === idx

                    return (
                      <g
                        key={p.Year}
                        className="cursor-pointer"
                        onMouseEnter={() => setHoveredYearIndex(idx)}
                        onMouseLeave={() => setHoveredYearIndex(null)}
                      >
                        {isHovered && (
                          <rect
                            x={padding.left + colWidth * idx}
                            y={padding.top}
                            width={colWidth}
                            height={innerHeight}
                            className="fill-neutral-500/5 dark:fill-neutral-400/5"
                          />
                        )}

                        {/* CapEx (Bottom Segment) */}
                        <rect
                          x={x}
                          y={(currentY -= hCap)}
                          width={barW}
                          height={Math.max(1, hCap)}
                          className="fill-amber-500 hover:fill-amber-400 transition-colors"
                        />

                        {/* R&D (Middle Segment) */}
                        <rect
                          x={x}
                          y={(currentY -= hRD)}
                          width={barW}
                          height={Math.max(1, hRD)}
                          className="fill-blue-500 hover:fill-blue-400 transition-colors"
                        />

                        {/* FCF (Top Segment) */}
                        <rect
                          x={x}
                          y={(currentY -= hFCF)}
                          width={barW}
                          height={Math.max(1, hFCF)}
                          className="fill-teal-400 hover:fill-teal-300 transition-colors"
                        />

                        <text
                          x={centerX}
                          y={chartHeight - 10}
                          textAnchor="middle"
                          className={`text-[11px] font-mono transition-colors ${
                            isHovered
                              ? 'fill-neutral-950 dark:fill-white font-semibold'
                              : 'fill-neutral-500'
                          }`}
                        >
                          FY {p.Year}
                        </text>
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>
          )
        })()}

        {/* ========================================================= */}
        {/* TAB 4: BALANCE SHEET & CAPITAL STRUCTURE                  */}
        {/* ========================================================= */}
        {activeTab === 'capital_structure' && (() => {
          const allBS = [...cashes, ...debts, ...equities].filter((v): v is number => v !== null)
          const maxBS = Math.max(1, ...allBS)

          const colWidth = innerWidth / n
          const barGroupWidth = Math.min(colWidth * 0.7, 72)
          const barWidth = barGroupWidth / 3

          return (
            <div>
              <div className="flex flex-wrap items-center justify-between gap-4 mb-3 text-xs font-mono">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-emerald-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">Cash & Equivalents</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-rose-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">Total Borrowings / Debt</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-xs bg-blue-500" />
                    <span className="text-neutral-700 dark:text-neutral-300">Stockholders' Equity</span>
                  </div>
                </div>

                {hoveredYearIndex !== null && (
                  <div className="text-[11px] text-neutral-600 dark:text-neutral-400">
                    <strong className="text-neutral-950 dark:text-white">
                      FY {chronoPeriods[hoveredYearIndex]?.Year}
                    </strong>
                    : Cash {formatCurrency(cashes[hoveredYearIndex] || 0, true)} • Debt{' '}
                    {formatCurrency(debts[hoveredYearIndex] || 0, true)} • Equity{' '}
                    {formatCurrency(equities[hoveredYearIndex] || 0, true)}
                  </div>
                )}
              </div>

              <div className="w-full overflow-x-auto">
                <svg
                  viewBox={`0 0 ${chartWidth} ${chartHeight}`}
                  className="w-full h-48 sm:h-56 select-none"
                >
                  {/* Grid Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((pct, i) => {
                    const y = padding.top + innerHeight * (1 - pct)
                    const val = maxBS * pct
                    return (
                      <g key={i}>
                        <line
                          x1={padding.left}
                          y1={y}
                          x2={chartWidth - padding.right}
                          y2={y}
                          stroke="currentColor"
                          className="text-neutral-200 dark:text-neutral-800/80"
                          strokeDasharray="2,3"
                        />
                        <text
                          x={padding.left - 8}
                          y={y + 3}
                          textAnchor="end"
                          className="text-[10px] font-mono fill-neutral-400"
                        >
                          {formatCurrency(val, true)}
                        </text>
                      </g>
                    )
                  })}

                  {/* Bars per Year */}
                  {chronoPeriods.map((p, idx) => {
                    const centerX = padding.left + colWidth * idx + colWidth / 2
                    const groupLeft = centerX - barGroupWidth / 2

                    const cash = Math.max(0, cashes[idx] || 0)
                    const debt = Math.max(0, debts[idx] || 0)
                    const eq = Math.max(0, equities[idx] || 0)

                    const hCash = (cash / maxBS) * innerHeight
                    const hDebt = (debt / maxBS) * innerHeight
                    const hEq = (eq / maxBS) * innerHeight

                    const isHovered = hoveredYearIndex === idx

                    return (
                      <g
                        key={p.Year}
                        className="cursor-pointer"
                        onMouseEnter={() => setHoveredYearIndex(idx)}
                        onMouseLeave={() => setHoveredYearIndex(null)}
                      >
                        {isHovered && (
                          <rect
                            x={padding.left + colWidth * idx}
                            y={padding.top}
                            width={colWidth}
                            height={innerHeight}
                            className="fill-neutral-500/5 dark:fill-neutral-400/5"
                          />
                        )}

                        {/* Cash Bar */}
                        <rect
                          x={groupLeft}
                          y={padding.top + innerHeight - hCash}
                          width={barWidth - 2}
                          height={Math.max(1, hCash)}
                          rx={2}
                          className="fill-emerald-500 hover:fill-emerald-400 transition-colors"
                        />

                        {/* Debt Bar */}
                        <rect
                          x={groupLeft + barWidth}
                          y={padding.top + innerHeight - hDebt}
                          width={barWidth - 2}
                          height={Math.max(1, hDebt)}
                          rx={2}
                          className="fill-rose-500 hover:fill-rose-400 transition-colors"
                        />

                        {/* Equity Bar */}
                        <rect
                          x={groupLeft + barWidth * 2}
                          y={padding.top + innerHeight - hEq}
                          width={barWidth - 2}
                          height={Math.max(1, hEq)}
                          rx={2}
                          className="fill-blue-500 hover:fill-blue-400 transition-colors"
                        />

                        <text
                          x={centerX}
                          y={chartHeight - 10}
                          textAnchor="middle"
                          className={`text-[11px] font-mono transition-colors ${
                            isHovered
                              ? 'fill-neutral-950 dark:fill-white font-semibold'
                              : 'fill-neutral-500'
                          }`}
                        >
                          FY {p.Year}
                        </text>
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>
          )
        })()}
      </div>
    </div>
  )
}
