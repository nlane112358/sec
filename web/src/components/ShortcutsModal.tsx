import React from 'react'
import { X, Command } from 'lucide-react'

interface ShortcutsModalProps {
  open: boolean
  onClose: () => void
}

const shortcuts = [
  { key: '⌘ K', description: 'Open search and command palette' },
  { key: '⌘ ⇧ S', description: 'Export high-res Tear Sheet PNG card' },
  { key: '⌘ \\', description: 'Toggle the side-by-side split workspace' },
  { key: '1', description: 'Switch to Financial Statements' },
  { key: '2', description: 'Switch to DCF Valuation Modeler' },
  { key: '3', description: 'Switch to Intangibles & Modern ROIC' },
  { key: '4', description: 'Switch to SEC EDGAR Filings' },
  { key: '5', description: 'Switch to Universe Screener' },
  { key: '6', description: 'Switch to Peer Comparison' },
  { key: '[', description: 'Jump to previous company' },
  { key: ']', description: 'Jump to next company' },
  { key: 't', description: 'Toggle light and dark mode' },
  { key: 'b', description: 'Cycle rainbow ambient background (aurora, vibrant, subtle, off)' },
  { key: '?', description: 'Show keyboard shortcuts' },
  { key: 'Esc', description: 'Close command menu / modal' },
]

export const ShortcutsModal: React.FC<ShortcutsModalProps> = ({ open, onClose }) => {
  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-xs animate-in fade-in duration-100">
      <div className="w-full max-w-md border border-neutral-300 dark:border-neutral-800 bg-white dark:bg-neutral-950 p-5 shadow-2xl relative font-mono text-xs">
        <div className="flex items-center justify-between pb-3 border-b border-neutral-200 dark:border-neutral-800">
          <div className="flex items-center gap-2">
            <Command className="w-4 h-4 text-neutral-900 dark:text-white" />
            <h3 className="text-xs font-sans font-medium text-neutral-950 dark:text-white uppercase tracking-wider">
              Keyboard Shortcuts
            </h3>
          </div>
          <button
            onClick={onClose}
            className="text-neutral-400 hover:text-neutral-900 dark:hover:text-white transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="divide-y divide-neutral-100 dark:divide-neutral-900 mt-2 font-mono text-xs">
          {shortcuts.map((s) => (
            <div key={s.key} className="py-2 flex items-center justify-between">
              <span className="text-neutral-600 dark:text-neutral-400 font-sans text-xs">
                {s.description}
              </span>
              <kbd className="px-1.5 py-0.5 bg-neutral-100 dark:bg-neutral-900 border border-neutral-300 dark:border-neutral-800 text-neutral-800 dark:text-neutral-200 text-[11px]">
                {s.key}
              </kbd>
            </div>
          ))}
        </div>

        <div className="mt-4 pt-3 border-t border-neutral-200 dark:border-neutral-800 text-[11px] text-neutral-400 font-mono text-center">
          Press <kbd className="px-1 py-0.5 bg-neutral-100 dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800">Esc</kbd> to dismiss
        </div>
      </div>
    </div>
  )
}
