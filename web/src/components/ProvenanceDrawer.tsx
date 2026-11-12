import React, { useState, useEffect } from 'react'
import {
  X,
  Copy,
  Check,
  ExternalLink,
  Maximize2,
  Minimize2,
  Table,
  FileText,
  FileSearch,
  Sparkles,
  Info,
  ChevronRight,
  RefreshCw,
} from 'lucide-react'
import { SummaryPeriod, SummaryValue, SummaryMetric } from '../types'
import { formatCurrency, formatNumber } from '../lib/format'

interface ProvenanceDrawerProps {
  isOpen: boolean
  onClose: () => void
  ticker: string
  metricName: string
  period: SummaryPeriod
  value: SummaryValue
  allMetrics?: SummaryMetric[]
}

export const ProvenanceDrawer: React.FC<ProvenanceDrawerProps> = ({
  isOpen,
  onClose,
  ticker,
  metricName,
  period,
  value,
  allMetrics = [],
}) => {
  const [copiedFormula, setCopiedFormula] = useState(false)
  const [copiedConcept, setCopiedConcept] = useState(false)
  const [copiedAccession, setCopiedAccession] = useState(false)
  const [isFullScreen, setIsFullScreen] = useState(false)
  const [activeConcept, setActiveConcept] = useState<string>(value.Concept || '')
  const [activeAccession, setActiveAccession] = useState<string>(value.Accession || '')
  const [iframeKey, setIframeKey] = useState(0)

  useEffect(() => {
    setActiveConcept(value.Concept || '')
    setActiveAccession(value.Accession || '')
    setIframeKey((k) => k + 1)
  }, [value, metricName, period])

  if (!isOpen) return null

  const periodLabel = period.Label || (period.Quarter ? `${period.Quarter} ${period.Year}` : `FY ${period.Year}`)
  const yearParam = period.Year || period.End.slice(0, 4)
  const excelFormula = value.Formula || `=SEC("${ticker}", "${metricName}", ${yearParam})`

  const handleCopyFormula = () => {
    navigator.clipboard.writeText(excelFormula)
    setCopiedFormula(true)
    setTimeout(() => setCopiedFormula(false), 2000)
  }

  const handleCopyConcept = () => {
    if (activeConcept) {
      const tag = value.Taxonomy ? `${value.Taxonomy}:${activeConcept}` : activeConcept
      navigator.clipboard.writeText(tag)
      setCopiedConcept(true)
      setTimeout(() => setCopiedConcept(false), 2000)
    }
  }

  const handleCopyAccession = () => {
    if (activeAccession) {
      navigator.clipboard.writeText(activeAccession)
      setCopiedAccession(true)
      setTimeout(() => setCopiedAccession(false), 2000)
    }
  }

  // Format value display
  const formattedVal =
    value.Unit === '%'
      ? `${(value.Value * 100).toFixed(1)}%`
      : value.Unit === 'shares'
      ? `${formatNumber(value.Value)} shares`
      : formatCurrency(value.Value, false, value.Unit || 'USD')

  // SEC iframe source
  const iframeSrc = `/api/company/${ticker}/filing-html?accession=${encodeURIComponent(
    activeAccession
  )}&concept=${encodeURIComponent(activeConcept || metricName)}&period=${encodeURIComponent(period.End)}`

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-end bg-black/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div
        className={`bg-white dark:bg-neutral-950 border-l border-neutral-200 dark:border-neutral-800 h-full flex flex-col shadow-2xl transition-all duration-300 ${
          isFullScreen ? 'w-full' : 'w-full lg:w-[85vw] xl:w-[75vw]'
        }`}
      >
        {/* Top Header Bar */}
        <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/70 dark:bg-neutral-900/50 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1.5">
              <span className="px-2 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 text-[11px] font-mono font-medium">
                SEC Provenance
              </span>
              <span className="text-neutral-400">•</span>
              <h3 className="font-sans font-semibold text-sm text-neutral-950 dark:text-white">
                {metricName}
              </h3>
              <span className="text-xs text-neutral-500 font-mono">
                ({periodLabel})
              </span>
            </div>
            {value.Form && (
              <span className="px-1.5 py-0.5 rounded bg-neutral-200 dark:bg-neutral-800 text-neutral-800 dark:text-neutral-200 text-[10px] font-mono font-semibold">
                Form {value.Form}
              </span>
            )}
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={() => setIsFullScreen(!isFullScreen)}
              className="p-1.5 text-neutral-500 hover:text-neutral-900 dark:hover:text-white transition-colors rounded hover:bg-neutral-100 dark:hover:bg-neutral-800"
              title={isFullScreen ? 'Exit Full Screen' : 'Expand Full Screen'}
            >
              {isFullScreen ? (
                <Minimize2 className="w-4 h-4" />
              ) : (
                <Maximize2 className="w-4 h-4" />
              )}
            </button>
            <button
              onClick={onClose}
              className="p-1.5 text-neutral-500 hover:text-neutral-900 dark:hover:text-white transition-colors rounded hover:bg-neutral-100 dark:hover:bg-neutral-800"
              title="Close Drawer"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Split Screen Body */}
        <div className="flex-1 flex flex-col md:flex-row overflow-hidden">
          {/* Left Pane: Metric Provenance & 1-Click Formula */}
          <div className="w-full md:w-[360px] lg:w-[400px] border-r border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-950 flex flex-col overflow-y-auto p-5 space-y-5">
            {/* Value Display Card */}
            <div className="p-4 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/30">
              <span className="text-[10px] font-mono uppercase tracking-wider text-neutral-500 block">
                Reported Cell Value
              </span>
              <div className="text-2xl font-bold font-mono text-neutral-950 dark:text-white mt-1">
                {formattedVal}
              </div>
              <div className="text-xs text-neutral-500 font-sans mt-0.5">
                Exact audited value extracted from SEC EDGAR
              </div>
            </div>

            {/* 1-Click Copy Excel/Sheets Formula Button */}
            <div className="space-y-2">
              <span className="text-[11px] font-semibold text-neutral-900 dark:text-neutral-200 font-sans flex items-center gap-1.5">
                <Sparkles className="w-3.5 h-3.5 text-amber-500" />
                <span>1-Click Excel / Google Sheets Formula</span>
              </span>
              <div className="relative group">
                <div className="p-3 bg-neutral-900 text-emerald-400 dark:bg-black dark:text-emerald-400 font-mono text-xs rounded border border-neutral-800 select-all flex items-center justify-between">
                  <span className="truncate pr-2">{excelFormula}</span>
                  <button
                    onClick={handleCopyFormula}
                    className="p-1 text-neutral-400 hover:text-white transition-colors shrink-0"
                    title="Copy Formula to Clipboard"
                  >
                    {copiedFormula ? (
                      <Check className="w-3.5 h-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="w-3.5 h-3.5" />
                    )}
                  </button>
                </div>
              </div>
              <button
                onClick={handleCopyFormula}
                className={`w-full py-2 px-3 rounded text-xs font-medium font-sans flex items-center justify-center gap-1.5 transition-colors ${
                  copiedFormula
                    ? 'bg-emerald-600 text-white'
                    : 'bg-neutral-900 hover:bg-neutral-800 text-white dark:bg-white dark:text-black dark:hover:bg-neutral-200'
                }`}
              >
                {copiedFormula ? (
                  <>
                    <Check className="w-3.5 h-3.5" />
                    <span>Copied Formula to Clipboard!</span>
                  </>
                ) : (
                  <>
                    <Copy className="w-3.5 h-3.5" />
                    <span>Copy Spreadsheet Formula</span>
                  </>
                )}
              </button>
              <p className="text-[11px] text-neutral-500 font-sans leading-relaxed">
                Paste into your financial model in Excel or Google Sheets. Dynamically links to live SEC data.
              </p>
            </div>

            {/* Provenance Traceability Breakdown */}
            <div className="border-t border-neutral-200 dark:border-neutral-800 pt-4 space-y-3">
              <span className="text-[11px] font-semibold text-neutral-900 dark:text-neutral-200 font-sans block">
                Audited XBRL Lineage
              </span>

              <div className="space-y-2 text-xs font-mono">
                {/* XBRL Tag */}
                <div className="p-2.5 rounded bg-neutral-50 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 space-y-1">
                  <div className="flex items-center justify-between text-neutral-500 text-[10px] font-sans">
                    <span>Taxonomy &amp; Concept</span>
                    <button
                      onClick={handleCopyConcept}
                      className="hover:text-neutral-900 dark:hover:text-white"
                      title="Copy Tag"
                    >
                      {copiedConcept ? (
                        <Check className="w-3 h-3 text-emerald-500" />
                      ) : (
                        <Copy className="w-3 h-3" />
                      )}
                    </button>
                  </div>
                  <div className="font-semibold text-neutral-900 dark:text-white break-all text-[11px]">
                    {value.Taxonomy ? `${value.Taxonomy}:` : ''}
                    {activeConcept || 'Derived Calculation'}
                  </div>
                </div>

                {/* Filing & Accession */}
                <div className="p-2.5 rounded bg-neutral-50 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 space-y-1">
                  <div className="flex items-center justify-between text-neutral-500 text-[10px] font-sans">
                    <span>SEC Accession Number</span>
                    <button
                      onClick={handleCopyAccession}
                      className="hover:text-neutral-900 dark:hover:text-white"
                      title="Copy Accession"
                    >
                      {copiedAccession ? (
                        <Check className="w-3 h-3 text-emerald-500" />
                      ) : (
                        <Copy className="w-3 h-3" />
                      )}
                    </button>
                  </div>
                  <div className="font-semibold text-neutral-900 dark:text-white text-[11px]">
                    {activeAccession || 'Latest Filing'}
                  </div>
                  {value.FilingDate && (
                    <div className="text-[10px] text-neutral-500 font-sans">
                      Filed with SEC on {value.FilingDate}
                    </div>
                  )}
                </div>

                {/* Calculation breakdown if derived */}
                {value.Calculation && (
                  <div className="p-2.5 rounded bg-amber-500/10 border border-amber-500/20 text-neutral-900 dark:text-neutral-100 space-y-1">
                    <span className="text-[10px] uppercase tracking-wider text-amber-700 dark:text-amber-400 font-sans font-semibold block">
                      Formula Derivation
                    </span>
                    <div className="text-[11px] font-mono leading-relaxed">
                      {value.Calculation}
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* Jump to Statements in Document */}
            <div className="border-t border-neutral-200 dark:border-neutral-800 pt-4 space-y-2">
              <span className="text-[11px] font-semibold text-neutral-900 dark:text-neutral-200 font-sans block">
                Filing Section Anchors
              </span>
              <div className="grid grid-cols-1 gap-1.5 text-xs font-sans">
                <button
                  onClick={() => {
                    setActiveConcept('Revenues')
                    setIframeKey((k) => k + 1)
                  }}
                  className="py-1.5 px-2.5 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-left text-neutral-700 dark:text-neutral-300 transition-colors flex items-center justify-between text-[11px]"
                >
                  <span>Operations / Income Statement</span>
                  <ChevronRight className="w-3 h-3 text-neutral-400" />
                </button>
                <button
                  onClick={() => {
                    setActiveConcept('Assets')
                    setIframeKey((k) => k + 1)
                  }}
                  className="py-1.5 px-2.5 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-left text-neutral-700 dark:text-neutral-300 transition-colors flex items-center justify-between text-[11px]"
                >
                  <span>Consolidated Balance Sheets</span>
                  <ChevronRight className="w-3 h-3 text-neutral-400" />
                </button>
                <button
                  onClick={() => {
                    setActiveConcept('NetCashProvidedByUsedInOperatingActivities')
                    setIframeKey((k) => k + 1)
                  }}
                  className="py-1.5 px-2.5 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-900 text-left text-neutral-700 dark:text-neutral-300 transition-colors flex items-center justify-between text-[11px]"
                >
                  <span>Cash Flow Statements</span>
                  <ChevronRight className="w-3 h-3 text-neutral-400" />
                </button>
              </div>
            </div>

            {/* External Direct Link */}
            <div className="pt-2">
              <a
                href={iframeSrc}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 text-xs text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white transition-colors underline underline-offset-2 font-sans"
              >
                <span>Open Raw Filing in Separate Window</span>
                <ExternalLink className="w-3 h-3" />
              </a>
            </div>
          </div>

          {/* Right Pane: Embedded SEC Filing HTML Viewer */}
          <div className="flex-1 flex flex-col bg-white dark:bg-neutral-900 overflow-hidden relative">
            {/* Viewer Toolbar */}
            <div className="px-4 py-2 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-950 flex items-center justify-between text-xs font-mono">
              <div className="flex items-center gap-2">
                <Table className="w-3.5 h-3.5 text-amber-500" />
                <span className="font-sans font-medium text-neutral-900 dark:text-neutral-100">
                  Official SEC EDGAR Filing
                </span>
                <span className="text-[11px] text-neutral-400 hidden sm:inline">
                  • Anchored &amp; Highlighted Line Item
                </span>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setIframeKey((k) => k + 1)}
                  className="p-1 text-neutral-500 hover:text-neutral-900 dark:hover:text-white transition-colors"
                  title="Reload Filing"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                </button>
                <a
                  href={iframeSrc}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded border border-neutral-200 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-800 text-[11px] font-sans text-neutral-700 dark:text-neutral-300"
                >
                  <span>Full Screen Document</span>
                  <ExternalLink className="w-3 h-3" />
                </a>
              </div>
            </div>

            {/* Iframe Viewport */}
            <div className="flex-1 w-full h-full relative bg-white">
              <iframe
                key={iframeKey}
                src={iframeSrc}
                title={`SEC Filing Document for ${ticker}`}
                className="w-full h-full border-0"
                sandbox="allow-same-origin allow-scripts allow-popups"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
