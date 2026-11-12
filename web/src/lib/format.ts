export function formatCurrency(
  value: number | undefined | null,
  compact = true,
  currency = 'USD'
): string {
  if (value === undefined || value === null || isNaN(value)) {
    return '—'
  }

  const symbol = currency === 'USD' ? '$' : currency + ' '

  if (!compact) {
    return `${symbol}${value.toLocaleString('en-US', {
      maximumFractionDigits: 0,
    })}`
  }

  const abs = Math.abs(value)
  const sign = value < 0 ? '-' : ''

  if (abs >= 1e12) {
    return `${sign}${symbol}${(abs / 1e12).toFixed(2)}T`
  }
  if (abs >= 1e9) {
    return `${sign}${symbol}${(abs / 1e9).toFixed(2)}B`
  }
  if (abs >= 1e6) {
    return `${sign}${symbol}${(abs / 1e6).toFixed(2)}M`
  }
  if (abs >= 1e3) {
    return `${sign}${symbol}${(abs / 1e3).toFixed(1)}k`
  }
  return `${sign}${symbol}${abs.toFixed(2)}`
}

export function formatPercent(value: number | undefined | null, decimals = 1): string {
  if (value === undefined || value === null || isNaN(value)) {
    return '—'
  }
  // If value is a fraction (e.g. 0.155) or already in percent (e.g. 15.5)
  const pct = Math.abs(value) <= 1 && value !== 0 ? value * 100 : value
  return `${pct.toFixed(decimals)}%`
}

export function formatShares(value: number | undefined | null): string {
  if (value === undefined || value === null || isNaN(value) || value <= 0) {
    return '—'
  }
  if (value >= 1e9) {
    return `${(value / 1e9).toFixed(2)}B shares`
  }
  if (value >= 1e6) {
    return `${(value / 1e6).toFixed(2)}M shares`
  }
  return `${value.toLocaleString('en-US')} shares`
}

export function formatNumber(value: number | undefined | null, decimals = 2): string {
  if (value === undefined || value === null || isNaN(value)) {
    return '—'
  }
  return value.toLocaleString('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: decimals,
  })
}

export function formatDate(dateStr: string): string {
  if (!dateStr) return '—'
  try {
    const parts = dateStr.split('-')
    if (parts.length === 3) {
      return `${parts[0]}-${parts[1]}-${parts[2]}`
    }
  } catch {}
  return dateStr
}
