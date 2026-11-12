import React, { useState, useEffect } from 'react'
import { ExternalLink, Search, FileText, Copy, Check, X, FileSearch, GitCompare } from 'lucide-react'
import { FilingItem, RawFactItem, AuditResponse } from '../types'
import { formatDate } from '../lib/format'
import { FilingsDiff } from './FilingsDiff'

interface FilingsBrowserProps {
  ticker: string
}

export const FilingsBrowser: React.FC<FilingsBrowserProps> = ({ ticker }) => {
  const [filings, setFilings] = useState<FilingItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [formFilter, setFormFilter] = useState<string>('ALL')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const pageSize = 25

  // Quick Reader & Inspector State
  const [selectedFiling, setSelectedFiling] = useState<FilingItem | null>(null)
  const [inspectorTab, setInspectorTab] = useState<'facts' | 'reader'>('facts')
  const [auditFacts, setAuditFacts] = useState<RawFactItem[]>([])
  const [loadingAudit, setLoadingAudit] = useState(false)
  const [copiedAccession, setCopiedAccession] = useState(false)
  const [mainView, setMainView] = useState<'filings' | 'diff'>('filings')

  // Fetch filing list
  useEffect(() => {
    let ignore = false
    const fetchFilings = async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetch(`/api/company/${ticker}/filings`)
        if (!res.ok) throw new Error(`HTTP error ${res.status}`)
        const data: FilingItem[] = await res.json()
        if (!ignore) {
          setFilings(data)
          setPage(1)
        }
      } catch (err: any) {
        if (!ignore) setError(err.message || 'Failed to load filings')
      } finally {
        if (!ignore) setLoading(false)
      }
    }

    fetchFilings()
    return () => {
      ignore = true
    }
  }, [ticker])

  // Fetch raw facts for inspection when filing is selected
  useEffect(() => {
    if (selectedFiling && auditFacts.length === 0 && !loadingAudit) {
      let ignore = false
      setLoadingAudit(true)
      fetch(`/api/company/${ticker}/audit`)
        .then((res) => (res.ok ? res.json() : null))
        .then((data: AuditResponse | null) => {
          if (!ignore && data?.raw_facts) {
            setAuditFacts(data.raw_facts)
          }
        })
        .catch(() => {})
        .finally(() => {
          if (!ignore) setLoadingAudit(false)
        })
      return () => {
        ignore = true
      }
    }
  }, [selectedFiling, auditFacts.length, loadingAudit, ticker])

  const copyAccession = (acc: string) => {
    navigator.clipboard.writeText(acc)
    setCopiedAccession(true)
    setTimeout(() => setCopiedAccession(false), 2000)
  }

  const filtered = filings.filter((f) => {
    if (formFilter !== 'ALL' && !f.form.toUpperCase().includes(formFilter)) {
      return false
    }
    if (search.trim()) {
      const q = search.toLowerCase()
      return (
        f.accession.toLowerCase().includes(q) ||
        f.primary_document.toLowerCase().includes(q) ||
        f.document_title.toLowerCase().includes(q) ||
        f.form.toLowerCase().includes(q)
      )
    }
    return true
  })

  const totalPages = Math.ceil(filtered.length / pageSize)
  const paginated = filtered.slice((page - 1) * pageSize, page * pageSize)

  const formColor = (form: string) => {
    if (form === '10-K') return 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
    return 'bg-neutral-100 dark:bg-neutral-900 text-neutral-800 dark:text-neutral-200 border border-neutral-300 dark:border-neutral-800'
  }

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* Top Main Tab Toggle: Filings Directory vs. 10-K Redline & Diff */}
      <div className="flex items-center gap-2 border-b border-neutral-200 dark:border-neutral-800 pb-3">
        <button
          onClick={() => setMainView('filings')}
          className={`px-3 py-1.5 rounded text-xs font-mono transition-colors flex items-center gap-1.5 ${
            mainView === 'filings'
              ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
              : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
          }`}
        >
          <FileText className="w-3.5 h-3.5" />
          <span>Saved SEC Filings ({filings.length})</span>
        </button>
        <button
          onClick={() => setMainView('diff')}
          className={`px-3 py-1.5 rounded text-xs font-mono transition-colors flex items-center gap-1.5 ${
            mainView === 'diff'
              ? 'bg-neutral-900 text-white dark:bg-white dark:text-black font-semibold'
              : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
          }`}
        >
          <GitCompare className="w-3.5 h-3.5 text-amber-500" />
          <span>10-K Redline &amp; Diff (Item 1A / Item 7)</span>
          <span className="px-1.5 py-0.2 rounded text-[9px] bg-amber-500/20 text-amber-700 dark:text-amber-300 font-sans uppercase font-semibold">
            Institutional
          </span>
        </button>
      </div>

      {mainView === 'diff' ? (
        <FilingsDiff ticker={ticker} />
      ) : (
        <>
          {/* Filters & Search Toolbar */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-2 text-xs font-mono">
        {/* Form Filter Segments */}
        <div className="flex items-center border border-neutral-300 dark:border-neutral-800 divide-x divide-neutral-300 dark:divide-neutral-800">
          {['ALL', '10-K', '10-Q', '8-K', 'SC 13G'].map((f) => (
            <button
              key={f}
              onClick={() => {
                setFormFilter(f)
                setPage(1)
              }}
              className={`px-3 py-1 font-mono text-xs transition-colors whitespace-nowrap ${
                formFilter === f
                  ? 'bg-neutral-900 dark:bg-neutral-100 text-white dark:text-neutral-950 font-medium'
                  : 'bg-white dark:bg-neutral-950 text-neutral-600 dark:text-neutral-400 hover:text-neutral-950 dark:hover:text-white'
              }`}
            >
              {f === 'ALL' ? 'All Filings' : f}
            </button>
          ))}
        </div>

        {/* Search Input */}
        <div className="relative min-w-[220px]">
          <Search className="w-3.5 h-3.5 text-neutral-400 absolute left-2.5 top-2" />
          <input
            type="text"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(1)
            }}
            placeholder="Filter accession, document..."
            className="w-full bg-white dark:bg-neutral-950 border border-neutral-300 dark:border-neutral-800 pl-8 pr-3 py-1 text-xs text-neutral-800 dark:text-neutral-200 placeholder:text-neutral-400 outline-none focus:border-neutral-500 font-mono"
          />
        </div>
      </div>

      {loading && (
        <div className="py-12 text-center text-xs text-neutral-400 font-mono">
          Loading SEC EDGAR filings for {ticker}...
        </div>
      )}

      {error && (
        <div className="p-4 border border-rose-300 dark:border-rose-900/40 bg-rose-50 dark:bg-rose-950/20 text-xs text-rose-700 dark:text-rose-300 font-mono">
          {error}
        </div>
      )}

      {!loading && !error && (
        <div className="border border-neutral-200/80 dark:border-white/10 bg-white/70 dark:bg-[#030611]/60 backdrop-blur-md overflow-hidden">
          <div className="px-4 py-2.5 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/50 dark:bg-neutral-900/40 flex justify-between items-center text-xs font-mono">
            <span className="text-neutral-950 dark:text-white font-medium font-sans">
              Saved SEC Filings ({filtered.length} total)
            </span>
            <span className="text-neutral-500 font-sans">
              Page {page} of {Math.max(1, totalPages)}
            </span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono">
              <thead>
                <tr className="border-b border-neutral-200 dark:border-neutral-800 text-neutral-500 dark:text-neutral-400 bg-neutral-50/20 dark:bg-neutral-900/20 font-mono text-[11px] uppercase tracking-wider">
                  <th className="py-2.5 px-4 font-normal">Form</th>
                  <th className="py-2.5 px-4 font-normal">Filing Date</th>
                  <th className="py-2.5 px-4 font-normal">Report Date</th>
                  <th className="py-2.5 px-4 font-normal">Accession Number</th>
                  <th className="py-2.5 px-4 font-normal">Primary Document</th>
                  <th className="py-2.5 px-4 font-normal text-right">SEC Archive</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                {paginated.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="py-8 text-center text-neutral-400 font-mono">
                      No filings found matching your filter.
                    </td>
                  </tr>
                ) : (
                  paginated.map((f) => (
                    <tr
                      key={f.accession}
                      onClick={() => setSelectedFiling(f)}
                      className="cursor-pointer hover:bg-neutral-50/80 dark:hover:bg-neutral-900/40 transition-colors group"
                    >
                      <td className="py-2.5 px-4">
                        <span className={`px-1.5 py-0.5 text-[10px] font-mono ${formColor(f.form)}`}>
                          {f.form}
                        </span>
                      </td>
                      <td className="py-2.5 px-4 text-neutral-700 dark:text-neutral-300 tabular-nums">
                        {formatDate(f.filing_date)}
                      </td>
                      <td className="py-2.5 px-4 text-neutral-500 dark:text-neutral-400 tabular-nums">
                        {formatDate(f.report_date)}
                      </td>
                      <td className="py-2.5 px-4 text-neutral-500 dark:text-neutral-400 font-mono text-[11px] group-hover:text-neutral-900 dark:group-hover:text-white">
                        {f.accession}
                      </td>
                      <td className="py-2.5 px-4 text-neutral-800 dark:text-neutral-300 truncate max-w-[180px]">
                        {f.primary_document}
                      </td>
                      <td className="py-2.5 px-4 text-right" onClick={(e) => e.stopPropagation()}>
                        {f.url ? (
                          <a
                            href={f.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 px-2 py-0.5 bg-neutral-100 dark:bg-neutral-900 hover:bg-neutral-200 dark:hover:bg-neutral-800 text-neutral-700 dark:text-neutral-300 hover:text-neutral-950 dark:hover:text-white border border-neutral-200 dark:border-neutral-800 text-[11px] transition-colors font-sans"
                          >
                            <span>Open</span>
                            <ExternalLink className="w-2.5 h-2.5" />
                          </a>
                        ) : (
                          <span className="text-neutral-400 dark:text-neutral-600">—</span>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          {/* Pagination Footer */}
          {totalPages > 1 && (
            <div className="px-4 py-2 border-t border-neutral-200 dark:border-neutral-800 bg-neutral-50/30 dark:bg-neutral-900/30 flex justify-between items-center text-xs font-mono">
              <button
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                className="px-2.5 py-1 border border-neutral-300 dark:border-neutral-800 bg-white dark:bg-neutral-900 disabled:opacity-30 disabled:cursor-not-allowed hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-700 dark:text-neutral-300 transition-colors"
              >
                Previous
              </button>

              <span className="text-neutral-500">
                {page} / {totalPages}
              </span>

              <button
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
                className="px-2.5 py-1 border border-neutral-300 dark:border-neutral-800 bg-white dark:bg-neutral-900 disabled:opacity-30 disabled:cursor-not-allowed hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-700 dark:text-neutral-300 transition-colors"
              >
                Next
              </button>
            </div>
          )}
        </div>
      )}

      {/* Inline Filing Inspector & XBRL Facts Reader Modal */}
      {selectedFiling && (() => {
        const matchingFacts = auditFacts.filter(
          (f) => f.accession === selectedFiling.accession
        )
        const accessionClean = selectedFiling.accession.replace(/-/g, '')
        const secArchiveURL = `https://www.sec.gov/Archives/edgar/data/${parseInt(selectedFiling.cik, 10)}/${accessionClean}/`

        return (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-xs animate-in fade-in duration-100">
            <div className="bg-white dark:bg-neutral-950 border border-neutral-300 dark:border-neutral-800 max-w-4xl w-full max-h-[85vh] flex flex-col shadow-2xl overflow-hidden font-mono text-xs">
              {/* Header */}
              <div className="px-5 py-3 border-b border-neutral-200 dark:border-neutral-800 flex items-center justify-between bg-neutral-50/50 dark:bg-neutral-900/40">
                <div className="flex items-center gap-2">
                  <span className={`px-1.5 py-0.5 text-[10px] font-mono ${formColor(selectedFiling.form)}`}>
                    {selectedFiling.form}
                  </span>
                  <h3 className="font-sans font-medium text-xs text-neutral-950 dark:text-white">
                    {ticker} SEC EDGAR Filing Inspector
                  </h3>
                  <span className="text-neutral-400 text-[11px]">
                    Filed {formatDate(selectedFiling.filing_date)}
                  </span>
                </div>
                <button
                  onClick={() => setSelectedFiling(null)}
                  className="p-1 text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-colors"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>

              {/* Metadata Details Bar */}
              <div className="px-5 py-2.5 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/20 dark:bg-neutral-900/20 flex flex-wrap items-center justify-between gap-3 text-xs">
                <div className="flex items-center gap-4 text-neutral-600 dark:text-neutral-400">
                  <div className="flex items-center gap-1.5">
                    <span className="text-neutral-500 font-sans">Accession:</span>
                    <span className="font-semibold text-neutral-900 dark:text-white">
                      {selectedFiling.accession}
                    </span>
                    <button
                      onClick={() => copyAccession(selectedFiling.accession)}
                      className="p-1 hover:text-neutral-900 dark:hover:text-white transition-colors"
                      title="Copy Accession Number"
                    >
                      {copiedAccession ? (
                        <Check className="w-3 h-3 text-emerald-500" />
                      ) : (
                        <Copy className="w-3 h-3" />
                      )}
                    </button>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <span className="text-neutral-500 font-sans">Report Date:</span>
                    <span className="text-neutral-900 dark:text-white">
                      {formatDate(selectedFiling.report_date)}
                    </span>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <span className="text-neutral-500 font-sans">Document:</span>
                    <span className="text-neutral-900 dark:text-white">
                      {selectedFiling.primary_document}
                    </span>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {selectedFiling.url && (
                    <a
                      href={selectedFiling.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 px-2.5 py-1 bg-neutral-900 text-white dark:bg-white dark:text-black hover:opacity-90 text-xs font-sans font-medium transition-opacity"
                    >
                      <span>Read Document</span>
                      <ExternalLink className="w-3 h-3" />
                    </a>
                  )}

                  <a
                    href={secArchiveURL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 px-2.5 py-1 border border-neutral-300 dark:border-neutral-800 hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-700 dark:text-neutral-300 text-xs font-sans transition-colors"
                  >
                    <span>Full SEC Folder</span>
                    <ExternalLink className="w-3 h-3" />
                  </a>
                </div>
              </div>

              {/* Modal Sub-Tabs */}
              <div className="px-5 border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50/40 dark:bg-neutral-900/40 flex items-center gap-4 text-xs font-mono">
                <button
                  onClick={() => setInspectorTab('facts')}
                  className={`py-2 border-b-2 -mb-px transition-colors ${
                    inspectorTab === 'facts'
                      ? 'border-neutral-900 dark:border-white text-neutral-950 dark:text-white font-medium'
                      : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
                  }`}
                >
                  Standardized XBRL Facts ({matchingFacts.length})
                </button>
                <button
                  onClick={() => setInspectorTab('reader')}
                  className={`py-2 border-b-2 -mb-px transition-colors ${
                    inspectorTab === 'reader'
                      ? 'border-neutral-900 dark:border-white text-neutral-950 dark:text-white font-medium'
                      : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
                  }`}
                >
                  Document Details &amp; Direct EDGAR Filing
                </button>
              </div>

              {/* Body: Extracted XBRL Facts */}
              {inspectorTab === 'facts' && (
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                  <div className="flex items-center justify-between text-xs font-sans text-neutral-500">
                    <span className="font-medium text-neutral-900 dark:text-neutral-200">
                      Standardized XBRL Facts ({matchingFacts.length})
                    </span>
                    <span className="font-mono text-[11px]">
                      Extracted from official SEC facts table
                    </span>
                  </div>

                  {loadingAudit ? (
                    <div className="py-16 text-center text-neutral-400 font-mono">
                      Matching XBRL facts for accession {selectedFiling.accession}...
                    </div>
                  ) : matchingFacts.length === 0 ? (
                    <div className="p-8 text-center text-neutral-400 font-mono space-y-2">
                      <p>No tagged line-item facts found for this filing accession.</p>
                      <p className="text-[11px] text-neutral-500 font-sans">
                        Use the "Document Details &amp; Direct EDGAR Filing" tab to view the primary document.
                      </p>
                    </div>
                  ) : (
                    <div className="border border-neutral-200 dark:border-neutral-800 overflow-hidden">
                      <table className="w-full text-left text-xs font-mono">
                        <thead>
                          <tr className="border-b border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900/50 text-neutral-500 font-sans">
                            <th className="py-2 px-3">Concept &amp; Taxonomy</th>
                            <th className="py-2 px-3">Period End</th>
                            <th className="py-2 px-3 text-right">Value</th>
                            <th className="py-2 px-3">Form</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-neutral-100 dark:divide-neutral-900">
                          {matchingFacts.map((f, i) => (
                            <tr key={i} className="hover:bg-neutral-50 dark:hover:bg-neutral-900/50">
                              <td className="py-2 px-3">
                                <span className="text-neutral-500 font-mono text-[11px]">
                                  {f.taxonomy}:
                                </span>
                                <span className="text-neutral-900 dark:text-white ml-1">
                                  {f.concept}
                                </span>
                                {f.label && f.label !== f.concept && (
                                  <div className="text-[10px] text-neutral-400 font-sans truncate max-w-md">
                                    {f.label}
                                  </div>
                                )}
                              </td>
                              <td className="py-2 px-3 text-neutral-600 dark:text-neutral-400">
                                {f.period_end}
                              </td>
                              <td className="py-2 px-3 text-right tabular-nums text-neutral-900 dark:text-white font-medium">
                                {f.value !== undefined ? f.value.toLocaleString() : '—'}
                                <span className="text-[10px] text-neutral-400 ml-1">
                                  {f.unit}
                                </span>
                              </td>
                              <td className="py-2 px-3">
                                <span className="px-1.5 py-0.5 bg-neutral-100 dark:bg-neutral-900 text-[10px]">
                                  {f.form}
                                </span>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              )}

              {/* Body: Direct EDGAR Filing & Archive Document Details */}
              {inspectorTab === 'reader' && (
                <div className="flex-1 overflow-y-auto p-6 space-y-6">
                  <div className="border border-neutral-200 dark:border-neutral-800 p-5 rounded-lg bg-neutral-50/50 dark:bg-neutral-900/30 space-y-4">
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-neutral-200 dark:border-neutral-800 pb-4">
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-neutral-400 font-sans block">
                          Primary Exhibit Document
                        </span>
                        <h4 className="text-sm font-semibold text-neutral-950 dark:text-white mt-0.5">
                          {selectedFiling.primary_document}
                        </h4>
                        <span className="text-neutral-500 text-xs font-sans">
                          {selectedFiling.document_title || `Official SEC Form ${selectedFiling.form} Exhibit`}
                        </span>
                      </div>

                      {selectedFiling.url && (
                        <a
                          href={selectedFiling.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-neutral-900 text-white dark:bg-white dark:text-black hover:opacity-90 rounded font-medium text-xs font-sans transition-opacity self-start sm:self-auto"
                        >
                          <span>Open Filing in SEC EDGAR Viewer</span>
                          <ExternalLink className="w-3.5 h-3.5" />
                        </a>
                      )}
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs font-mono">
                      <div>
                        <span className="text-neutral-400 text-[11px] block">Full Accession Number</span>
                        <span className="text-neutral-900 dark:text-white font-medium select-all">
                          {selectedFiling.accession}
                        </span>
                      </div>
                      <div>
                        <span className="text-neutral-400 text-[11px] block">Central Index Key (CIK)</span>
                        <span className="text-neutral-900 dark:text-white font-medium select-all">
                          {selectedFiling.cik}
                        </span>
                      </div>
                      <div>
                        <span className="text-neutral-400 text-[11px] block">Filing Date</span>
                        <span className="text-neutral-900 dark:text-white font-medium">
                          {formatDate(selectedFiling.filing_date)}
                        </span>
                      </div>
                      <div>
                        <span className="text-neutral-400 text-[11px] block">Report Period End</span>
                        <span className="text-neutral-900 dark:text-white font-medium">
                          {formatDate(selectedFiling.report_date)}
                        </span>
                      </div>
                    </div>

                    <div className="pt-2 border-t border-neutral-200 dark:border-neutral-800 flex flex-wrap items-center gap-3">
                      <a
                        href={secArchiveURL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 px-3 py-1.5 border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-800 dark:text-neutral-200 rounded text-xs font-sans transition-colors"
                      >
                        <span>Browse Raw SEC Directory Archives</span>
                        <ExternalLink className="w-3 h-3" />
                      </a>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        )
      })()}
        </>
      )}
    </div>
  )
}
