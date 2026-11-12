import React from 'react'

export type TabKey = 'statements' | 'dcf' | 'intangibles' | 'filings' | 'screener' | 'compare'

interface TabsProps {
  activeTab: TabKey
  onSelectTab: (tab: TabKey) => void
}

const tabItems: { key: TabKey; label: string; keyNumber: string }[] = [
  { key: 'statements', label: 'Financial Statements', keyNumber: '1' },
  { key: 'dcf', label: 'DCF Valuation', keyNumber: '2' },
  { key: 'intangibles', label: 'Intangibles & ROIC', keyNumber: '3' },
  { key: 'filings', label: 'SEC Filings', keyNumber: '4' },
  { key: 'screener', label: 'Universe Screener', keyNumber: '5' },
  { key: 'compare', label: 'Peer Comparison', keyNumber: '6' },
]

export const Tabs: React.FC<TabsProps> = ({ activeTab, onSelectTab }) => {
  return (
    <nav className="flex items-center gap-6 border-b border-neutral-200 dark:border-neutral-800 overflow-x-auto scrollbar-none font-sans text-xs">
      {tabItems.map((tab) => {
        const isActive = activeTab === tab.key
        return (
          <button
            key={tab.key}
            onClick={() => onSelectTab(tab.key)}
            className={`pb-2.5 pt-1 relative transition-colors whitespace-nowrap flex items-center gap-1.5 ${
              isActive
                ? 'text-neutral-950 dark:text-white font-medium'
                : 'text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-300'
            }`}
          >
            <span>{tab.label}</span>
            <span className="text-[10px] text-neutral-400 dark:text-neutral-600 font-mono">
              [{tab.keyNumber}]
            </span>
            {isActive && (
              <span className="absolute bottom-0 left-0 right-0 h-[2px] bg-neutral-950 dark:bg-white -mb-[1px]" />
            )}
          </button>
        )
      })}
    </nav>
  )
}
