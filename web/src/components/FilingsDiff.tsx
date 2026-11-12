import React, { useState, useEffect } from 'react'
import {
  FileText,
  Search,
  Check,
  Copy,
  ArrowRight,
  RefreshCw,
  SlidersHorizontal,
  PlusCircle,
  MinusCircle,
  Edit3,
  CheckCircle2,
  AlertTriangle,
  ExternalLink,
  ShieldAlert,
} from 'lucide-react'
import { FilingDiffResult, DiffParagraph, FilingVersion } from '../types'
import { formatDate } from '../lib/format'

interface FilingsDiffProps {
  ticker: string
}

export const FilingsDiff: React.FC<FilingsDiffProps> = ({ ticker }) => {
  const [itemType, setItemType] = useState<'1A' | '7'>('1A')
  const [viewMode, setViewMode] = useState<'unified' | 'sideBySide' | 'changesOnly'>('unified')
  const [diffData, setDiffData] = useState<FilingDiffResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [selectedCurrentAcc, setSelectedCurrentAcc] = useState<string>('')
  const [selectedPriorAcc, setSelectedPriorAcc] = useState<string>('')
  const [copiedReport, setCopiedReport] = useState(false)

  // Fetch diff from backend
  const fetchDiff = async (item: '1A' | '7', currAcc?: string, prAcc?: string) => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      params.set('item', item)
      if (currAcc) params.set('current', currAcc)
      if (prAcc) params.set('prior', prAcc)

      const res = await fetch(`/api/company/${ticker}/filings/diff?${params.toString()}`)
      if (!res.ok) {
        const errJson = await res.json().catch(() => null)
        throw new Error(errJson?.error || `HTTP error ${res.status}`)
      }
      const data: FilingDiffResult = await res.json()
      setDiffData(data)
      if (!selectedCurrentAcc && data.current_filing) {
        setSelectedCurrentAcc(data.current_filing.accession)
      }
      if (!selectedPriorAcc && data.prior_filing) {
        setSelectedPriorAcc(data.prior_filing.accession)
      }
    } catch (err: any) {
      setError(err.message || 'Failed to load filing diff')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchDiff(itemType, selectedCurrentAcc, selectedPriorAcc)
  }, [ticker, itemType])

  const handleComparisonChange = (currAcc: string, prAcc: string) => {
    setSelectedCurrentAcc(currAcc)
    setSelectedPriorAcc(prAcc)
    fetchDiff(itemType, currAcc, prAcc)
  }

  // Filter diff items by search text
  const filteredItems = (diffData?.diff_items || []).filter((item) => {
    if (viewMode === 'changesOnly' && item.type === 'equal') {
      return false
    }
    if (!search.trim()) return true
    const q = search.toLowerCase()
    return (
      item.text.toLowerCase().includes(q) ||
      (item.prior_text && item.prior_text.toLowerCase().includes(q))
    )
  })

  // 1-Click Copy Diff Summary Report
  const copySummaryReport = () => {
    if (!diffData) return
    const stats = diffData.stats
    const report = [
      `=== ${ticker} 10-K Redline Report: ${diffData.item_title} ===`,
      `Current: Form 10-K (${diffData.current_filing.filing_date}, Acc: ${diffData.current_filing.accession})`,
      `Prior: Form 10-K (${diffData.prior_filing.filing_date}, Acc: ${diffData.prior_filing.accession})`,
      `------------------------------------------------------------`,
      `Added Paragraphs    : +${stats.added_paragraphs}`,
      `Deleted Paragraphs  : -${stats.deleted_paragraphs}`,
      `Modified Paragraphs : ~${stats.modified_paragraphs}`,
      `Unchanged           : =${stats.unchanged_paragraphs}`,
      `Text Similarity     : ${stats.similarity_pct}%`,
      `------------------------------------------------------------`,
      `Key Added Disclosures:`,
      ...diffData.diff_items
        .filter((d) => d.type === 'added')
        .slice(0, 5)
        .map((d, i) => `[+ New #${i + 1}]: ${d.text.slice(0, 200)}...`),
      `------------------------------------------------------------`,
      `Key Deleted Disclosures:`,
      ...diffData.diff_items
        .filter((d) => d.type === 'deleted')
        .slice(0, 5)
        .map((d, i) => `[- Removed #${i + 1}]: ${d.text.slice(0, 200)}...`),
    ].join('\n')

    navigator.clipboard.writeText(report)
    setCopiedReport(true)
    setTimeout(() => setCopiedReport(false), 2000)
  }

  const availableFilings = diffData?.available_10k_filings || []

  return (
    <div className="space-y-5 animate-in fade-in duration-150">
      {/* Top Controls Toolbar */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 p-4 border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-lg">
        {/* Section Segment (Item 1A vs Item 7) */}
        <div className="flex items-center gap-3">
          <div className="inline-flex border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 font-mono text-xs rounded overflow-hidden">
            <button
              onClick={() => {
                setItemType('1A')
                fetchDiff('1A', selectedCurrentAcc, selectedPriorAcc)
              }}
              className={`px-3 py-1.5 transition-colors flex items-center gap-1.5 ${
                itemType === '1A'
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
              }`}
            >
              <ShieldAlert className="w-3.5 h-3.5 text-amber-500" />
              <span>Item 1A: Risk Factors</span>
            </button>
            <button
              onClick={() => {
                setItemType('7')
                fetchDiff('7', selectedCurrentAcc, selectedPriorAcc)
              }}
              className={`px-3 py-1.5 transition-colors flex items-center gap-1.5 ${
                itemType === '7'
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900 dark:hover:text-white'
              }`}
            >
              <FileText className="w-3.5 h-3.5 text-blue-500" />
              <span>Item 7: MD&amp;A</span>
            </button>
          </div>

          {/* Filing Year Selectors */}
          {availableFilings.length >= 2 && diffData && (
            <div className="hidden sm:flex items-center gap-2 text-xs font-mono">
              <span className="text-neutral-400">Comparing</span>
              <select
                value={selectedPriorAcc || diffData.prior_filing.accession}
                onChange={(e) => handleComparisonChange(selectedCurrentAcc || diffData.current_filing.accession, e.target.value)}
                className="bg-white dark:bg-neutral-900 border border-neutral-300 dark:border-neutral-700 rounded px-2 py-1 text-xs text-neutral-800 dark:text-neutral-200 outline-none"
              >
                {availableFilings.map((f) => (
                  <option key={f.accession} value={f.accession}>
                    Prior: FY {f.year || f.filing_date.slice(0, 4)} ({f.filing_date})
                  </option>
                ))}
              </select>

              <ArrowRight className="w-3.5 h-3.5 text-neutral-400" />

              <select
                value={selectedCurrentAcc || diffData.current_filing.accession}
                onChange={(e) => handleComparisonChange(e.target.value, selectedPriorAcc || diffData.prior_filing.accession)}
                className="bg-white dark:bg-neutral-900 border border-neutral-300 dark:border-neutral-700 rounded px-2 py-1 text-xs text-neutral-800 dark:text-neutral-200 outline-none font-medium"
              >
                {availableFilings.map((f) => (
                  <option key={f.accession} value={f.accession}>
                    Current: FY {f.year || f.filing_date.slice(0, 4)} ({f.filing_date})
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>

        {/* View Mode & Search */}
        <div className="flex flex-wrap items-center gap-3">
          {/* View Mode Toggle */}
          <div className="inline-flex border border-neutral-300 dark:border-neutral-700 divide-x divide-neutral-300 dark:divide-neutral-700 font-mono text-xs rounded overflow-hidden">
            <button
              onClick={() => setViewMode('unified')}
              className={`px-2.5 py-1 transition-colors ${
                viewMode === 'unified'
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900'
              }`}
            >
              Unified Redline
            </button>
            <button
              onClick={() => setViewMode('changesOnly')}
              className={`px-2.5 py-1 transition-colors ${
                viewMode === 'changesOnly'
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900'
              }`}
            >
              Changes Only
            </button>
            <button
              onClick={() => setViewMode('sideBySide')}
              className={`px-2.5 py-1 transition-colors ${
                viewMode === 'sideBySide'
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-900'
              }`}
            >
              Side-by-Side
            </button>
          </div>

          {/* Search Input */}
          <div className="relative min-w-[200px]">
            <Search className="w-3.5 h-3.5 text-neutral-400 absolute left-2.5 top-2" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Filter risks (AI, China, margins)..."
              className="w-full bg-white dark:bg-neutral-950 border border-neutral-300 dark:border-neutral-800 pl-8 pr-3 py-1 text-xs text-neutral-800 dark:text-neutral-200 placeholder:text-neutral-400 outline-none focus:border-neutral-500 font-mono rounded"
            />
          </div>

          {/* 1-Click Copy Report */}
          <button
            onClick={copySummaryReport}
            className="inline-flex items-center gap-1.5 px-3 py-1 rounded border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800 text-xs font-sans text-neutral-800 dark:text-neutral-200 transition-colors"
            title="Copy Executive Summary Diff Report"
          >
            {copiedReport ? (
              <>
                <Check className="w-3.5 h-3.5 text-emerald-500" />
                <span>Copied Report!</span>
              </>
            ) : (
              <>
                <Copy className="w-3.5 h-3.5" />
                <span>Copy Summary</span>
              </>
            )}
          </button>
        </div>
      </div>

      {loading && (
        <div className="py-20 text-center space-y-3 font-mono text-xs text-neutral-400">
          <RefreshCw className="w-6 h-6 animate-spin mx-auto text-amber-500" />
          <p>Analyzing and diffing 10-K {itemType === '1A' ? 'Risk Factors' : 'MD&A'} disclosures for {ticker}...</p>
        </div>
      )}

      {error && (
        <div className="p-4 border border-rose-300 dark:border-rose-900/40 bg-rose-50 dark:bg-rose-950/20 text-xs text-rose-700 dark:text-rose-300 font-mono rounded-lg">
          {error}
        </div>
      )}

      {!loading && !error && diffData && (
        <div className="space-y-4">
          {/* Executive Stats KPI Banner */}
          <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
            <div className="p-3 rounded-lg border border-emerald-500/20 bg-emerald-500/5 text-center">
              <span className="text-[10px] font-mono uppercase tracking-wider text-emerald-700 dark:text-emerald-400 block">
                Added Paragraphs
              </span>
              <span className="text-xl font-bold font-mono text-emerald-600 dark:text-emerald-400">
                +{diffData.stats.added_paragraphs}
              </span>
            </div>

            <div className="p-3 rounded-lg border border-rose-500/20 bg-rose-500/5 text-center">
              <span className="text-[10px] font-mono uppercase tracking-wider text-rose-700 dark:text-rose-400 block">
                Deleted Paragraphs
              </span>
              <span className="text-xl font-bold font-mono text-rose-600 dark:text-rose-400">
                -{diffData.stats.deleted_paragraphs}
              </span>
            </div>

            <div className="p-3 rounded-lg border border-amber-500/20 bg-amber-500/5 text-center">
              <span className="text-[10px] font-mono uppercase tracking-wider text-amber-700 dark:text-amber-400 block">
                Modified Paragraphs
              </span>
              <span className="text-xl font-bold font-mono text-amber-600 dark:text-amber-400">
                ~{diffData.stats.modified_paragraphs}
              </span>
            </div>

            <div className="p-3 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/40 text-center">
              <span className="text-[10px] font-mono uppercase tracking-wider text-neutral-500 block">
                Unchanged
              </span>
              <span className="text-xl font-bold font-mono text-neutral-700 dark:text-neutral-300">
                ={diffData.stats.unchanged_paragraphs}
              </span>
            </div>

            <div className="p-3 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/40 text-center">
              <span className="text-[10px] font-mono uppercase tracking-wider text-neutral-500 block">
                Similarity Score
              </span>
              <span className="text-xl font-bold font-mono text-neutral-900 dark:text-white">
                {diffData.stats.similarity_pct}%
              </span>
            </div>
          </div>

          {/* Diff Content View */}
          <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md rounded-lg overflow-hidden">
            <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/40 flex flex-wrap items-center justify-between gap-3 text-xs font-mono">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-neutral-950 dark:text-white font-sans text-sm">
                  {diffData.item_title}
                </span>
                <span className="text-neutral-400">•</span>
                <span className="text-neutral-500">
                  Showing {filteredItems.length} paragraphs
                </span>
              </div>
              <div className="flex items-center gap-4 text-neutral-500 text-[11px]">
                <span className="flex items-center gap-1">
                  <span className="w-2.5 h-2.5 rounded-full bg-emerald-500 inline-block" />
                  <span>Added</span>
                </span>
                <span className="flex items-center gap-1">
                  <span className="w-2.5 h-2.5 rounded-full bg-rose-500 inline-block" />
                  <span>Deleted</span>
                </span>
                <span className="flex items-center gap-1">
                  <span className="w-2.5 h-2.5 rounded-full bg-amber-500 inline-block" />
                  <span>Modified Words</span>
                </span>
              </div>
            </div>

            <div className="p-5 divide-y divide-neutral-200/60 dark:divide-neutral-800/60 max-h-[75vh] overflow-y-auto space-y-4 font-sans text-sm leading-relaxed">
              {filteredItems.length === 0 ? (
                <div className="py-12 text-center text-neutral-400 font-mono text-xs">
                  No disclosures match your search or filter criteria.
                </div>
              ) : (
                filteredItems.map((item, idx) => {
                  if (viewMode === 'sideBySide') {
                    return (
                      <div key={idx} className="pt-4 grid grid-cols-1 md:grid-cols-2 gap-4 text-xs font-mono">
                        {/* Prior */}
                        <div
                          className={`p-3 rounded border ${
                            item.type === 'deleted'
                              ? 'bg-rose-500/10 border-rose-500/30 text-rose-900 dark:text-rose-300'
                              : item.type === 'modified'
                              ? 'bg-neutral-50 dark:bg-neutral-900/60 border-neutral-200 dark:border-neutral-800 text-neutral-600 dark:text-neutral-400'
                              : 'bg-white dark:bg-neutral-950 border-neutral-200 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300'
                          }`}
                        >
                          <span className="text-[10px] uppercase tracking-wider text-neutral-400 block mb-1">
                            Prior 10-K ({diffData.prior_filing.year || 'Prior'})
                          </span>
                          <p className="font-sans text-sm leading-relaxed">
                            {item.prior_text || (item.type === 'added' ? '— (Not present in prior filing)' : item.text)}
                          </p>
                        </div>

                        {/* Current */}
                        <div
                          className={`p-3 rounded border ${
                            item.type === 'added'
                              ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-900 dark:text-emerald-200'
                              : item.type === 'modified'
                              ? 'bg-amber-500/5 border-amber-500/20 text-neutral-900 dark:text-neutral-100'
                              : 'bg-white dark:bg-neutral-950 border-neutral-200 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300'
                          }`}
                        >
                          <span className="text-[10px] uppercase tracking-wider text-neutral-400 block mb-1">
                            Current 10-K ({diffData.current_filing.year || 'Current'})
                          </span>
                          {item.type === 'modified' && item.words ? (
                            <p className="font-sans text-sm leading-relaxed">
                              {item.words.map((w, wIdx) => {
                                if (w.type === 'insert') {
                                  return (
                                    <span
                                      key={wIdx}
                                      className="bg-emerald-500/25 text-emerald-900 dark:text-emerald-200 font-semibold px-0.5 rounded mr-1"
                                    >
                                      {w.text}
                                    </span>
                                  )
                                }
                                if (w.type === 'delete') {
                                  return (
                                    <span
                                      key={wIdx}
                                      className="bg-rose-500/20 text-rose-800 dark:text-rose-400 line-through px-0.5 rounded mr-1 opacity-75"
                                    >
                                      {w.text}
                                    </span>
                                  )
                                }
                                return <span key={wIdx} className="mr-1">{w.text}</span>
                              })}
                            </p>
                          ) : (
                            <p className="font-sans text-sm leading-relaxed">
                              {item.type === 'deleted' ? '— (Deleted from current filing)' : item.text}
                            </p>
                          )}
                        </div>
                      </div>
                    )
                  }

                  // Unified Redline Mode
                  if (item.type === 'added') {
                    return (
                      <div
                        key={idx}
                        className="pt-4 p-4 rounded-lg bg-emerald-500/10 dark:bg-emerald-950/20 border-l-4 border-emerald-500 text-emerald-950 dark:text-emerald-100 space-y-1.5"
                      >
                        <div className="flex items-center gap-1.5 text-emerald-700 dark:text-emerald-400 text-xs font-mono font-semibold">
                          <PlusCircle className="w-3.5 h-3.5" />
                          <span>NEW DISCLOSURE ADDED IN FY {diffData.current_filing.year}</span>
                        </div>
                        <p className="leading-relaxed">{item.text}</p>
                      </div>
                    )
                  }

                  if (item.type === 'deleted') {
                    return (
                      <div
                        key={idx}
                        className="pt-4 p-4 rounded-lg bg-rose-500/10 dark:bg-rose-950/20 border-l-4 border-rose-500 text-rose-950 dark:text-rose-200 space-y-1.5"
                      >
                        <div className="flex items-center gap-1.5 text-rose-700 dark:text-rose-400 text-xs font-mono font-semibold">
                          <MinusCircle className="w-3.5 h-3.5" />
                          <span>DELETED / REMOVED FROM PRIOR 10-K</span>
                        </div>
                        <p className="leading-relaxed line-through opacity-80">{item.text}</p>
                      </div>
                    )
                  }

                  if (item.type === 'modified') {
                    return (
                      <div
                        key={idx}
                        className="pt-4 p-4 rounded-lg bg-amber-500/5 dark:bg-amber-950/10 border-l-4 border-amber-500 text-neutral-900 dark:text-neutral-100 space-y-1.5"
                      >
                        <div className="flex items-center gap-1.5 text-amber-700 dark:text-amber-400 text-xs font-mono font-semibold">
                          <Edit3 className="w-3.5 h-3.5" />
                          <span>REVISED DISCLOSURE PARAGRAPH</span>
                        </div>
                        <p className="leading-relaxed font-sans">
                          {item.words ? (
                            item.words.map((w, wIdx) => {
                              if (w.type === 'insert') {
                                return (
                                  <span
                                    key={wIdx}
                                    className="bg-emerald-500/30 text-emerald-900 dark:text-emerald-200 font-semibold px-0.5 rounded mr-1"
                                  >
                                    {w.text}
                                  </span>
                                )
                              }
                              if (w.type === 'delete') {
                                return (
                                  <span
                                    key={wIdx}
                                    className="bg-rose-500/25 text-rose-800 dark:text-rose-300 line-through px-0.5 rounded mr-1"
                                  >
                                    {w.text}
                                  </span>
                                )
                              }
                              return <span key={wIdx} className="mr-1">{w.text}</span>
                            })
                          ) : (
                            item.text
                          )}
                        </p>
                      </div>
                    )
                  }

                  // Equal / Unchanged
                  return (
                    <div key={idx} className="pt-4 text-neutral-700 dark:text-neutral-300">
                      <p className="leading-relaxed">{item.text}</p>
                    </div>
                  )
                })
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
