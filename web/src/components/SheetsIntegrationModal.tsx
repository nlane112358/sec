import React, { useState, useEffect } from 'react'
import {
  X,
  Copy,
  Check,
  Download,
  FileSpreadsheet,
  Terminal,
  ExternalLink,
  Code2,
  Table,
  Zap,
} from 'lucide-react'

interface SheetsIntegrationModalProps {
  isOpen: boolean
  onClose: () => void
  currentTicker: string
}

export const SheetsIntegrationModal: React.FC<SheetsIntegrationModalProps> = ({
  isOpen,
  onClose,
  currentTicker,
}) => {
  const [activeTab, setActiveTab] = useState<'sheets' | 'excel' | 'vba' | 'tester'>('tester')
  const [ticker, setTicker] = useState(currentTicker || 'MSFT')
  const [metric, setMetric] = useState('ADJ_ROIC')
  const [period, setPeriod] = useState('TTM')
  const [testResult, setTestResult] = useState<string>('...')
  const [testFormatted, setTestFormatted] = useState<string>('...')
  const [loadingTest, setLoadingTest] = useState(false)
  const [copiedFormula, setCopiedFormula] = useState(false)
  const [copiedScript, setCopiedScript] = useState(false)
  const [copiedLambda, setCopiedLambda] = useState(false)
  const [scriptCode, setScriptCode] = useState<string>('')

  useEffect(() => {
    if (currentTicker) {
      setTicker(currentTicker)
    }
  }, [currentTicker])

  // Fetch live formula value
  useEffect(() => {
    if (!isOpen) return
    setLoadingTest(true)
    const t = ticker.trim().toUpperCase() || 'MSFT'
    const m = metric.trim().toUpperCase() || 'ADJ_ROIC'
    const p = period.trim() || 'TTM'

    fetch(`/api/sec/${t}/${m}?period=${encodeURIComponent(p)}&format=json`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data) {
          setTestResult(String(data.value))
          setTestFormatted(data.formatted || String(data.value))
        } else {
          setTestResult('Error')
          setTestFormatted('N/A')
        }
      })
      .catch(() => {
        setTestResult('Error')
        setTestFormatted('N/A')
      })
      .finally(() => setLoadingTest(false))
  }, [isOpen, ticker, metric, period])

  // Fetch Apps Script code
  useEffect(() => {
    if (isOpen && !scriptCode) {
      fetch('/api/integrations/sheets/code')
        .then((res) => (res.ok ? res.text() : ''))
        .then((text) => setScriptCode(text))
        .catch(() => {})
    }
  }, [isOpen, scriptCode])

  if (!isOpen) return null

  const liveFormula = `=SEC("${ticker.toUpperCase()}", "${metric}"${period !== 'TTM' ? `, "${period}"` : ''})`
  const excelWebServiceFormula = `=NUMBERVALUE(WEBSERVICE("http://localhost:8080/api/sec/${ticker.toUpperCase()}/${metric}?period=${period}&raw=1"))`
  const excelLambdaDefinition = `=LAMBDA(ticker,metric,[period],NUMBERVALUE(WEBSERVICE("http://localhost:8080/api/sec/" & UPPER(TRIM(ticker)) & "/" & UPPER(TRIM(metric)) & "?period=" & IF(ISOMITTED(period),"TTM",period) & "&raw=1")))`

  const copyToClipboard = (text: string, setter: (val: boolean) => void) => {
    navigator.clipboard.writeText(text)
    setter(true)
    setTimeout(() => setter(false), 2000)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-in fade-in duration-150">
      <div className="relative w-full max-w-3xl rounded-2xl border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-[#0a0f1d] shadow-2xl overflow-hidden font-sans">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-neutral-200 dark:border-neutral-800 px-6 py-4">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-xl bg-emerald-500/10 text-emerald-500 flex items-center justify-center border border-emerald-500/20">
              <FileSpreadsheet className="w-5 h-5" />
            </div>
            <div>
              <h2 className="text-base font-semibold text-neutral-900 dark:text-white flex items-center gap-2">
                <span>Excel & Google Sheets Integration</span>
                <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-500 border border-emerald-500/20 font-normal">
                  =SEC(...) Add-In
                </span>
              </h2>
              <p className="text-xs text-neutral-500 mt-0.5">
                Query institutional SEC fundamentals directly in your spreadsheets via live custom formulas.
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg text-neutral-400 hover:text-neutral-900 dark:hover:text-white hover:bg-neutral-100 dark:hover:bg-neutral-800 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Navigation Tabs */}
        <div className="flex items-center gap-2 border-b border-neutral-200 dark:border-neutral-800 px-6 bg-neutral-50/50 dark:bg-neutral-950/40 text-xs font-mono">
          <button
            onClick={() => setActiveTab('tester')}
            className={`py-3 px-3 border-b-2 font-medium transition-colors flex items-center gap-1.5 ${
              activeTab === 'tester'
                ? 'border-emerald-500 text-emerald-600 dark:text-emerald-400'
                : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
            }`}
          >
            <Zap className="w-3.5 h-3.5" />
            <span>Interactive Tester</span>
          </button>
          <button
            onClick={() => setActiveTab('sheets')}
            className={`py-3 px-3 border-b-2 font-medium transition-colors flex items-center gap-1.5 ${
              activeTab === 'sheets'
                ? 'border-emerald-500 text-emerald-600 dark:text-emerald-400'
                : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
            }`}
          >
            <Table className="w-3.5 h-3.5" />
            <span>Google Sheets (Apps Script)</span>
          </button>
          <button
            onClick={() => setActiveTab('excel')}
            className={`py-3 px-3 border-b-2 font-medium transition-colors flex items-center gap-1.5 ${
              activeTab === 'excel'
                ? 'border-emerald-500 text-emerald-600 dark:text-emerald-400'
                : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
            }`}
          >
            <Code2 className="w-3.5 h-3.5" />
            <span>Excel (LAMBDA / WebService)</span>
          </button>
          <button
            onClick={() => setActiveTab('vba')}
            className={`py-3 px-3 border-b-2 font-medium transition-colors flex items-center gap-1.5 ${
              activeTab === 'vba'
                ? 'border-emerald-500 text-emerald-600 dark:text-emerald-400'
                : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:hover:text-white'
            }`}
          >
            <Terminal className="w-3.5 h-3.5" />
            <span>Excel VBA Module (.bas)</span>
          </button>
        </div>

        {/* Content Area */}
        <div className="p-6 max-h-[65vh] overflow-y-auto space-y-6">
          {activeTab === 'tester' && (
            <div className="space-y-6">
              {/* Formula Builder Inputs */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <label className="block text-[11px] font-mono uppercase text-neutral-400 mb-1.5">
                    Ticker
                  </label>
                  <input
                    type="text"
                    value={ticker}
                    onChange={(e) => setTicker(e.target.value.toUpperCase())}
                    placeholder="MSFT"
                    className="w-full px-3 py-2 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900 text-sm font-mono text-neutral-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-emerald-500/50"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-mono uppercase text-neutral-400 mb-1.5">
                    Metric Line Item
                  </label>
                  <select
                    value={metric}
                    onChange={(e) => setMetric(e.target.value)}
                    className="w-full px-3 py-2 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900 text-sm font-mono text-neutral-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-emerald-500/50"
                  >
                    <optgroup label="Core Intangibles & Returns">
                      <option value="ADJ_ROIC">ADJ_ROIC (Intangible-Adjusted ROIC)</option>
                      <option value="ADJ_EBIT">ADJ_EBIT (Adjusted Operating Income)</option>
                      <option value="INTANGIBLE_CAPITAL">INTANGIBLE_CAPITAL (Peters & Taylor)</option>
                      <option value="ADJ_INVESTED_CAPITAL">ADJ_INVESTED_CAPITAL</option>
                      <option value="OWNER_EARNINGS">OWNER_EARNINGS (SBC-Adjusted FCF)</option>
                    </optgroup>
                    <optgroup label="Income Statement">
                      <option value="REVENUE">REVENUE (Topline Sales)</option>
                      <option value="OPERATING_INCOME">OPERATING_INCOME (EBIT)</option>
                      <option value="NET_INCOME">NET_INCOME (Bottom Line)</option>
                      <option value="GROSS_MARGIN">GROSS_MARGIN (%)</option>
                      <option value="OPERATING_MARGIN">OPERATING_MARGIN (%)</option>
                      <option value="RD">RD (R&D Expense)</option>
                      <option value="SGA">SGA (SG&A Expense)</option>
                    </optgroup>
                    <optgroup label="Cash Flow">
                      <option value="FCF">FCF (Free Cash Flow: OCF - CapEx)</option>
                      <option value="OCF">OCF (Operating Cash Flow)</option>
                      <option value="CAPEX">CAPEX (Capital Expenditures)</option>
                      <option value="SBC">SBC (Stock-Based Compensation)</option>
                    </optgroup>
                    <optgroup label="Balance Sheet">
                      <option value="CASH">CASH (Cash & Equivalents)</option>
                      <option value="DEBT">DEBT (Total Borrowings)</option>
                      <option value="NET_DEBT">NET_DEBT (Debt - Cash)</option>
                      <option value="EQUITY">EQUITY (Stockholders' Equity)</option>
                      <option value="SHARES">SHARES (Period-End Count)</option>
                    </optgroup>
                    <optgroup label="Valuation">
                      <option value="PRICE">PRICE (Live Share Price)</option>
                      <option value="MARKET_CAP">MARKET_CAP (Total Market Value)</option>
                      <option value="PE">PE (Price to Earnings)</option>
                      <option value="EV">EV (Enterprise Value)</option>
                      <option value="FCF_YIELD">FCF_YIELD (Free Cash Flow Yield)</option>
                    </optgroup>
                  </select>
                </div>

                <div>
                  <label className="block text-[11px] font-mono uppercase text-neutral-400 mb-1.5">
                    Period
                  </label>
                  <select
                    value={period}
                    onChange={(e) => setPeriod(e.target.value)}
                    className="w-full px-3 py-2 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900 text-sm font-mono text-neutral-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-emerald-500/50"
                  >
                    <option value="TTM">TTM (Trailing 12 Months)</option>
                    <option value="FY">FY (Latest Fiscal Year)</option>
                    <option value="-1">Prior Year (FY - 1)</option>
                    <option value="2024">FY 2024</option>
                    <option value="2023">FY 2023</option>
                  </select>
                </div>
              </div>

              {/* Live Formula Preview Box */}
              <div className="rounded-2xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50/80 dark:bg-neutral-900/50 p-4 space-y-4">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-mono text-neutral-400 uppercase tracking-wider">
                    Spreadsheet Formula
                  </span>
                  <button
                    onClick={() => copyToClipboard(liveFormula, setCopiedFormula)}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-emerald-500 text-white font-medium text-xs shadow-sm hover:bg-emerald-600 transition-colors"
                  >
                    {copiedFormula ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copiedFormula ? 'Copied!' : 'Copy Formula'}</span>
                  </button>
                </div>

                <div className="p-3.5 rounded-xl bg-white dark:bg-black border border-neutral-200 dark:border-neutral-800 font-mono text-base text-emerald-600 dark:text-emerald-400 select-all font-semibold">
                  {liveFormula}
                </div>

                <div className="flex items-center justify-between text-xs font-mono pt-2 border-t border-neutral-200/60 dark:border-neutral-800/60">
                  <div className="flex items-center gap-2 text-neutral-500">
                    <span>Live Output:</span>
                    {loadingTest ? (
                      <span className="text-neutral-400 animate-pulse">Calculating...</span>
                    ) : (
                      <span className="text-neutral-900 dark:text-white font-semibold text-sm">
                        {testFormatted}
                      </span>
                    )}
                  </div>
                  <div className="text-neutral-400 text-[11px]">
                    Raw: {testResult}
                  </div>
                </div>
              </div>

              {/* Supported Formulas Grid */}
              <div className="space-y-2">
                <h4 className="text-xs font-mono uppercase text-neutral-400">Popular Examples</h4>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs font-mono">
                  <div className="p-2.5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-white/50 dark:bg-neutral-900/30 flex justify-between items-center">
                    <span className="text-neutral-800 dark:text-neutral-200">=SEC("MSFT", "ADJ_ROIC")</span>
                    <span className="text-emerald-500">Adjusted ROIC</span>
                  </div>
                  <div className="p-2.5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-white/50 dark:bg-neutral-900/30 flex justify-between items-center">
                    <span className="text-neutral-800 dark:text-neutral-200">=SEC("AAPL", "REVENUE", "TTM")</span>
                    <span className="text-neutral-500">TTM Revenue</span>
                  </div>
                  <div className="p-2.5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-white/50 dark:bg-neutral-900/30 flex justify-between items-center">
                    <span className="text-neutral-800 dark:text-neutral-200">=SEC("NVDA", "FCF")</span>
                    <span className="text-neutral-500">Free Cash Flow</span>
                  </div>
                  <div className="p-2.5 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-white/50 dark:bg-neutral-900/30 flex justify-between items-center">
                    <span className="text-neutral-800 dark:text-neutral-200">=SEC("GOOGL", "OWNER_EARNINGS")</span>
                    <span className="text-neutral-500">Owner Earnings</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'sheets' && (
            <div className="space-y-5">
              <div className="p-4 rounded-xl border border-emerald-500/20 bg-emerald-500/5 text-xs space-y-2">
                <h4 className="font-semibold text-emerald-800 dark:text-emerald-300 font-sans flex items-center gap-1.5">
                  <Check className="w-4 h-4" />
                  <span>3-Step Google Sheets Installation</span>
                </h4>
                <ol className="list-decimal list-inside space-y-1 text-neutral-600 dark:text-neutral-400">
                  <li>In any Google Sheet, open menu <strong>Extensions &gt; Apps Script</strong>.</li>
                  <li>Replace the file contents with the script below and press <strong>Save (⌘S / Ctrl+S)</strong>.</li>
                  <li>Return to your spreadsheet and type <code className="bg-emerald-500/10 px-1 py-0.5 rounded text-emerald-600 dark:text-emerald-400 font-mono font-semibold">=SEC("MSFT", "ADJ_ROIC")</code>!</li>
                </ol>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-mono text-neutral-400">Google Apps Script (SEC.gs)</span>
                  <button
                    onClick={() => copyToClipboard(scriptCode, setCopiedScript)}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-neutral-900 text-white dark:bg-white dark:text-black font-medium text-xs shadow-sm hover:opacity-90 transition-opacity"
                  >
                    {copiedScript ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copiedScript ? 'Copied Full Script!' : 'Copy Script Code'}</span>
                  </button>
                </div>

                <pre className="p-4 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-black font-mono text-xs overflow-x-auto text-neutral-800 dark:text-neutral-200 max-h-72">
                  {scriptCode || '// Loading script...'}
                </pre>
              </div>
            </div>
          )}

          {activeTab === 'excel' && (
            <div className="space-y-5">
              <div className="p-4 rounded-xl border border-indigo-500/20 bg-indigo-500/5 text-xs space-y-2">
                <h4 className="font-semibold text-indigo-800 dark:text-indigo-300 font-sans flex items-center gap-1.5">
                  <Zap className="w-4 h-4" />
                  <span>Zero-Install Excel Direct Formulas</span>
                </h4>
                <p className="text-neutral-600 dark:text-neutral-400">
                  Modern Excel supports direct web queries and custom LAMBDA formulas without installing macros, VBA, or plugins.
                </p>
              </div>

              {/* Direct WebService Formula */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-mono text-neutral-400">Direct Cell Formula (=WEBSERVICE)</span>
                  <button
                    onClick={() => copyToClipboard(excelWebServiceFormula, setCopiedFormula)}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-neutral-900 text-white dark:bg-white dark:text-black font-medium text-xs shadow-sm hover:opacity-90 transition-opacity"
                  >
                    <Copy className="w-3.5 h-3.5" />
                    <span>Copy Formula</span>
                  </button>
                </div>
                <div className="p-3.5 rounded-xl bg-neutral-50 dark:bg-black border border-neutral-200 dark:border-neutral-800 font-mono text-xs text-neutral-800 dark:text-neutral-200 select-all overflow-x-auto">
                  {excelWebServiceFormula}
                </div>
              </div>

              {/* LAMBDA Definition */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-mono text-neutral-400">Excel LAMBDA Name Manager Definition</span>
                  <button
                    onClick={() => copyToClipboard(excelLambdaDefinition, setCopiedLambda)}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-neutral-900 text-white dark:bg-white dark:text-black font-medium text-xs shadow-sm hover:opacity-90 transition-opacity"
                  >
                    {copiedLambda ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copiedLambda ? 'Copied!' : 'Copy LAMBDA'}</span>
                  </button>
                </div>
                <div className="p-3.5 rounded-xl bg-neutral-50 dark:bg-black border border-neutral-200 dark:border-neutral-800 font-mono text-xs text-neutral-800 dark:text-neutral-200 select-all overflow-x-auto">
                  {excelLambdaDefinition}
                </div>
                <p className="text-[11px] text-neutral-500">
                  Tip: In Excel, go to Formulas &gt; Name Manager &gt; New. Name it <strong>SEC</strong>, paste the formula above, and you can now use <code className="font-mono text-emerald-600 dark:text-emerald-400">=SEC("MSFT", "ADJ_ROIC")</code> directly!
                </p>
              </div>
            </div>
          )}

          {activeTab === 'vba' && (
            <div className="space-y-5">
              <div className="p-4 rounded-xl border border-neutral-200 dark:border-neutral-800 bg-neutral-50 dark:bg-neutral-900/50 text-xs space-y-2">
                <h4 className="font-semibold text-neutral-900 dark:text-white font-sans">
                  Excel VBA Add-In Module (Mac & Windows)
                </h4>
                <p className="text-neutral-600 dark:text-neutral-400">
                  For classic Excel workbooks (.xlsm, .xlam), import the <code className="font-mono font-semibold">SEC.bas</code> module to enable native <code className="font-mono text-emerald-600 dark:text-emerald-400">=SEC(...)</code> across both Windows and macOS Excel.
                </p>
              </div>

              <div className="flex flex-col sm:flex-row items-center gap-3">
                <a
                  href="/api/integrations/excel/vba"
                  download="SEC.bas"
                  className="w-full sm:w-auto flex items-center justify-center gap-2 px-4 py-2.5 rounded-xl bg-emerald-500 text-white font-semibold text-xs shadow-sm hover:bg-emerald-600 transition-colors"
                >
                  <Download className="w-4 h-4" />
                  <span>Download SEC.bas Module</span>
                </a>
                <span className="text-xs text-neutral-400">or open VBA Editor (Alt+F11) &gt; File &gt; Import</span>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-neutral-200 dark:border-neutral-800 px-6 py-3.5 bg-neutral-50/50 dark:bg-neutral-950/40 flex items-center justify-between text-xs text-neutral-500 font-mono">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-emerald-500" />
            <span>SEC REST Endpoint: http://localhost:8080/api/sec</span>
          </div>
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded-lg border border-neutral-200 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300 hover:text-neutral-900 dark:hover:text-white transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
