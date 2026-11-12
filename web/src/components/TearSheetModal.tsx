import React, { useState, useEffect } from 'react'
import { X, Copy, Check, Download, Camera, ExternalLink, Sparkles } from 'lucide-react'
import { SummaryResponse, QuoteResponse } from '../types'
import { renderTearSheetToBlob } from '../lib/tearSheetCanvas'

interface TearSheetModalProps {
  open: boolean
  onClose: () => void
  summary: SummaryResponse | null
  quote: QuoteResponse | null
  onToast?: (message: string) => void
}

export const TearSheetModal: React.FC<TearSheetModalProps> = ({
  open,
  onClose,
  summary,
  quote,
  onToast,
}) => {
  const [blob, setBlob] = useState<Blob | null>(null)
  const [dataUrl, setDataUrl] = useState<string | null>(null)
  const [generating, setGenerating] = useState<boolean>(false)
  const [copied, setCopied] = useState<boolean>(false)

  useEffect(() => {
    if (!open || !summary) return
    let ignore = false
    setGenerating(true)
    setCopied(false)

    renderTearSheetToBlob(summary, quote)
      .then((b) => {
        if (!ignore) {
          setBlob(b)
          const url = URL.createObjectURL(b)
          setDataUrl(url)
        }
      })
      .catch((err) => {
        console.error('Failed to generate Tear Sheet PNG:', err)
      })
      .finally(() => {
        if (!ignore) setGenerating(false)
      })

    return () => {
      ignore = true
    }
  }, [open, summary, quote])

  // Clean up object URL when modal closes
  useEffect(() => {
    return () => {
      if (dataUrl) {
        URL.revokeObjectURL(dataUrl)
      }
    }
  }, [dataUrl])

  if (!open || !summary) return null

  const handleCopyClipboard = async () => {
    if (!blob) return
    try {
      if (navigator.clipboard && window.ClipboardItem) {
        await navigator.clipboard.write([
          new ClipboardItem({
            'image/png': blob,
          }),
        ])
        setCopied(true)
        onToast?.(`Copied ${summary.ticker} Tear Sheet PNG to clipboard! Ready to paste into X, Slack, or Substack.`)
        setTimeout(() => setCopied(false), 3000)
      } else {
        handleDownload()
      }
    } catch (err) {
      console.warn('Clipboard write failed, downloading fallback:', err)
      handleDownload()
    }
  }

  const handleDownload = () => {
    if (!blob) return
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${summary.ticker}_TearSheet_${new Date().toISOString().split('T')[0]}.png`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    onToast?.(`Downloaded ${summary.ticker} Tear Sheet card PNG!`)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-6 bg-black/70 backdrop-blur-md animate-in fade-in duration-150">
      <div className="bg-[#09090b] border border-neutral-800 rounded-xl max-w-5xl w-full max-h-[92vh] flex flex-col shadow-2xl overflow-hidden font-sans">
        {/* Modal Header */}
        <div className="px-5 py-3.5 border-b border-neutral-800 flex items-center justify-between bg-neutral-950/80">
          <div className="flex items-center gap-2.5">
            <div className="p-1 rounded bg-indigo-500/10 border border-indigo-500/30 text-indigo-400">
              <Camera className="w-4 h-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-white tracking-tight">
                  {summary.company} ({summary.ticker}) Tear Sheet Card
                </h3>
                <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-neutral-800 text-neutral-300 border border-neutral-700">
                  2X Retina PNG • 2400×1500
                </span>
              </div>
              <p className="text-[11px] text-neutral-400 font-sans mt-0.5">
                Paco Coursey editorial craft. Ready to paste directly into X/Twitter, Substack, or Slack (⌘+Shift+S).
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={handleCopyClipboard}
              disabled={generating || !blob}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-mono font-medium transition-all shadow-xs ${
                copied
                  ? 'bg-emerald-600 text-white'
                  : 'bg-white text-black hover:bg-neutral-200 active:scale-95'
              }`}
              title="Copy high-res PNG to clipboard for instant pasting"
            >
              {copied ? (
                <>
                  <Check className="w-3.5 h-3.5" />
                  <span>Copied to Clipboard!</span>
                </>
              ) : (
                <>
                  <Copy className="w-3.5 h-3.5" />
                  <span>Copy Image (⌘V)</span>
                </>
              )}
            </button>

            <button
              onClick={handleDownload}
              disabled={generating || !blob}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-mono text-neutral-300 hover:text-white bg-neutral-900 hover:bg-neutral-800 border border-neutral-800 transition-colors"
              title="Download PNG file"
            >
              <Download className="w-3.5 h-3.5" />
              <span className="hidden sm:inline">Download</span>
            </button>

            <button
              onClick={onClose}
              className="p-1.5 rounded-lg text-neutral-400 hover:text-white hover:bg-neutral-900 border border-transparent hover:border-neutral-800 transition-colors ml-1"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Modal Body / Image Preview */}
        <div className="p-4 sm:p-6 overflow-y-auto flex-1 flex flex-col items-center justify-center bg-[#070709]">
          {generating ? (
            <div className="py-20 flex flex-col items-center gap-3 text-neutral-400 font-mono text-xs">
              <Sparkles className="w-6 h-6 text-indigo-400 animate-spin" />
              <span>Synthesizing high-resolution dark-mode tear sheet card...</span>
            </div>
          ) : dataUrl ? (
            <div className="w-full flex flex-col items-center gap-3">
              <div className="w-full max-w-4xl border border-neutral-800 rounded-lg overflow-hidden shadow-2xl bg-[#09090b]">
                <img
                  src={dataUrl}
                  alt={`${summary.ticker} Fundamental Tear Sheet`}
                  className="w-full h-auto block select-none"
                />
              </div>
              <div className="text-[11px] font-mono text-neutral-500 flex items-center gap-2">
                <span>Tip: Click <strong>Copy Image</strong> and paste directly into X/Twitter or Slack composer</span>
                <span>•</span>
                <kbd className="px-1 py-0.5 rounded bg-neutral-900 border border-neutral-800 text-[10px]">
                  ⌘+Shift+S
                </kbd>
              </div>
            </div>
          ) : (
            <div className="py-20 text-neutral-500 font-mono text-xs">
              Failed to generate preview image.
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
