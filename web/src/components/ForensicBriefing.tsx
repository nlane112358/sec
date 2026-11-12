import React from 'react'
import { AlertTriangle, ShieldCheck, Sparkles, TrendingUp } from 'lucide-react'
import { QuoteResponse, SummaryResponse } from '../types'

interface ForensicBriefingProps {
  summary: SummaryResponse | null
  quote: QuoteResponse | null
}

type Tone = 'positive' | 'watch' | 'risk' | 'neutral'

interface Finding {
  label: string
  text: string
  tone: Tone
  icon: React.ReactNode
}

const toneStyles: Record<Tone, { border: string; icon: string; label: string }> = {
  positive: {
    border: 'border-emerald-500/25 bg-emerald-500/[0.035]',
    icon: 'text-emerald-600 dark:text-emerald-400',
    label: 'text-emerald-700 dark:text-emerald-400',
  },
  watch: {
    border: 'border-amber-500/25 bg-amber-500/[0.035]',
    icon: 'text-amber-600 dark:text-amber-400',
    label: 'text-amber-700 dark:text-amber-400',
  },
  risk: {
    border: 'border-rose-500/25 bg-rose-500/[0.035]',
    icon: 'text-rose-600 dark:text-rose-400',
    label: 'text-rose-700 dark:text-rose-400',
  },
  neutral: {
    border: 'border-neutral-200 dark:border-white/10 bg-white/40 dark:bg-white/[0.02]',
    icon: 'text-neutral-400',
    label: 'text-neutral-500 dark:text-neutral-400',
  },
}

const metricValue = (summary: SummaryResponse, name: string): number | undefined => {
  const value = summary.metrics.find((metric) => metric.Name === name)?.Values?.[0]
  return value?.Present && Number.isFinite(value.Value) ? value.Value : undefined
}

const inflection = (summary: SummaryResponse, name: string) =>
  summary.inflections?.find((item) => item.metric === name)

const signedPercent = (value: number): string =>
  `${value > 0 ? '+' : ''}${(value * 100).toFixed(1)}%`

const signedBps = (value: number): string =>
  `${value > 0 ? '+' : ''}${Math.round(value)} bps`

const latestQuarterLabel = (summary: SummaryResponse): string => {
  const reportedPeriod = summary.inflections?.find((item) => item.metric === 'Revenue')?.period_label
  if (reportedPeriod) return reportedPeriod
  const end = summary.periods?.[0]?.End
  if (!end || end.length < 7) return 'the latest reported period'
  const month = Number(end.slice(5, 7))
  const year = end.slice(0, 4)
  if (!Number.isFinite(month) || month < 1 || month > 12) return 'the latest reported period'
  return `Q${Math.ceil(month / 3)} ${year}`
}

