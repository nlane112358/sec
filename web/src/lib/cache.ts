import { SummaryResponse, QuoteResponse } from '../types'

// Centralized in-memory LRU / Map cache for 0ms transitions
export const summaryCache = new Map<string, SummaryResponse>()
export const quoteCache = new Map<string, QuoteResponse>()

const inFlightSummary = new Map<string, Promise<SummaryResponse | null>>()
const inFlightQuote = new Map<string, Promise<QuoteResponse | null>>()

/**
 * Optimistically prefetches company summary data into summaryCache in the background.
 * Prevents duplicate in-flight requests and avoids refetching if already cached.
 */
export async function prefetchSummary(
  ticker: string,
  cadence: 'annual' | 'quarterly' | 'ttm' = 'annual'
): Promise<SummaryResponse | null> {
  const normTicker = ticker.trim().toUpperCase()
  if (!normTicker) return null

  const cacheKey = `${normTicker}_${cadence}`
  if (summaryCache.has(cacheKey)) {
    return summaryCache.get(cacheKey)!
  }

  if (inFlightSummary.has(cacheKey)) {
    return inFlightSummary.get(cacheKey)!
  }

  // Also prefetch quote if not already cached
  prefetchQuote(normTicker).catch(() => {})

  const url =
    cadence === 'quarterly'
      ? `/api/company/${normTicker}/summary?view=quarterly&quarters=8`
      : cadence === 'ttm'
      ? `/api/company/${normTicker}/summary?view=ttm`
      : `/api/company/${normTicker}/summary?view=annual&years=5`

  const promise = fetch(url)
    .then((res) => {
      if (!res.ok) throw new Error(`Prefetch failed for ${normTicker}`)
      return res.json()
    })
    .then((data: SummaryResponse) => {
      summaryCache.set(cacheKey, data)
      // Also cache as annual default if not present
      if (cadence === 'annual' && !summaryCache.has(`${normTicker}_annual`)) {
        summaryCache.set(`${normTicker}_annual`, data)
      }
      return data
    })
    .catch((err) => {
      console.debug(`Prefetch error for ${normTicker}:`, err)
      return null
    })
    .finally(() => {
      inFlightSummary.delete(cacheKey)
    })

  inFlightSummary.set(cacheKey, promise)
  return promise
}

/**
 * Optimistically prefetches market quote
 */
export async function prefetchQuote(ticker: string): Promise<QuoteResponse | null> {
  const normTicker = ticker.trim().toUpperCase()
  if (!normTicker) return null

  if (quoteCache.has(normTicker)) {
    return quoteCache.get(normTicker)!
  }

  if (inFlightQuote.has(normTicker)) {
    return inFlightQuote.get(normTicker)!
  }

  const promise = fetch(`/api/company/${normTicker}/quote`)
    .then((res) => (res.ok ? res.json() : null))
    .then((data: QuoteResponse | null) => {
      if (data && data.price > 0) {
        quoteCache.set(normTicker, data)
      }
      return data
    })
    .catch(() => null)
    .finally(() => {
      inFlightQuote.delete(normTicker)
    })

  inFlightQuote.set(normTicker, promise)
  return promise
}

// Global registry of pending hover timeouts to prevent spamming
const hoverTimers = new Map<string, number>()

/**
 * Reusable event handlers for 80ms hover prefetching into summaryCache.
 * Attach using: {...prefetchOnHover(ticker)}
 */
export function prefetchOnHover(
  ticker: string,
  cadence: 'annual' | 'quarterly' | 'ttm' = 'annual',
  delayMs = 80
) {
  if (!ticker) return {}
  const norm = ticker.trim().toUpperCase()

  return {
    onMouseEnter: () => {
      // If already cached, no need to set timer
      if (summaryCache.has(`${norm}_${cadence}`)) return

      const existingTimer = hoverTimers.get(norm)
      if (existingTimer) {
        window.clearTimeout(existingTimer)
      }

      const timerId = window.setTimeout(() => {
        prefetchSummary(norm, cadence)
        hoverTimers.delete(norm)
      }, delayMs)

      hoverTimers.set(norm, timerId)
    },
    onMouseLeave: () => {
      const existingTimer = hoverTimers.get(norm)
      if (existingTimer) {
        window.clearTimeout(existingTimer)
        hoverTimers.delete(norm)
      }
    },
  }
}
