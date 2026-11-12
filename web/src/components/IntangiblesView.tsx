import React from 'react'
import { SummaryResponse, SummaryValue } from '../types'
import { formatCurrency, formatPercent } from '../lib/format'

interface IntangiblesViewProps {
  summary: SummaryResponse
  compact: boolean
}

export const IntangiblesView: React.FC<IntangiblesViewProps> = ({ summary, compact }) => {
  const { intangibles, periods } = summary
  const curr = intangibles.Currency || 'USD'

  const latestVal = (arr: SummaryValue[] | undefined): number | undefined => {
    if (!arr || arr.length === 0 || !arr[0]?.Present) return undefined
    return arr[0].Value
  }

  const latestIntangibleCap = latestVal(intangibles.IntangibleCapital)
  const latestIntangibleAmort = latestVal(intangibles.IntangibleAmort)
  const latestAdjEBIT = latestVal(intangibles.AdjustedEBIT)
  const latestAdjInvestedCap = latestVal(intangibles.AdjustedInvestedCap)
  const latestAdjROIC = latestVal(intangibles.AdjustedROIC)
  const latestAdjOwnerEarnings = latestVal(intangibles.AdjustedOwnerEarnings)

  const findMetricVal = (name: string): number | undefined => {
    const m = summary.metrics.find((x) => x.Name === name)
    return m?.Values?.[0]?.Present ? m.Values[0].Value : undefined
  }

  const latestRevenue = findMetricVal('Revenue') || 0
  const latestGAAPOperatingIncome = findMetricVal('Operating Income')
  const latestRD = findMetricVal('Research & Development') || 0
  const latestSGA = findMetricVal('Selling, General & Administrative') || 0
  const latestBrandCapital = latestSGA * 0.3
  const latestGAAPFCF = findMetricVal('Free Cash Flow')
  const latestSBC = findMetricVal('Stock-Based Compensation')
  const latestEquity = findMetricVal("Stockholders' Equity") || 0
  const latestDebt = findMetricVal('Debt / Borrowings') || 0
  const latestCash = findMetricVal('Cash & Equivalents') || 0

  // -------------------------------------------------------------
  // DuPont ROIC Decomposition Calculations
  // ROIC = NOPAT Margin * Invested Capital Turnover
  // -------------------------------------------------------------
  const taxRate = 0.21

  // 1. GAAP DuPont
  const gaapNOPAT = latestGAAPOperatingIncome !== undefined ? latestGAAPOperatingIncome * (1 - taxRate) : 0
  const gaapInvestedCap = Math.max(1, latestEquity + latestDebt - latestCash)
  const gaapNOPATMargin = latestRevenue > 0 ? gaapNOPAT / latestRevenue : 0
  const gaapEBITMargin = latestRevenue > 0 && latestGAAPOperatingIncome !== undefined ? latestGAAPOperatingIncome / latestRevenue : 0
  const gaapCapitalTurnover = gaapInvestedCap > 0 && latestRevenue > 0 ? latestRevenue / gaapInvestedCap : 0
  const computedGAAPROIC = gaapInvestedCap > 0 ? gaapNOPAT / gaapInvestedCap : 0

  // 2. Peters & Taylor Adjusted DuPont
  const adjNOPAT = latestAdjEBIT !== undefined ? latestAdjEBIT * (1 - taxRate) : 0
  const adjInvestedCap = latestAdjInvestedCap && latestAdjInvestedCap > 0 ? latestAdjInvestedCap : gaapInvestedCap + (latestIntangibleCap || 0)
  const adjNOPATMargin = latestRevenue > 0 ? adjNOPAT / latestRevenue : 0
  const adjEBITMargin = latestRevenue > 0 && latestAdjEBIT !== undefined ? latestAdjEBIT / latestRevenue : 0
  const adjCapitalTurnover = adjInvestedCap > 0 && latestRevenue > 0 ? latestRevenue / adjInvestedCap : 0
  const computedAdjROIC = latestAdjROIC !== undefined ? latestAdjROIC : (adjInvestedCap > 0 ? adjNOPAT / adjInvestedCap : 0)

  // Diagnostics
  const roicSpread = computedAdjROIC - computedGAAPROIC
  const primaryDriver =
    adjEBITMargin > 0.25
      ? 'Pricing Power & Value Capture'
      : adjCapitalTurnover > 1.2
      ? 'Capital Velocity & Balance Sheet Efficiency'
      : 'Balanced Operating Structure'

  return (
    <div className="space-y-6 animate-in fade-in duration-150 font-sans">
      {/* Editorial Overview & Method Line */}
      <div className="border border-neutral-200 dark:border-neutral-800 p-4 bg-neutral-50/50 dark:bg-neutral-950 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs">
        <div>
          <span className="font-semibold text-neutral-900 dark:text-white uppercase tracking-wider text-[11px] font-mono block">
            Peters & Taylor (2017) Knowledge & Brand Capitalization
          </span>
          <p className="text-neutral-500 font-sans mt-0.5 text-xs max-w-2xl">
            Capitalizes 100% of R&D (5-year straight-line amortization) and 30% of SG&A customer acquisition (3-year amortization), reversing GAAP expensing to reveal true economic invested capital and ROIC.
          </p>
        </div>

        <div className="flex items-center gap-4 text-xs font-mono text-neutral-600 dark:text-neutral-400 self-start sm:self-auto border-t sm:border-t-0 pt-2 sm:pt-0 border-neutral-200 dark:border-neutral-800">
          <div>
            <span className="text-neutral-400 text-[10px] block">R&D Amortization</span>
            <span className="font-medium text-neutral-900 dark:text-white">5 Years S-L</span>
          </div>
          <div className="h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
          <div>
            <span className="text-neutral-400 text-[10px] block">Brand SG&A</span>
            <span className="font-medium text-neutral-900 dark:text-white">3 Years (30%)</span>
          </div>
        </div>
      </div>

      {/* Two-Column Structured Reconciliation: Waterfall Bridge + DuPont Tree */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Column 1: EBIT Capitalization Reconciliation Bridge */}
        <div className="border border-neutral-200/80 dark:border-white/10 p-4 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md space-y-3 font-mono text-xs">
          <div className="flex items-center justify-between border-b border-neutral-200/80 dark:border-white/10 pb-2">
            <span className="font-sans font-medium text-xs text-neutral-950 dark:text-white">
              EBIT Capitalization Reconciliation (Latest FY)
            </span>
            <span className="text-[10px] text-neutral-400">Standardized EDGAR</span>
          </div>

          <div className="space-y-1.5 divide-y divide-neutral-100 dark:divide-neutral-900">
            <div className="flex justify-between py-1">
              <span className="text-neutral-600 dark:text-neutral-400 font-sans">Reported GAAP Operating Income</span>
              <span className="font-semibold text-neutral-900 dark:text-white">
                {formatCurrency(latestGAAPOperatingIncome, compact, curr)}
              </span>
            </div>

            <div className="flex justify-between py-1 text-emerald-600 dark:text-emerald-400">
              <span className="font-sans">(+) 100% R&D Expenditure Capitalized</span>
              <span>+{formatCurrency(latestRD, compact, curr)}</span>
            </div>

            <div className="flex justify-between py-1 text-emerald-600 dark:text-emerald-400">
              <span className="font-sans">(+) 30% SG&A Brand Capital Capitalized</span>
              <span>+{formatCurrency(latestBrandCapital, compact, curr)}</span>
            </div>

            <div className="flex justify-between py-1 text-rose-600 dark:text-rose-400">
              <span className="font-sans">(-) Intangible Asset Amortization</span>
              <span>-{formatCurrency(latestIntangibleAmort, compact, curr)}</span>
            </div>

            <div className="flex justify-between py-1.5 font-medium border-t-2 border-neutral-200 dark:border-neutral-800 text-neutral-950 dark:text-white">
              <span className="font-sans font-semibold">(=) Intangible-Adjusted EBIT</span>
              <span className="font-semibold">
                {formatCurrency(latestAdjEBIT, compact, curr)}
              </span>
            </div>
          </div>
        </div>

        {/* Column 2: DuPont ROIC Decomposition Tree */}
        <div className="border border-neutral-200/80 dark:border-white/10 p-4 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md space-y-3 font-mono text-xs rounded-xl">
          <div className="flex items-center justify-between border-b border-neutral-200/80 dark:border-white/10 pb-2">
            <span className="font-sans font-medium text-xs text-neutral-950 dark:text-white">
              Visual DuPont ROIC Tree
            </span>
            <span className="text-[10px] text-neutral-400">
              NOPAT Margin × Invested Capital Turnover
            </span>
          </div>

          {/* Visual Interactive Tree Layout */}
          <div className="space-y-3 pt-1">
            {/* Level 1: Root Node (ROIC Spread) */}
            <div className="flex items-center justify-between p-2.5 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-neutral-50/80 dark:bg-neutral-900/60">
              <div>
                <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans block">
                  Peters & Taylor Adjusted ROIC
                </span>
                <span className="text-base font-bold text-neutral-950 dark:text-white">
                  {formatPercent(computedAdjROIC)}
                </span>
              </div>
              <div className="text-right">
                <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans block">
                  GAAP Reported ROIC
                </span>
                <span className="text-xs text-neutral-600 dark:text-neutral-400">
                  {formatPercent(computedGAAPROIC)}{' '}
                  <span className={roicSpread >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'}>
                    ({roicSpread >= 0 ? '+' : ''}{(roicSpread * 100).toFixed(1)}% pts)
                  </span>
                </span>
              </div>
            </div>

            {/* Tree Branch Visual Connector */}
            <div className="flex justify-around items-center px-6 text-neutral-400 dark:text-neutral-600 text-xs font-mono">
              <span>┌─────────────</span>
              <span className="text-[10px] uppercase font-sans tracking-widest text-neutral-400">Decomposition</span>
              <span>─────────────┐</span>
            </div>

            {/* Level 2: Two Branches */}
            <div className="grid grid-cols-2 gap-2.5">
              {/* Branch 1: NOPAT Margin */}
              <div className="p-2.5 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-950 space-y-1">
                <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans block">
                  1. NOPAT Margin
                </span>
                <div className="text-sm font-semibold text-neutral-900 dark:text-white">
                  {(adjNOPATMargin * 100).toFixed(1)}%
                </div>
                <div className="text-[10px] text-neutral-500 font-sans pt-0.5 border-t border-neutral-100 dark:border-neutral-900">
                  GAAP: {(gaapNOPATMargin * 100).toFixed(1)}% • EBIT Margin: {(adjEBITMargin * 100).toFixed(1)}%
                </div>
              </div>

              {/* Branch 2: Invested Capital Turnover */}
              <div className="p-2.5 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-950 space-y-1">
                <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans block">
                  2. Capital Turnover
                </span>
                <div className="text-sm font-semibold text-neutral-900 dark:text-white">
                  {adjCapitalTurnover.toFixed(2)}x
                </div>
                <div className="text-[10px] text-neutral-500 font-sans pt-0.5 border-t border-neutral-100 dark:border-neutral-900">
                  GAAP: {gaapCapitalTurnover.toFixed(2)}x • Inv. Cap: {formatCurrency(adjInvestedCap, true, curr)}
                </div>
              </div>
            </div>

            {/* Diagnostic Moat Summary */}
            <div className="flex justify-between items-center text-[11px] text-neutral-500 pt-1 font-sans border-t border-neutral-200/60 dark:border-neutral-800/60">
              <span>Primary Moat Driver: <strong className="text-neutral-900 dark:text-white">{primaryDriver}</strong></span>
              <span className="font-mono text-neutral-700 dark:text-neutral-300">
                Spread: {roicSpread >= 0 ? '+' : ''}{(roicSpread * 100).toFixed(1)}%
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Multi-Year Intangibles Schedule Table */}
      <div className="border border-neutral-200/80 dark:border-white/10 overflow-hidden bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md">
        <div className="px-4 py-2.5 border-b border-neutral-200/80 dark:border-white/10 bg-neutral-50/60 dark:bg-white/[0.04] flex justify-between items-center text-xs">
          <span className="font-mono text-[10px] uppercase tracking-wider text-neutral-500 font-semibold">
            Historical Intangibles & Invested Capital Schedule
          </span>
          <span className="text-[11px] text-neutral-400 font-mono">
            {periods.length} Fiscal Periods
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead>
              <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-400 bg-neutral-50/50 dark:bg-neutral-950 font-sans">
                <th className="py-2 px-4 font-normal text-neutral-500">Metric Line Item</th>
                {periods.map((p) => (
                  <th key={p.Year} className="py-2 px-3 font-normal text-right tabular-nums">
                    FY {p.Year}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-700 dark:text-neutral-300 font-sans font-medium">
                  Capitalized Intangible Stock
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                    {formatCurrency(intangibles.IntangibleCapital?.[i]?.Value, compact, curr)}
                  </td>
                ))}
              </tr>

              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400 font-sans">
                  Intangible Amortization (Decay)
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-600 dark:text-neutral-400">
                    {formatCurrency(intangibles.IntangibleAmort?.[i]?.Value, compact, curr)}
                  </td>
                ))}
              </tr>

              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-700 dark:text-neutral-300 font-sans font-medium">
                  Adjusted Operating Income (EBIT)
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                    {formatCurrency(intangibles.AdjustedEBIT?.[i]?.Value, compact, curr)}
                  </td>
                ))}
              </tr>

              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400 font-sans">
                  Adjusted Invested Capital
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-600 dark:text-neutral-400">
                    {formatCurrency(intangibles.AdjustedInvestedCap?.[i]?.Value, compact, curr)}
                  </td>
                ))}
              </tr>

              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-900 dark:text-white font-sans font-semibold">
                  Intangible-Adjusted ROIC
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-950 dark:text-white font-semibold">
                    {formatPercent(intangibles.AdjustedROIC?.[i]?.Value)}
                  </td>
                ))}
              </tr>

              <tr className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                <td className="py-2 px-4 text-neutral-600 dark:text-neutral-400 font-sans">
                  Adjusted Owner Earnings
                </td>
                {periods.map((_, i) => (
                  <td key={i} className="py-2 px-3 text-right tabular-nums text-neutral-700 dark:text-neutral-300">
                    {formatCurrency(intangibles.AdjustedOwnerEarnings?.[i]?.Value, compact, curr)}
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