const buildFindings = (summary: SummaryResponse, quote: QuoteResponse | null): Finding[] => {
  const quarter = latestQuarterLabel(summary)
  const revenueTrend = inflection(summary, 'Revenue')
  const operatingIncomeTrend = inflection(summary, 'Operating Income')
  const researchTrend = inflection(summary, 'Research & Development')
  const receivablesTrend = inflection(summary, 'Accounts Receivable')

  const currentRevenue = revenueTrend?.latest_value
  const priorRevenue = revenueTrend?.prior_year_quarter_value
  const currentOperatingIncome = operatingIncomeTrend?.latest_value
  const priorOperatingIncome = operatingIncomeTrend?.prior_year_quarter_value
  const marginChangeBps =
    currentRevenue !== undefined && currentRevenue > 0 &&
    priorRevenue !== undefined && priorRevenue > 0 &&
    currentOperatingIncome !== undefined && priorOperatingIncome !== undefined
      ? ((currentOperatingIncome / currentRevenue - priorOperatingIncome / priorRevenue) * 10000)
      : undefined

  const rdIntensityChangeBps =
    currentRevenue !== undefined && currentRevenue > 0 &&
    priorRevenue !== undefined && priorRevenue > 0 &&
    researchTrend?.latest_value !== undefined && researchTrend.prior_year_quarter_value !== undefined
      ? ((Math.abs(researchTrend.latest_value) / currentRevenue -
          Math.abs(researchTrend.prior_year_quarter_value) / priorRevenue) * 10000)
      : undefined

  let operatingText = 'A matched operating-margin comparison is not available for the latest reported period.'
  let operatingTone: Tone = 'neutral'
  let operatingLabel = 'INFLECTION SCREEN'
  if (marginChangeBps !== undefined) {
    if (marginChangeBps >= 200) {
      operatingTone = 'positive'
      operatingLabel = 'GREEN FLAG'
      operatingText =
        rdIntensityChangeBps !== undefined && rdIntensityChangeBps > 0
          ? `Operating margin expanded ${signedBps(marginChangeBps)} YoY in ${quarter} while R&D intensity rose ${signedBps(rdIntensityChangeBps)}—a classic operating leverage inflection.`
          : `Operating margin expanded ${signedBps(marginChangeBps)} YoY in ${quarter}; the operating-leverage trigger cleared.`
    } else if (marginChangeBps < -100) {
      operatingTone = 'watch'
      operatingLabel = 'MARGIN WATCH'
      operatingText = `Operating margin contracted ${signedBps(marginChangeBps)} YoY in ${quarter}; the 200 bps expansion trigger did not fire.`
    } else {
      operatingText = `Operating margin changed ${signedBps(marginChangeBps)} YoY in ${quarter}; the 200 bps expansion trigger did not fire.`
    }
  }

  const revenue = metricValue(summary, 'Revenue')
  const sbc = metricValue(summary, 'Stock-Based Compensation')
  const fcf = metricValue(summary, 'Free Cash Flow')
  const shares = metricValue(summary, 'Diluted Weighted Average Shares') ?? metricValue(summary, 'Shares Outstanding')
  const ownerEarnings = metricValue(summary, 'SBC-Adjusted FCF Proxy')
  const marketCap = quote && quote.ticker === summary.ticker && quote.price > 0 && shares !== undefined && shares > 0
    ? quote.price * shares
    : undefined
  const sbcOfRevenue = revenue !== undefined && revenue > 0 && sbc !== undefined
    ? (Math.abs(sbc) / revenue) * 100
    : undefined
  const sbcOfFCF = fcf !== undefined && fcf > 0 && sbc !== undefined
    ? (Math.abs(sbc) / fcf) * 100
    : undefined
  const fcfYield = marketCap !== undefined && fcf !== undefined ? (fcf / marketCap) * 100 : undefined
  const ownerYield = marketCap !== undefined && ownerEarnings !== undefined
    ? (ownerEarnings / marketCap) * 100
    : undefined
  const yieldGap = fcfYield !== undefined && ownerYield !== undefined ? fcfYield - ownerYield : undefined
  const sbcFlag =
    (sbcOfRevenue !== undefined && sbcOfRevenue >= 10) ||
    (sbcOfFCF !== undefined && sbcOfFCF >= 25) ||
    (yieldGap !== undefined && yieldGap >= 1.5)

  let sbcText = 'SBC exposure is not available in the reported SEC facts.'
  let sbcTone: Tone = 'neutral'
  let sbcLabel = 'CAPITAL QUALITY'
  if (sbcOfRevenue !== undefined) {
    const burden = `SBC is ${sbcOfRevenue.toFixed(1)}% of Revenue${sbcOfFCF !== undefined ? ` and ${sbcOfFCF.toFixed(1)}% of reported FCF` : ''}`
    const yieldComparison =
      fcfYield !== undefined && ownerYield !== undefined
        ? `; true owner earnings yield is ${ownerYield.toFixed(1)}% vs. ${fcfYield.toFixed(1)}% headline FCF yield`
        : ''
    if (sbcFlag) {
      sbcTone = 'watch'
      sbcLabel = 'YELLOW FLAG'
      sbcText = `${burden}${yieldComparison}.`
    } else if (sbcOfFCF !== undefined) {
      sbcText = `${burden}${yieldComparison}; both SBC screens are below 10% of Revenue and 25% of FCF.`
    } else {
      sbcText = `${burden}${yieldComparison}; the 10% of Revenue screen did not fire, but FCF burden is unavailable.`
    }
  } else if (sbcOfFCF !== undefined) {
    const yieldComparison =
      fcfYield !== undefined && ownerYield !== undefined
        ? `; true owner earnings yield is ${ownerYield.toFixed(1)}% vs. ${fcfYield.toFixed(1)}% headline FCF yield`
        : ''
    if (sbcFlag) {
      sbcTone = 'watch'
      sbcLabel = 'YELLOW FLAG'
      sbcText = `SBC is ${sbcOfFCF.toFixed(1)}% of reported FCF${yieldComparison}; the elevated-SBC screen is active, while revenue intensity is unavailable.`
    } else {
      sbcText = `SBC is ${sbcOfFCF.toFixed(1)}% of reported FCF${yieldComparison}; below the 25% FCF screen, while revenue intensity is unavailable.`
    }
  } else if (yieldGap !== undefined && yieldGap >= 1.5) {
    sbcTone = 'watch'
    sbcLabel = 'YELLOW FLAG'
    sbcText = `True owner earnings yield is ${ownerYield?.toFixed(1)}% vs. ${fcfYield?.toFixed(1)}% headline FCF yield, a ${yieldGap.toFixed(1)} point gap consistent with material SBC drag.`
  }

  const netIncome = metricValue(summary, 'Net Income')
  const operatingCashFlow = metricValue(summary, 'Operating Cash Flow')
  const assets = metricValue(summary, 'Total Assets')
  const sloanRatio =
    netIncome !== undefined && operatingCashFlow !== undefined && assets !== undefined && assets > 0
      ? (netIncome - operatingCashFlow) / assets
      : undefined
  const receivablesGrowth = receivablesTrend?.yoy_growth
  const revenueGrowth = revenueTrend?.yoy_growth
  const receivablesGap =
    receivablesGrowth !== undefined && revenueGrowth !== undefined
      ? receivablesGrowth - revenueGrowth
      : undefined
  let accrualText = 'Cash-conversion data is not sufficient to assess the Sloan Accrual Ratio.'
  let accrualTone: Tone = 'neutral'
  let accrualLabel = 'CASH CONVERSION'
  if (sloanRatio !== undefined && sloanRatio > 0.1) {
    accrualTone = 'risk'
    accrualLabel = 'RED FLAG'
    accrualText =
      receivablesGap !== undefined && receivablesGap >= 0.1 && receivablesGrowth !== undefined && revenueGrowth !== undefined
        ? `Receivables grew ${signedPercent(receivablesGrowth)} YoY vs. Revenue ${signedPercent(revenueGrowth)}; Sloan Accrual Ratio is ${(sloanRatio * 100).toFixed(1)}%, above the 10% danger threshold.`
        : `Sloan Accrual Ratio is ${(sloanRatio * 100).toFixed(1)}%, above the 10% danger threshold; reported earnings materially exceed operating cash flow.`
  } else if (receivablesGap !== undefined && receivablesGap >= 0.1 && receivablesGrowth !== undefined && revenueGrowth !== undefined) {
    accrualTone = 'watch'
    accrualLabel = 'WORKING CAPITAL WATCH'
    accrualText = `Receivables growth outpaced Revenue by ${(receivablesGap * 100).toFixed(1)} pp (${signedPercent(receivablesGrowth)} vs. ${signedPercent(revenueGrowth)}); ${sloanRatio !== undefined ? `Sloan is ${(sloanRatio * 100).toFixed(1)}%, below the 10% danger threshold` : 'the Sloan ratio is unavailable'}.`
  } else if (sloanRatio !== undefined && receivablesGap !== undefined && receivablesGrowth !== undefined && revenueGrowth !== undefined) {
    accrualText = `Sloan Accrual Ratio is ${(sloanRatio * 100).toFixed(1)}%, below the 10% danger threshold; Receivables grew ${signedPercent(receivablesGrowth)} vs. Revenue ${signedPercent(revenueGrowth)}.`
  } else if (sloanRatio !== undefined) {
    accrualText = `Sloan Accrual Ratio is ${(sloanRatio * 100).toFixed(1)}%, below the 10% danger threshold; receivables growth comparison is unavailable.`
  }

  return [
    { label: operatingLabel, text: operatingText, tone: operatingTone, icon: <TrendingUp className="h-3.5 w-3.5" /> },
    { label: sbcLabel, text: sbcText, tone: sbcTone, icon: <AlertTriangle className="h-3.5 w-3.5" /> },
    {
      label: accrualLabel,
      text: accrualText,
      tone: accrualTone,
      icon: accrualTone === 'risk' ? <AlertTriangle className="h-3.5 w-3.5" /> : <ShieldCheck className="h-3.5 w-3.5" />,
    },
  ]
}

