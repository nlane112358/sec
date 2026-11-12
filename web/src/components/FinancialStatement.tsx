import React, { useState } from 'react'
import {
  Download,
  FileSpreadsheet,
  Eye,
  EyeOff,
  FileText,
  FileSearch,
  ExternalLink,
  X,
  Search,
  BarChart3,
  Percent,
  TrendingUp,
  ShieldCheck,
  AlertTriangle,
  Printer,
  Sparkles,
} from 'lucide-react'
import { SummaryResponse, SummaryMetric, SummaryValue, RawFactItem, AuditResponse } from '../types'
import { formatCurrency, formatPercent, formatShares } from '../lib/format'
import { FinancialCharts } from './FinancialCharts'

interface FinancialStatementProps {
  summary: SummaryResponse
  compact: boolean
  onToggleCompact: () => void
  cadence?: 'annual' | 'quarterly' | 'ttm'
  onSelectCadence?: (cadence: 'annual' | 'quarterly' | 'ttm') => void
}

interface MetricGroup {
  title: string
  subtitle: string
  metrics: string[]
}

const groups: MetricGroup[] = [
  {
    title: 'Income & Operations',
    subtitle: 'Revenue, core operational expenses, GAAP operating and net income',
    metrics: [
      'Revenue',
      'Cost of Revenue',
      'Selling, General & Administrative',
      'Research & Development',
      'Operating Income',
      'Net Income',
      'Income Before Tax',
      'Income Tax Expense',
      'Interest Expense',
    ],
  },
  {
    title: 'Peters & Taylor Intangibles Model',
    subtitle: 'R&D and SG&A capitalized into knowledge and brand assets (Peters & Taylor / Damodaran)',
    metrics: [
      'Amortization of Intangibles',
      'Adjusted Operating Income',
      'Intangible Capital',
      'Adjusted Invested Capital',
      'Intangible-Adjusted ROIC',
    ],
  },
  {
    title: 'Cash Flow & Capital Allocation',
    subtitle: 'Operating cash flow, CapEx, free cash flow, and stock-based compensation proxy',
    metrics: [
      'Operating Cash Flow',
      'Capital Expenditure',
      'Free Cash Flow',
      'Stock-Based Compensation',
      'SBC-Adjusted FCF Proxy',
    ],
  },
  {
    title: 'Balance Sheet & Capital Structure',
    subtitle: 'Liquidity, leverage, reported equity, and total balance sheet footprint',
    metrics: [
      'Cash & Equivalents',
      'Current Assets',
      'Current Liabilities',
      'Debt / Borrowings',
      'Stockholders\' Equity',
      'Total Assets',
    ],
  },
  {
    title: 'Per Share & Dilution',
    subtitle: 'Period-end outstanding and diluted weighted-average share counts',
    metrics: ['Shares Outstanding', 'Diluted Weighted Average Shares'],
  },
]