export const ForensicBriefing: React.FC<ForensicBriefingProps> = ({ summary, quote }) => {
  if (!summary) return null

  const findings = buildFindings(summary, quote)
  const reportedPeriod = latestQuarterLabel(summary)
  const basisLabel = reportedPeriod.startsWith('FY ')
    ? `latest annual filing · ${reportedPeriod}`
    : `TTM cash flow · ${reportedPeriod}`

  return (
    <section
      aria-labelledby="forensic-briefing-title"
      className="rounded-2xl border border-neutral-200/80 dark:border-white/10 bg-white/65 dark:bg-[#030611]/55 backdrop-blur-md px-4 py-4 sm:px-5"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 mb-3">
        <div>
          <h2 id="forensic-briefing-title" className="flex items-center gap-2 text-sm font-semibold tracking-tight text-neutral-900 dark:text-neutral-100">
            <Sparkles className="h-4 w-4 text-indigo-500" />
            Forensic &amp; Inflection Summary
          </h2>
          <p className="mt-1 text-[9px] text-neutral-400">
            Screens: margin expansion ≥200 bps · SBC ≥10% Revenue or ≥25% positive FCF · receivables gap ≥10 pp · Sloan &gt;10%
          </p>
        </div>
        <span className="font-mono text-[9px] uppercase tracking-wider text-neutral-400">
          Rule based · {basisLabel}
        </span>
      </div>

      <ul className="grid grid-cols-1 gap-2 lg:grid-cols-3">
        {findings.map((finding) => {
          const styles = toneStyles[finding.tone]
          return (
            <li key={finding.label} className={`min-w-0 rounded-xl border px-3 py-3 ${styles.border}`}>
              <div className={`mb-1.5 flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-wider ${styles.label}`}>
                <span className={styles.icon}>{finding.icon}</span>
                {finding.label}
              </div>
              <p className="text-[11px] leading-relaxed text-neutral-700 dark:text-neutral-300">
                {finding.text}
              </p>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