export const FinancialStatement: React.FC<FinancialStatementProps> = ({
  summary,
  compact,
  onToggleCompact,
  cadence = 'annual',
  onSelectCadence,
}) => {
  const [highlightSBC, setHighlightSBC] = useState(true)
  const [commonSize, setCommonSize] = useState(false)
  const [showCharts, setShowCharts] = useState(true)
  const [auditOpen, setAuditOpen] = useState(false)
  const [auditFacts, setAuditFacts] = useState<RawFactItem[]>([])
  const [loadingAudit, setLoadingAudit] = useState(false)
  const [selectedAuditMetric, setSelectedAuditMetric] = useState<string | null>(null)
  const [auditSearch, setAuditSearch] = useState('')

  const metricMap = new Map<string, SummaryMetric>()
  summary.metrics.forEach((m) => metricMap.set(m.Name, m))

  const periods = summary.periods || []

  // Load audit XBRL facts
  const openAuditDrawer = (metricName?: string) => {
    setSelectedAuditMetric(metricName || null)
    setAuditOpen(true)
    if (auditFacts.length === 0 && !loadingAudit) {
      setLoadingAudit(true)
      fetch(`/api/company/${summary.ticker}/audit`)
        .then((res) => (res.ok ? res.json() : null))
        .then((data: AuditResponse | null) => {
          if (data?.raw_facts) {
            setAuditFacts(data.raw_facts)
          }
        })
        .catch(() => {})
        .finally(() => setLoadingAudit(false))
    }
  }

  // -------------------------------------------------------------
  // Forensic Accounting Health Computations
  // -------------------------------------------------------------
  const latestNetIncome = metricMap.get('Net Income')?.Values?.[0]?.Value
  const latestOCF = metricMap.get('Operating Cash Flow')?.Values?.[0]?.Value
  const latestAssets = metricMap.get('Total Assets')?.Values?.[0]?.Value
  const latestRevenue = metricMap.get('Revenue')?.Values?.[0]?.Value
  const latestSBC = metricMap.get('Stock-Based Compensation')?.Values?.[0]?.Value

  // 1. Sloan Accrual Ratio = (Net Income - Operating Cash Flow) / Total Assets
  const hasSloan =
    latestNetIncome !== undefined &&
    latestOCF !== undefined &&
    latestAssets !== undefined &&
    latestAssets > 0
  const sloanRatio = hasSloan ? (latestNetIncome - latestOCF) / latestAssets : undefined

  // 2. Dilution / Cannibal Calculation (latest vs oldest period)
  const sharesMetric =
    metricMap.get('Diluted Weighted Average Shares') || metricMap.get('Shares Outstanding')
  const validShareValues = sharesMetric?.Values?.filter((v) => v && v.Present && v.Value > 0) || []
  const shareChange =
    validShareValues.length >= 2
      ? (validShareValues[0].Value - validShareValues[validShareValues.length - 1].Value) /
        validShareValues[validShareValues.length - 1].Value
      : undefined

  const sbcPctOfRev =
    latestRevenue && latestRevenue > 0 && latestSBC !== undefined
      ? (latestSBC / latestRevenue) * 100
      : undefined

  // 3. Liquidity & Working Capital Health
  const currentAssets = metricMap.get('Current Assets')?.Values?.[0]?.Value
  const currentLiabilities = metricMap.get('Current Liabilities')?.Values?.[0]?.Value
  const currentRatio =
    currentAssets && currentLiabilities && currentLiabilities > 0
      ? currentAssets / currentLiabilities
      : undefined

  // 4. Piotroski F-Score (0 - 9)
  const netIncomeValues = metricMap.get('Net Income')?.Values || []
  const ocfValues = metricMap.get('Operating Cash Flow')?.Values || []
  const assetValues = metricMap.get('Total Assets')?.Values || []
  const debtValues = metricMap.get('Debt / Borrowings')?.Values || []
  const caValues = metricMap.get('Current Assets')?.Values || []
  const clValues = metricMap.get('Current Liabilities')?.Values || []
  const revValues = metricMap.get('Revenue')?.Values || []
  const corValues = metricMap.get('Cost of Revenue')?.Values || []
  const opIncomeValues = metricMap.get('Operating Income')?.Values || []
  const equityValues = metricMap.get("Stockholders' Equity")?.Values || []

  const computePiotroski = (): { score: number; signals: { name: string; pass: boolean }[] } | null => {
    if (netIncomeValues.length < 2 || ocfValues.length < 2 || assetValues.length < 2) return null
    const ni0 = netIncomeValues[0]?.Value
    const ni1 = netIncomeValues[1]?.Value
    const ocf0 = ocfValues[0]?.Value
    const a0 = assetValues[0]?.Value
    const a1 = assetValues[1]?.Value
    if (ni0 === undefined || ocf0 === undefined || !a0 || a0 <= 0) return null

    const roa0 = ni0 / a0
    const roa1 = ni1 !== undefined && a1 && a1 > 0 ? ni1 / a1 : undefined

    const s1 = ni0 > 0
    const s2 = ocf0 > 0
    const s3 = roa1 !== undefined ? roa0 > roa1 : roa0 > 0
    const s4 = ocf0 > ni0

    const d0 = debtValues[0]?.Value || 0
    const d1 = debtValues[1]?.Value || 0
    const lev0 = d0 / a0
    const lev1 = a1 && a1 > 0 ? d1 / a1 : lev0
    const s5 = lev0 <= lev1

    const cr0 = caValues[0]?.Value && clValues[0]?.Value && clValues[0].Value > 0 ? caValues[0].Value / clValues[0].Value : 1
    const cr1 = caValues[1]?.Value && clValues[1]?.Value && clValues[1].Value > 0 ? caValues[1].Value / clValues[1].Value : 1
    const s6 = cr0 >= cr1

    const sh0 = validShareValues[0]?.Value
    const sh1 = validShareValues[1]?.Value
    const s7 = sh0 && sh1 ? sh0 <= sh1 * 1.01 : true

    const rev0 = revValues[0]?.Value || 0
    const rev1 = revValues[1]?.Value || 0
    const cor0 = corValues[0]?.Value || 0
    const cor1 = corValues[1]?.Value || 0
    const gm0 = rev0 > 0 ? (rev0 - cor0) / rev0 : 0
    const gm1 = rev1 > 0 ? (rev1 - cor1) / rev1 : 0
    const s8 = gm0 >= gm1

    const at0 = rev0 / a0
    const at1 = a1 && a1 > 0 ? rev1 / a1 : at0
    const s9 = at0 >= at1

    const signals = [
      { name: 'Positive Net Income', pass: s1 },
      { name: 'Positive Cash Flow', pass: s2 },
      { name: 'ROA Expansion YoY', pass: s3 },
      { name: 'Cash Exceeds Net Income', pass: s4 },
      { name: 'Stable / Lower Leverage', pass: s5 },
      { name: 'Liquidity (Current Ratio)', pass: s6 },
      { name: 'No Share Dilution', pass: s7 },
      { name: 'Gross Margin Expansion', pass: s8 },
      { name: 'Asset Turnover Velocity', pass: s9 },
    ]
    const score = signals.filter((s) => s.pass).length
    return { score, signals }
  }

  const piotroskiResult = computePiotroski()

  // 5. Altman Z-Score for non-financial companies
  const computeAltmanZ = (): { score: number; zone: 'safe' | 'grey' | 'distress' } | null => {
    const a0 = assetValues[0]?.Value
    if (!a0 || a0 <= 0) return null
    const ca0 = caValues[0]?.Value || 0
    const cl0 = clValues[0]?.Value || 0
    const wc = ca0 - cl0
    const x1 = wc / a0

    const ni0 = netIncomeValues[0]?.Value || 0
    const x2 = ni0 / a0

    const ebit0 = opIncomeValues[0]?.Value || 0
    const x3 = ebit0 / a0

    const eq0 = equityValues[0]?.Value || 0
    const tl0 = Math.max(1, a0 - eq0)
    const x4 = eq0 / tl0

    const rev0 = revValues[0]?.Value || 0
    const x5 = rev0 / a0

    const z = 1.2 * x1 + 1.4 * x2 + 3.3 * x3 + 0.6 * x4 + 1.0 * x5
    const zone = z > 2.99 ? 'safe' : z >= 1.81 ? 'grey' : 'distress'
    return { score: z, zone }
  }

  const altmanZResult = computeAltmanZ()

  const formatValue = (metricName: string, val: SummaryValue | undefined, colIdx?: number): string => {
    if (!val || !val.Present) return '—'

    if (commonSize && colIdx !== undefined) {
      if (metricName.includes('ROIC') || val.Unit === '%') {
        return formatPercent(val.Value)
      }
      if (metricName.includes('Shares')) {
        return formatShares(val.Value)
      }
      const isBS = groups.find((g) => g.title.includes('Balance Sheet'))?.metrics.includes(metricName)
      if (isBS) {
        const assetsVal = metricMap.get('Total Assets')?.Values?.[colIdx]?.Value
        if (assetsVal && assetsVal > 0) {
          return `${((val.Value / assetsVal) * 100).toFixed(1)}%`
        }
      }
      const revVal = metricMap.get('Revenue')?.Values?.[colIdx]?.Value
      if (revVal && revVal > 0) {
        return `${((val.Value / revVal) * 100).toFixed(1)}%`
      }
    }

    if (metricName.includes('ROIC') || val.Unit === '%') {
      return formatPercent(val.Value)
    }
    if (metricName.includes('Shares')) {
      return formatShares(val.Value)
    }
    return formatCurrency(val.Value, compact, val.Unit || 'USD')
  }

  const calculateDelta = (metricName: string): { text: string; positive: boolean | null } => {
    const m = metricMap.get(metricName)
    if (!m || m.Values.length < 2) return { text: '—', positive: null }
    const curr = m.Values[0]
    const prev = m.Values[1]
    if (!curr?.Present || !prev?.Present || prev.Value === 0) {
      return { text: '—', positive: null }
    }
    const delta = (curr.Value - prev.Value) / Math.abs(prev.Value)
    const sign = delta > 0 ? '+' : ''
    const text = `${sign}${(delta * 100).toFixed(1)}%`
    return { text, positive: delta >= 0 }
  }

  const calculateCAGR = (metricName: string, yearsCount: 3 | 5): { text: string; positive: boolean | null } => {
    const m = metricMap.get(metricName)
    if (!m || m.Values.length <= (yearsCount === 3 ? 2 : 3)) return { text: '—', positive: null }
    const curr = m.Values[0]
    const pastIdx = Math.min(m.Values.length - 1, yearsCount)
    const past = m.Values[pastIdx]
    if (!curr?.Present || !past?.Present || past.Value <= 0 || curr.Value <= 0) {
      return { text: '—', positive: null }
    }
    const cagr = Math.pow(curr.Value / past.Value, 1 / pastIdx) - 1
    const sign = cagr > 0 ? '+' : ''
    return { text: `${sign}${(cagr * 100).toFixed(1)}%`, positive: cagr >= 0 }
  }

  const renderSparkline = (values: SummaryValue[]) => {
    const valid = values.filter((v) => v && v.Present)
    if (valid.length < 2) return <span className="text-neutral-300 dark:text-neutral-700">—</span>

    const chrono = [...values].reverse().filter((v) => v && v.Present).map((v) => v.Value)
    const min = Math.min(...chrono)
    const max = Math.max(...chrono)
    const range = max - min || 1

    const width = 36
    const height = 12
    const points = chrono
      .map((v, i) => {
        const x = (i / (chrono.length - 1)) * (width - 4) + 2
        const y = height - 2 - ((v - min) / range) * (height - 4)
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')

    const isUp = chrono[chrono.length - 1] >= chrono[0]

    return (
      <svg width={width} height={height} className="inline-block overflow-visible opacity-75">
        <polyline
          fill="none"
          stroke={isUp ? '#10b981' : '#f43f5e'}
          strokeWidth="1.2"
          strokeLinecap="round"
          strokeLinejoin="round"
          points={points}
        />
      </svg>
    )
  }

  const exportCSV = () => {
    const headers = ['Group', 'Metric', ...periods.map((p) => `FY ${p.Year}`)]
    const rows: string[][] = [headers]

    for (const grp of groups) {
      for (const name of grp.metrics) {
        const m = metricMap.get(name)
        if (!m) continue
        const row = [
          grp.title,
          name,
          ...periods.map((_, idx) => {
            const v = m.Values[idx]
            return v && v.Present ? v.Value.toString() : ''
          }),
        ]
        rows.push(row)
      }
    }

    const csvContent =
      'data:text/csv;charset=utf-8,' +
      rows.map((e) => e.map((x) => `"${x.replace(/"/g, '""')}"`).join(',')).join('\n')

    const encodedUri = encodeURI(csvContent)
    const link = document.createElement('a')
    link.setAttribute('href', encodedUri)
    link.setAttribute('download', `${summary.ticker}_sec_financials.csv`)
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const exportJSON = () => {
    const dataStr =
      'data:text/json;charset=utf-8,' +
      encodeURIComponent(JSON.stringify(summary, null, 2))
    const link = document.createElement('a')
    link.setAttribute('href', dataStr)
    link.setAttribute('download', `${summary.ticker}_sec_financials.json`)
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const exportExecutiveMemo = () => {
    let md = `# Fundamental & Forensic Report: ${summary.company} (${summary.ticker})\n`
    md += `*Standardized SEC EDGAR Financials • Date: ${new Date().toISOString().split('T')[0]}*\n\n`
    md += `## Company Profile\n`
    md += `- Ticker: ${summary.ticker} | CIK: ${summary.cik}\n`
    md += `- SIC: ${summary.sic_description || summary.sic}\n`
    md += `- Periods: ${periods.map((p) => 'FY' + p.Year).join(', ')}\n\n`

    md += `## Forensic Accounting Quality\n`
    if (sloanRatio !== undefined) {
      md += `- Sloan Accrual Ratio: ${(sloanRatio * 100).toFixed(2)}% (${
        sloanRatio <= 0.05 ? 'Cash Backed' : 'Accrual Reliance'
      })\n`
    }
    if (shareChange !== undefined) {
      md += `- 5Y Share Count Change: ${(shareChange * 100).toFixed(2)}%\n`
    }
    if (sbcPctOfRev !== undefined) {
      md += `- SBC as % of Revenue: ${sbcPctOfRev.toFixed(2)}%\n`
    }
    md += `\n`

    md += `## Standardized Financial Statements\n\n`
    for (const grp of groups) {
      const avail = grp.metrics.filter((m) => metricMap.has(m))
      if (avail.length === 0) continue
      md += `### ${grp.title}\n`
      md += `| Metric | ${periods.map((p) => 'FY ' + p.Year).join(' | ')} |\n`
      md += `| :--- | ${periods.map(() => ':---:').join(' | ')} |\n`
      for (const name of avail) {
        const m = metricMap.get(name)!
        md += `| ${name} | ${periods
          .map((_, idx) => formatValue(name, m.Values[idx]))
          .join(' | ')} |\n`
      }
      md += `\n`
    }

    const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${summary.ticker}_financial_report.md`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const filteredAuditFacts = auditFacts.filter((f) => {
    if (selectedAuditMetric) {
      const q = selectedAuditMetric.toLowerCase()
      const matchConcept =
        f.concept.toLowerCase().includes(q) ||
        f.label.toLowerCase().includes(q)
      if (!matchConcept) return false
    }
    if (auditSearch.trim()) {
      const s = auditSearch.toLowerCase()
      return (
        f.concept.toLowerCase().includes(s) ||
        f.label.toLowerCase().includes(s) ||
        f.accession.toLowerCase().includes(s) ||
        f.taxonomy.toLowerCase().includes(s)
      )
    }
    return true
  })

  const numericCIK = parseInt(summary.cik, 10).toString()

  return (
    <div className="space-y-4 animate-in fade-in duration-150 font-sans">
      {/* High-Density Forensic Diagnostic Strip */}
      <div className="flex flex-wrap items-center justify-between gap-y-2 gap-x-4 border border-neutral-200/80 dark:border-white/10 px-4 py-2.5 text-xs font-mono bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-xl">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5">
          {/* Piotroski F-Score */}
          {piotroskiResult && (
            <div
              className="flex items-baseline gap-1.5 cursor-help"
              title={`Piotroski F-Score: ${piotroskiResult.score}/9\n${piotroskiResult.signals
                .map((s) => `${s.pass ? '✓' : '✗'} ${s.name}`)
                .join('\n')}`}
            >
              <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
                Piotroski F
              </span>
              <span
                className={`font-semibold px-1.5 py-0.2 rounded text-[11px] ${
                  piotroskiResult.score >= 7
                    ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20'
                    : piotroskiResult.score >= 5
                    ? 'bg-neutral-100 dark:bg-neutral-800 text-neutral-800 dark:text-neutral-200 border border-neutral-200 dark:border-neutral-700'
                    : 'bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20'
                }`}
              >
                {piotroskiResult.score}/9
              </span>
              <span className="text-[10px] text-neutral-400 font-sans hidden sm:inline">
                {piotroskiResult.score >= 7 ? '(Strong)' : piotroskiResult.score >= 5 ? '(Moderate)' : '(Weak)'}
              </span>
            </div>
          )}

          {piotroskiResult && <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden sm:block" />}

          {/* Altman Z-Score */}
          {altmanZResult && (
            <div
              className="flex items-baseline gap-1.5 cursor-help"
              title={`Altman Z-Score: ${altmanZResult.score.toFixed(2)}\nSafe Zone: Z > 2.99\nGrey Zone: 1.81 <= Z <= 2.99\nDistress Zone: Z < 1.81`}
            >
              <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
                Altman Z
              </span>
              <span
                className={`font-semibold px-1.5 py-0.2 rounded text-[11px] ${
                  altmanZResult.zone === 'safe'
                    ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20'
                    : altmanZResult.zone === 'grey'
                    ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20'
                    : 'bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20'
                }`}
              >
                {altmanZResult.score.toFixed(2)}
              </span>
              <span className="text-[10px] text-neutral-400 font-sans hidden sm:inline">
                {altmanZResult.zone === 'safe' ? '(Safe)' : altmanZResult.zone === 'grey' ? '(Grey)' : '(Distress)'}
              </span>
            </div>
          )}

          {altmanZResult && <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden sm:block" />}

          {/* Sloan Ratio */}
          <div className="flex items-baseline gap-2">
            <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
              Sloan Accrual
            </span>
            <span className="font-semibold text-neutral-900 dark:text-neutral-100">
              {sloanRatio !== undefined ? `${(sloanRatio * 100).toFixed(1)}%` : '—'}
            </span>
            {sloanRatio !== undefined && (
              <span className="text-[11px] text-neutral-500 font-sans hidden md:inline">
                {sloanRatio <= 0.05 ? '(Clean Cash)' : '(High Accrual)'}
              </span>
            )}
          </div>

          <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden sm:block" />

          {/* Share Count Trend */}
          <div className="flex items-baseline gap-2">
            <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
              5Y Shares
            </span>
            <span className="font-semibold text-neutral-900 dark:text-neutral-100">
              {shareChange !== undefined
                ? `${shareChange > 0 ? '+' : ''}${(shareChange * 100).toFixed(1)}%`
                : '—'}
            </span>
            {shareChange !== undefined && (
              <span className="text-[11px] text-neutral-500 font-sans hidden md:inline">
                {shareChange <= -0.05 ? '(Cannibal)' : shareChange > 0.05 ? '(Dilution)' : '(Flat)'}
              </span>
            )}
          </div>

          <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden sm:block" />

          {/* Current Ratio */}
          <div className="flex items-baseline gap-2">
            <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
              Current Ratio
            </span>
            <span className="font-semibold text-neutral-900 dark:text-neutral-100">
              {currentRatio !== undefined ? `${currentRatio.toFixed(2)}x` : '—'}
            </span>
          </div>

          {sbcPctOfRev !== undefined && (
            <>
              <div className="h-3 w-px bg-neutral-200 dark:bg-neutral-800 hidden lg:block" />
              <div className="flex items-baseline gap-2 hidden lg:flex">
                <span className="text-[10px] text-neutral-400 uppercase tracking-wider font-sans">
                  SBC/Rev
                </span>
                <span className="text-neutral-700 dark:text-neutral-300">
                  {sbcPctOfRev.toFixed(1)}%
                </span>
              </div>
            </>
          )}
        </div>

        <button
          onClick={() => openAuditDrawer()}
          className="text-[11px] text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white underline underline-offset-2 transition-colors font-sans"
        >
          Audit XBRL Facts
        </button>
      </div>

      {/* Control Bar */}
      <div className="flex flex-wrap items-center justify-between gap-3 text-xs font-mono py-1">
        <div className="flex items-center gap-2 text-neutral-500">
          <span>{summary.intangibles.Currency || 'USD'}</span>
          <span>•</span>
          <span>{periods.length} Fiscal Periods</span>
          <span>•</span>
          <span>SEC Form 10-K / 20-F</span>
        </div>

        <div className="flex items-center gap-1.5">
          <button
            onClick={() => setShowCharts(!showCharts)}
            className={`flex items-center gap-1 px-2.5 py-1 rounded border text-xs transition-colors ${
              showCharts
                ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold border-transparent'
                : 'border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
            }`}
            title="Toggle Visual Analytics & Trend Charts"
          >
            <BarChart3 className="w-3 h-3" />
            <span>Visual Charts</span>
          </button>

          <button
            onClick={() => setCommonSize(!commonSize)}
            className={`flex items-center gap-1 px-2.5 py-1 rounded border text-xs transition-colors ${
              commonSize
                ? 'bg-indigo-600 text-white dark:bg-indigo-500 dark:text-white font-semibold border-indigo-600'
                : 'border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
            }`}
            title="Express Income/Cash lines as % of Revenue and Balance Sheet lines as % of Assets"
          >
            <Percent className="w-3 h-3" />
            <span>% Common Size</span>
          </button>

          <button
            onClick={() => setHighlightSBC(!highlightSBC)}
            className={`flex items-center gap-1 px-2.5 py-1 rounded border text-xs transition-colors ${
              highlightSBC
                ? 'bg-neutral-100 dark:bg-neutral-900 text-neutral-900 dark:text-white border-neutral-300 dark:border-neutral-700'
                : 'border-neutral-200 dark:border-neutral-800 text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
            }`}
          >
            {highlightSBC ? <Eye className="w-3 h-3" /> : <EyeOff className="w-3 h-3" />}
            <span>SBC Row</span>
          </button>

          <button
            onClick={onToggleCompact}
            className="px-2.5 py-1 rounded border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
          >
            {compact ? 'Exact Numbers' : 'Compact Units'}
          </button>

          <button
            onClick={exportExecutiveMemo}
            className="flex items-center gap-1 px-2.5 py-1 rounded border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
            title="Download formatted Investment Memo"
          >
            <FileText className="w-3 h-3" />
            <span>Memo</span>
          </button>

          <button
            onClick={exportCSV}
            className="flex items-center gap-1 px-2.5 py-1 rounded border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
            title="Download CSV"
          >
            <Download className="w-3 h-3" />
            <span>CSV</span>
          </button>

          <button
            onClick={exportJSON}
            className="flex items-center gap-1 px-2.5 py-1 rounded border border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white text-xs transition-colors"
            title="Download JSON"
          >
            <FileSpreadsheet className="w-3 h-3" />
            <span>JSON</span>
          </button>
        </div>
      </div>

      {/* Visual Analytics Charts Section */}
      {showCharts && <FinancialCharts summary={summary} compact={compact} />}

      {/* Single Continuous Financial Statement Grid */}
      <div className="border border-neutral-200/80 dark:border-white/10 overflow-hidden bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-xl">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead>
              <tr className="border-b border-neutral-200/80 dark:border-white/10 text-neutral-400 bg-neutral-50/60 dark:bg-white/[0.04] font-sans">
                <th className="py-2.5 px-4 font-normal text-neutral-500 w-1/4">Metric Line Item</th>
                {periods.map((p) => (
                  <th key={p.Year} className="py-2.5 px-3 font-normal text-right tabular-nums">
                    <div>FY {p.Year}</div>
                    <div className="text-[10px] text-neutral-400 dark:text-neutral-600 font-mono">
                      {p.End}
                    </div>
                  </th>
                ))}
                <th className="py-2.5 px-3 font-normal text-right tabular-nums text-neutral-400 font-sans w-16">
                  YoY Δ
                </th>
                <th className="py-2.5 px-3 font-normal text-right tabular-nums text-neutral-400 font-sans w-16">
                  3Y CAGR
                </th>
                <th className="py-2.5 px-3 font-normal text-right tabular-nums text-neutral-400 font-sans w-16">
                  5Y CAGR
                </th>
                <th className="py-2.5 px-3 font-normal text-center text-neutral-400 font-sans w-14">
                  Trend
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-100 dark:divide-white/5">
              {groups.map((grp) => {
                const availableMetrics = grp.metrics.filter((m) => metricMap.has(m))
                if (availableMetrics.length === 0) return null

                return (
                  <React.Fragment key={grp.title}>
                    {/* Section Header Divider */}
                    <tr className="bg-neutral-50/60 dark:bg-white/[0.03] border-t border-b border-neutral-200/80 dark:border-white/10">
                      <td
                        colSpan={periods.length + 5}
                        className="py-1.5 px-4 text-[10px] font-mono uppercase tracking-wider text-neutral-500 font-semibold"
                      >
                        {grp.title}
                        <span className="font-normal lowercase text-neutral-400 ml-2 font-sans">
                          — {grp.subtitle}
                        </span>
                      </td>
                    </tr>

                    {/* Section Line Items */}
                    {availableMetrics.map((name) => {
                      const metric = metricMap.get(name)!
                      const isKeyMetric =
                        name === 'Revenue' ||
                        name === 'Operating Income' ||
                        name === 'Net Income' ||
                        name === 'Free Cash Flow' ||
                        name === 'Adjusted Operating Income' ||
                        name === 'Intangible-Adjusted ROIC'
                      const isSBCRow =
                        name === 'Stock-Based Compensation' ||
                        name === 'SBC-Adjusted FCF Proxy'
                      const delta = calculateDelta(name)
                      const cagr3 = calculateCAGR(name, 3)
                      const cagr5 = calculateCAGR(name, 5)

                      return (
                        <tr
                          key={name}
                          className={`hover:bg-neutral-100/60 dark:hover:bg-white/[0.05] transition-colors group ${
                            isSBCRow && highlightSBC
                              ? 'bg-amber-500/5'
                              : ''
                          }`}
                        >
                          <td className="py-2 px-4 text-neutral-700 dark:text-neutral-300 group-hover:text-neutral-950 dark:group-hover:text-white flex items-center justify-between font-sans">
                            <div className="flex items-center gap-1.5">
                              <span
                                className={
                                   isKeyMetric
                                    ? 'font-medium text-neutral-950 dark:text-white'
                                    : ''
                                }
                              >
                                {name}
                              </span>
                              <button
                                onClick={() => openAuditDrawer(name)}
                                className="opacity-0 group-hover:opacity-100 text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-opacity p-0.5"
                                title={`Audit raw XBRL facts for ${name}`}
                              >
                                <FileSearch className="w-3 h-3" />
                              </button>
                            </div>
                            {isSBCRow && (
                              <span className="text-[9px] px-1 py-0.2 rounded border border-amber-500/20 text-amber-700 dark:text-amber-400 font-mono">
                                SBC
                              </span>
                            )}
                          </td>
                          {periods.map((_, idx) => {
                            const val = metric.Values[idx]
                            return (
                              <td
                                key={idx}
                                className={`py-2 px-3 text-right tabular-nums ${
                                  isKeyMetric
                                    ? 'font-medium text-neutral-950 dark:text-white'
                                    : 'text-neutral-600 dark:text-neutral-400'
                                }`}
                              >
                                {formatValue(name, val, idx)}
                              </td>
                            )
                          })}
                          {/* 1Y Delta */}
                          <td className="py-2 px-3 text-right tabular-nums text-[11px]">
                            {delta.text === '—' ? (
                              <span className="text-neutral-300 dark:text-neutral-700">—</span>
                            ) : (
                              <span
                                className={
                                  delta.positive === true
                                    ? 'text-emerald-600 dark:text-emerald-400'
                                    : delta.positive === false
                                    ? 'text-rose-600 dark:text-rose-400'
                                    : 'text-neutral-500'
                                }
                              >
                                {delta.text}
                              </span>
                            )}
                          </td>
                          {/* 3Y CAGR */}
                          <td className="py-2 px-3 text-right tabular-nums text-[11px]">
                            {cagr3.text === '—' ? (
                              <span className="text-neutral-300 dark:text-neutral-700">—</span>
                            ) : (
                              <span
                                className={
                                  cagr3.positive === true
                                    ? 'text-emerald-600 dark:text-emerald-400'
                                    : cagr3.positive === false
                                    ? 'text-rose-600 dark:text-rose-400'
                                    : 'text-neutral-500'
                                }
                              >
                                {cagr3.text}
                              </span>
                            )}
                          </td>
                          {/* 5Y CAGR */}
                          <td className="py-2 px-3 text-right tabular-nums text-[11px]">
                            {cagr5.text === '—' ? (
                              <span className="text-neutral-300 dark:text-neutral-700">—</span>
                            ) : (
                              <span
                                className={
                                  cagr5.positive === true
                                    ? 'text-emerald-600 dark:text-emerald-400'
                                    : cagr5.positive === false
                                    ? 'text-rose-600 dark:text-rose-400'
                                    : 'text-neutral-500'
                                }
                              >
                                {cagr5.text}
                              </span>
                            )}
                          </td>
                          <td className="py-2 px-3 text-center">
                            {renderSparkline(metric.Values)}
                          </td>
                        </tr>
                      )
                    })}
                  </React.Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* Audited XBRL Fact Drawer */}
      {auditOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-xs">
          <div className="bg-white dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 rounded-lg max-w-4xl w-full max-h-[85vh] flex flex-col shadow-2xl overflow-hidden font-mono text-xs">
            {/* Header */}
            <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800 flex items-center justify-between bg-neutral-50 dark:bg-neutral-950">
              <div className="flex items-center gap-2">
                <span className="font-sans font-medium text-neutral-950 dark:text-white">
                  Audited SEC Facts • {summary.ticker}
                </span>
                {selectedAuditMetric && (
                  <span className="px-1.5 py-0.5 rounded border border-neutral-300 dark:border-neutral-700 text-neutral-700 dark:text-neutral-300 text-[10px]">
                    {selectedAuditMetric}
                  </span>
                )}
              </div>
              <button
                onClick={() => setAuditOpen(false)}
                className="p-1 text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Filter Search */}
            <div className="px-5 py-2.5 border-b border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-900 flex items-center justify-between gap-4">
              <div className="relative flex-1">
                <Search className="w-3.5 h-3.5 text-neutral-400 absolute left-2.5 top-2" />
                <input
                  type="text"
                  value={auditSearch}
                  onChange={(e) => setAuditSearch(e.target.value)}
                  placeholder="Filter concept, accession, taxonomy..."
                  className="w-full bg-neutral-50 dark:bg-neutral-950 border border-neutral-200 dark:border-neutral-800 rounded pl-7 pr-3 py-1 text-xs text-neutral-800 dark:text-neutral-200 outline-none focus:border-neutral-400 font-mono"
                />
              </div>

              {selectedAuditMetric && (
                <button
                  onClick={() => setSelectedAuditMetric(null)}
                  className="text-[11px] text-neutral-500 hover:text-neutral-900 dark:hover:text-white font-sans"
                >
                  Clear Filter
                </button>
              )}

              <span className="text-[11px] text-neutral-400 font-mono">
                {filteredAuditFacts.length} facts
              </span>
            </div>

            {/* Table */}
            <div className="flex-1 overflow-y-auto p-3">
              {loadingAudit ? (
                <div className="py-16 text-center text-neutral-400 font-mono">
                  Loading facts from SEC SQLite table...
                </div>
              ) : filteredAuditFacts.length === 0 ? (
                <div className="py-16 text-center text-neutral-400 font-mono">
                  No matching facts found.
                </div>
              ) : (
                <table className="w-full text-left text-xs font-mono">
                  <thead>
                    <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-400 font-sans">
                      <th className="py-1.5 px-3 font-normal">Concept & Taxonomy</th>
                      <th className="py-1.5 px-3 font-normal">Period End</th>
                      <th className="py-1.5 px-3 font-normal text-right">Raw Value</th>
                      <th className="py-1.5 px-3 font-normal">Form</th>
                      <th className="py-1.5 px-3 font-normal">Accession</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                    {filteredAuditFacts.slice(0, 100).map((f, i) => {
                      const accessionClean = f.accession.replace(/-/g, '')
                      const secArchiveURL = `https://www.sec.gov/Archives/edgar/data/${numericCIK}/${accessionClean}/`

                      return (
                        <tr key={i} className="hover:bg-neutral-50 dark:hover:bg-neutral-950">
                          <td className="py-1.5 px-3">
                            <span className="text-neutral-400">{f.taxonomy}:</span>
                            <span className="text-neutral-900 dark:text-white ml-0.5">
                              {f.concept}
                            </span>
                          </td>
                          <td className="py-1.5 px-3 text-neutral-600 dark:text-neutral-400">
                            {f.period_end}
                          </td>
                          <td className="py-1.5 px-3 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                            {f.value !== undefined ? f.value.toLocaleString() : '—'}
                            <span className="text-[10px] text-neutral-400 ml-1">
                              {f.unit}
                            </span>
                          </td>
                          <td className="py-1.5 px-3 text-neutral-500">{f.form}</td>
                          <td className="py-1.5 px-3">
                            <a
                              href={secArchiveURL}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="text-neutral-500 hover:text-neutral-900 dark:hover:text-white inline-flex items-center gap-1"
                            >
                              <span>{f.accession}</span>
                              <ExternalLink className="w-2.5 h-2.5 text-neutral-400" />
                            </a>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
