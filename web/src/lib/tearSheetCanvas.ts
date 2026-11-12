import { SummaryResponse, QuoteResponse, SummaryValue } from '../types'
import { formatCurrency, formatPercent } from './format'

export interface TearSheetData {
  summary: SummaryResponse
  quote: QuoteResponse | null
}

/**
 * Calculates DCF Intrinsic Values for Bear, Base, and Bull scenarios
 */
export function calculateDCFScenarios(summary: SummaryResponse, quote: QuoteResponse | null) {
  const findVal = (name: string): number | undefined => {
    const m = summary.metrics.find((x) => x.Name === name)
    return m?.Values?.[0]?.Present ? m.Values[0].Value : undefined
  }

  const fcf = findVal('Free Cash Flow') || 0
  const adjOwnerEarnings = summary.intangibles.AdjustedOwnerEarnings?.[0]?.Value || fcf
  const baseFCF = adjOwnerEarnings > 0 ? adjOwnerEarnings : Math.max(1, fcf)

  const shares =
    findVal('Diluted Weighted Average Shares') || findVal('Shares Outstanding') || 1
  const debt = findVal('Debt / Borrowings') || 0
  const cash = findVal('Cash & Equivalents') || 0
  const netDebt = debt - cash

  // 5Y Rev CAGR
  const revMetric = summary.metrics.find((x) => x.Name === 'Revenue')
  let revCAGR = 0.08
  if (revMetric && revMetric.Values.length >= 4) {
    const v0 = revMetric.Values[0]?.Value
    const vEnd = revMetric.Values[revMetric.Values.length - 1]?.Value
    const yrs = revMetric.Values.length - 1
    if (v0 && vEnd && vEnd > 0 && v0 > 0) {
      revCAGR = Math.max(-0.1, Math.min(0.35, Math.pow(v0 / vEnd, 1 / yrs) - 1))
    }
  }

  const calcPV = (growth: number, discount: number, terminal: number, years = 5) => {
    let pvExplicit = 0
    let currFCF = baseFCF
    for (let i = 1; i <= years; i++) {
      currFCF *= 1 + growth
      pvExplicit += currFCF / Math.pow(1 + discount, i)
    }
    const terminalFCF = currFCF * (1 + terminal)
    const denominator = discount - terminal
    const tv = denominator > 0.005 ? terminalFCF / denominator : terminalFCF / 0.05
    const pvTV = tv / Math.pow(1 + discount, years)
    const ev = pvExplicit + pvTV
    const equityVal = ev - netDebt
    return Math.max(0, equityVal / shares)
  }

  const baseG = revCAGR
  const baseR = 0.085
  const baseT = 0.025

  const bearG = Math.max(-0.05, baseG - 0.04)
  const bearR = baseR + 0.015
  const bearT = 0.02

  const bullG = Math.min(0.4, baseG + 0.04)
  const bullR = Math.max(0.065, baseR - 0.01)
  const bullT = 0.03

  const baseVal = calcPV(baseG, baseR, baseT)
  const bearVal = calcPV(bearG, bearR, bearT)
  const bullVal = calcPV(bullG, bullR, bullT)

  const weightedVal = bearVal * 0.25 + baseVal * 0.5 + bullVal * 0.25
  const currentPrice = quote?.price || 0

  return {
    bearVal,
    baseVal,
    bullVal,
    weightedVal,
    currentPrice,
    baseG,
    baseR,
    bearG,
    bullG,
  }
}

/**
 * Renders an ultra-crisp, high-resolution (2x Retina, 2400x1500) Paco Coursey craft dark-mode
 * Tear Sheet card directly to an HTML Canvas and exports it as a PNG Blob.
 */
export async function renderTearSheetToBlob(
  summary: SummaryResponse,
  quote: QuoteResponse | null
): Promise<Blob> {
  const canvas = document.createElement('canvas')
  const scale = 2
  const width = 1200
  const height = 750

  canvas.width = width * scale
  canvas.height = height * scale
  const ctx = canvas.getContext('2d', { alpha: false })!
  ctx.scale(scale, scale)

  // Typography helpers
  const fontMono = (size: number, weight = 'normal') =>
    `${weight} ${size}px "SF Mono", "Fira Code", Menlo, Monaco, Consolas, monospace`
  const fontSans = (size: number, weight = 'normal') =>
    `${weight} ${size}px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Inter, Helvetica, sans-serif`

  // Helper values
  const findVal = (name: string): number | undefined => {
    const m = summary.metrics.find((x) => x.Name === name)
    return m?.Values?.[0]?.Present ? m.Values[0].Value : undefined
  }

  const netIncome = findVal('Net Income')
  const adjEBIT = summary.intangibles.AdjustedEBIT?.[0]?.Value
  const fcf = findVal('Free Cash Flow')
  const adjOwnerEarnings = summary.intangibles.AdjustedOwnerEarnings?.[0]?.Value || fcf
  const shares =
    findVal('Diluted Weighted Average Shares') || findVal('Shares Outstanding') || 1
  const equity = findVal("Stockholders' Equity")
  const debt = findVal('Debt / Borrowings') || 0
  const cash = findVal('Cash & Equivalents') || 0

  const livePrice = quote?.price
  const marketCap = livePrice && shares ? livePrice * shares : undefined
  const enterpriseValue = marketCap !== undefined ? marketCap + debt - cash : undefined
  const pe = marketCap && netIncome && netIncome > 0 ? marketCap / netIncome : undefined
  const p_fcf = marketCap && fcf && fcf > 0 ? marketCap / fcf : undefined
  const ev_ebit = enterpriseValue && adjEBIT && adjEBIT > 0 ? enterpriseValue / adjEBIT : undefined
  const pb = marketCap && equity && equity > 0 ? marketCap / equity : undefined
  const ownerYield =
    adjOwnerEarnings && marketCap && marketCap > 0 ? (adjOwnerEarnings / marketCap) * 100 : undefined

  // -------------------------------------------------------------
  // 1. BACKGROUND & BASE STYLING
  // -------------------------------------------------------------
  ctx.fillStyle = '#09090b'
  ctx.fillRect(0, 0, width, height)

  // Subtle radial ambient glow at top left
  const glow = ctx.createRadialGradient(250, 0, 10, 250, 0, 500)
  glow.addColorStop(0, 'rgba(99, 102, 241, 0.08)')
  glow.addColorStop(1, 'rgba(9, 9, 11, 0)')
  ctx.fillStyle = glow
  ctx.fillRect(0, 0, width, height)

  // Top accent border (Gradient line: Indigo -> Emerald -> Purple)
  const topGrad = ctx.createLinearGradient(0, 0, width, 0)
  topGrad.addColorStop(0, '#6366f1')
  topGrad.addColorStop(0.5, '#10b981')
  topGrad.addColorStop(1, '#a855f7')
  ctx.fillStyle = topGrad
  ctx.fillRect(0, 0, width, 2)

  // Outer thin border
  ctx.strokeStyle = '#27272a'
  ctx.lineWidth = 1
  ctx.strokeRect(0.5, 0.5, width - 1, height - 1)

  // Helper for drawing rounded boxes
  const drawCard = (x: number, y: number, w: number, h: number, fill = '#121217', stroke = '#27272a', r = 6) => {
    ctx.fillStyle = fill
    ctx.strokeStyle = stroke
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.roundRect(x, y, w, h, r)
    ctx.fill()
    ctx.stroke()
  }

  // -------------------------------------------------------------
  // 2. COMPANY HEADER
  // -------------------------------------------------------------
  const headerY = 24
  ctx.textBaseline = 'top'

  // Company Name
  ctx.font = fontSans(24, 'bold')
  ctx.fillStyle = '#ffffff'
  ctx.fillText(summary.company, 28, headerY)
  const nameWidth = ctx.measureText(summary.company).width

  // Ticker Badge
  const badgeX = 28 + nameWidth + 12
  const badgeY = headerY + 1
  ctx.fillStyle = '#1c1c22'
  ctx.strokeStyle = '#3f3f46'
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.roundRect(badgeX, badgeY, 60, 24, 4)
  ctx.fill()
  ctx.stroke()

  ctx.font = fontMono(12, 'bold')
  ctx.fillStyle = '#ffffff'
  ctx.textAlign = 'center'
  ctx.fillText(summary.ticker, badgeX + 30, badgeY + 6)
  ctx.textAlign = 'left'

  // Subtitle line (CIK, SIC, Source)
  ctx.font = fontMono(11, 'normal')
  ctx.fillStyle = '#71717a'
  ctx.fillText(
    `CIK ${summary.cik}  •  ${summary.sic_description || `SIC ${summary.sic}`}  •  SEC EDGAR Standardized Facts`,
    28,
    headerY + 34
  )

  // Right Header: Price & Market Cap
  ctx.textAlign = 'right'
  if (livePrice && livePrice > 0) {
    ctx.font = fontMono(22, 'bold')
    ctx.fillStyle = '#ffffff'
    ctx.fillText(`$${livePrice.toFixed(2)}`, width - 28, headerY)

    ctx.font = fontMono(11, 'normal')
    ctx.fillStyle = '#a1a1aa'
    const capText = marketCap ? `Mkt Cap: ${formatCurrency(marketCap, true)}` : 'SEC Standardized'
    ctx.fillText(capText, width - 28, headerY + 28)
  } else {
    ctx.font = fontMono(12, 'bold')
    ctx.fillStyle = '#a1a1aa'
    ctx.fillText('SEC EDGAR Fundamental Tear Sheet', width - 28, headerY + 6)
  }

  // Badge: Paco Coursey craft watermark
  ctx.font = fontMono(9, 'normal')
  ctx.fillStyle = '#52525b'
  ctx.fillText('PACO COURSEY CRAFT  •  HIGH RESOLUTION TEAR SHEET', width - 28, headerY + 46)
  ctx.textAlign = 'left'

  // -------------------------------------------------------------
  // 3. KEY MULTIPLES STRIP (5 Cards)
  // -------------------------------------------------------------
  const multY = 82
  const multH = 64
  const gap = 12
  const multW = (width - 56 - gap * 4) / 5

  const multiples = [
    { label: 'P/E RATIO', val: pe !== undefined ? `${pe.toFixed(1)}x` : '—', sub: 'GAAP TTM Diluted', color: '#ffffff' },
    { label: 'EV / ADJ. EBIT', val: ev_ebit !== undefined ? `${ev_ebit.toFixed(1)}x` : '—', sub: 'Peters & Taylor', color: '#818cf8' },
    { label: 'P / FCF', val: p_fcf !== undefined ? `${p_fcf.toFixed(1)}x` : '—', sub: 'OCF - CapEx', color: '#34d399' },
    { label: 'OWNER YIELD', val: ownerYield !== undefined ? `${ownerYield.toFixed(1)}%` : '—', sub: 'SBC-Adjusted FCF', color: '#fbbf24' },
    { label: 'P / BOOK', val: pb !== undefined ? `${pb.toFixed(1)}x` : '—', sub: 'Reported Equity', color: '#e4e4e7' },
  ]

  multiples.forEach((m, idx) => {
    const cardX = 28 + idx * (multW + gap)
    drawCard(cardX, multY, multW, multH)

    // Label
    ctx.font = fontMono(9, 'normal')
    ctx.fillStyle = '#71717a'
    ctx.fillText(m.label, cardX + 12, multY + 10)

    // Value
    ctx.font = fontMono(16, 'bold')
    ctx.fillStyle = m.color
    ctx.fillText(m.val, cardX + 12, multY + 24)

    // Subtitle
    ctx.font = fontSans(9, 'normal')
    ctx.fillStyle = '#52525b'
    ctx.fillText(m.sub, cardX + 12, multY + 46)
  })

  // -------------------------------------------------------------
  // 4. MIDDLE SECTION: ROIC & DCF FOOTBALL FIELD
  // -------------------------------------------------------------
  const midY = 158
  const midH = 240
  const halfGap = 16
  const leftW = 550
  const rightW = width - 56 - leftW - halfGap

  // -------------------------------------------------------------
  // 4A: PETERS & TAYLOR ROIC PANEL (Left)
  // -------------------------------------------------------------
  drawCard(28, midY, leftW, midH)

  // Header
  ctx.font = fontMono(10, 'bold')
  ctx.fillStyle = '#a1a1aa'
  ctx.fillText('PETERS & TAYLOR INTANGIBLES & ECONOMIC ROIC', 44, midY + 14)

  ctx.font = fontSans(10, 'normal')
  ctx.fillStyle = '#71717a'
  ctx.fillText('Capitalizes 100% R&D (5Y) & 30% SG&A Brand (3Y) to reverse GAAP distortion', 44, midY + 30)

  // ROIC Spread Display Box
  const latestAdjROIC = summary.intangibles.AdjustedROIC?.[0]?.Value
  const latestRev = findVal('Revenue') || 0
  const latestGAAPOp = findVal('Operating Income') || 0
  const latestEq = findVal("Stockholders' Equity") || 0
  const gaapInvested = Math.max(1, latestEq + debt - cash)
  const gaapROIC = (latestGAAPOp * 0.79) / gaapInvested

  const roicBoxY = midY + 50
  drawCard(44, roicBoxY, leftW - 32, 68, '#18181f', '#2f2f38')

  // GAAP ROIC vs P&T ROIC
  ctx.font = fontMono(10, 'normal')
  ctx.fillStyle = '#a1a1aa'
  ctx.fillText('Reported GAAP ROIC', 58, roicBoxY + 12)
  ctx.font = fontMono(16, 'bold')
  ctx.fillStyle = '#d4d4d8'
  ctx.fillText(formatPercent(gaapROIC), 58, roicBoxY + 28)

  ctx.font = fontMono(10, 'normal')
  ctx.fillStyle = '#c084fc'
  ctx.fillText('Peters & Taylor Adj. ROIC', 210, roicBoxY + 12)
  ctx.font = fontMono(20, 'bold')
  ctx.fillStyle = '#c084fc'
  ctx.fillText(latestAdjROIC ? formatPercent(latestAdjROIC) : formatPercent(gaapROIC), 210, roicBoxY + 26)

  // Spread Badge
  const spread = latestAdjROIC !== undefined ? latestAdjROIC - gaapROIC : 0
  const spreadX = 400
  const spreadY = roicBoxY + 18
  ctx.fillStyle = spread >= 0 ? '#064e3b' : '#4c0519'
  ctx.strokeStyle = spread >= 0 ? '#059669' : '#e11d48'
  ctx.beginPath()
  ctx.roundRect(spreadX, spreadY, 100, 32, 4)
  ctx.fill()
  ctx.stroke()

  ctx.textAlign = 'center'
  ctx.font = fontMono(12, 'bold')
  ctx.fillStyle = spread >= 0 ? '#34d399' : '#fb7185'
  ctx.fillText(`${spread >= 0 ? '+' : ''}${(spread * 100).toFixed(1)}%`, spreadX + 50, spreadY + 8)
  ctx.font = fontMono(8, 'normal')
  ctx.fillStyle = spread >= 0 ? '#6ee7b7' : '#fda4af'
  ctx.fillText('GAAP ARBITRAGE', spreadX + 50, spreadY + 20)
  ctx.textAlign = 'left'

  // Detailed Intangible Balance Sheet Breakdown
  const intangiblesY = roicBoxY + 80
  const latestIntangibleCap = summary.intangibles.IntangibleCapital?.[0]?.Value || 0
  const latestRD = findVal('Research & Development') || 0
  const latestSGA = findVal('Selling, General & Administrative') || 0
  const brandCap = latestSGA * 0.3
  const latestAdjInvested = summary.intangibles.AdjustedInvestedCap?.[0]?.Value || gaapInvested + latestIntangibleCap

  const breakdownRows = [
    { label: 'Knowledge Capital (100% R&D 5Y Amortized)', val: formatCurrency(latestRD * 3.0, true) },
    { label: 'Brand & Organization Capital (30% SG&A 3Y)', val: formatCurrency(brandCap * 1.8, true) },
    { label: 'Total Capitalized Intangible Assets', val: formatCurrency(latestIntangibleCap, true), highlight: true },
    { label: 'Adjusted Economic Invested Capital', val: formatCurrency(latestAdjInvested, true), highlight: true },
  ]

  breakdownRows.forEach((row, i) => {
    const rowY = intangiblesY + i * 22
    ctx.font = fontSans(11, row.highlight ? 'bold' : 'normal')
    ctx.fillStyle = row.highlight ? '#ffffff' : '#a1a1aa'
    ctx.fillText(row.label, 44, rowY)

    ctx.textAlign = 'right'
    ctx.font = fontMono(11, row.highlight ? 'bold' : 'normal')
    ctx.fillStyle = row.highlight ? (i === 2 ? '#c084fc' : '#ffffff') : '#d4d4d8'
    ctx.fillText(row.val, 28 + leftW - 20, rowY)
    ctx.textAlign = 'left'
  })

  // -------------------------------------------------------------
  // 4B: DCF FOOTBALL FIELD VALUATION RANGE (Right)
  // -------------------------------------------------------------
  const rightX = 28 + leftW + halfGap
  drawCard(rightX, midY, rightW, midH)

  // Header
  ctx.font = fontMono(10, 'bold')
  ctx.fillStyle = '#a1a1aa'
  ctx.fillText('DCF VALUATION FOOTBALL FIELD', rightX + 16, midY + 14)

  const dcf = calculateDCFScenarios(summary, quote)

  ctx.font = fontSans(10, 'normal')
  ctx.fillStyle = '#71717a'
  ctx.fillText('Multi-scenario intrinsic equity value vs live market price', rightX + 16, midY + 30)

  // Visual Football Field Canvas
  const ffBoxY = midY + 50
  drawCard(rightX + 16, ffBoxY, rightW - 32, 92, '#18181f', '#2f2f38')

  const minVal = Math.max(0, Math.min(dcf.bearVal, dcf.currentPrice || dcf.bearVal) * 0.8)
  const maxVal = Math.max(dcf.bullVal, dcf.currentPrice || dcf.bullVal) * 1.2
  const valRange = maxVal - minVal || 1

  const barTrackX = rightX + 36
  const barTrackW = rightW - 72
  const barTrackY = ffBoxY + 34
  const barTrackH = 18

  // Background track
  ctx.fillStyle = '#27272a'
  ctx.beginPath()
  ctx.roundRect(barTrackX, barTrackY, barTrackW, barTrackH, 4)
  ctx.fill()

  // Scenario Bar (Bear to Bull range)
  const getValX = (v: number) => barTrackX + ((v - minVal) / valRange) * barTrackW
  const bearX = getValX(dcf.bearVal)
  const bullX = getValX(dcf.bullVal)
  const baseX = getValX(dcf.baseVal)

  const dcfGrad = ctx.createLinearGradient(bearX, 0, bullX, 0)
  dcfGrad.addColorStop(0, '#3b82f6')
  dcfGrad.addColorStop(0.5, '#10b981')
  dcfGrad.addColorStop(1, '#a855f7')

  ctx.fillStyle = dcfGrad
  ctx.beginPath()
  ctx.roundRect(bearX, barTrackY, Math.max(8, bullX - bearX), barTrackH, 4)
  ctx.fill()

  // Base case diamond / marker
  ctx.fillStyle = '#ffffff'
  ctx.beginPath()
  ctx.arc(baseX, barTrackY + barTrackH / 2, 5, 0, Math.PI * 2)
  ctx.fill()
  ctx.stroke()

  // Market Price line
  if (dcf.currentPrice > 0) {
    const marketX = getValX(dcf.currentPrice)
    ctx.strokeStyle = '#f43f5e'
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.moveTo(marketX, barTrackY - 10)
    ctx.lineTo(marketX, barTrackY + barTrackH + 10)
    ctx.stroke()

    // Market Price label
    ctx.textAlign = 'center'
    ctx.font = fontMono(10, 'bold')
    ctx.fillStyle = '#f43f5e'
    ctx.fillText(`Market $${dcf.currentPrice.toFixed(0)}`, marketX, barTrackY - 18)
    ctx.textAlign = 'left'
  }

  // Range labels below bar
  ctx.font = fontMono(10, 'bold')
  ctx.fillStyle = '#60a5fa'
  ctx.fillText(`Bear $${dcf.bearVal.toFixed(0)}`, bearX - 10, barTrackY + barTrackH + 14)

  ctx.font = fontMono(10, 'bold')
  ctx.fillStyle = '#ffffff'
  ctx.fillText(`Base $${dcf.baseVal.toFixed(0)}`, baseX - 12, barTrackY + barTrackH + 14)

  ctx.textAlign = 'right'
  ctx.font = fontMono(10, 'bold')
  ctx.fillStyle = '#c084fc'
  ctx.fillText(`Bull $${dcf.bullVal.toFixed(0)}`, bullX + 16, barTrackY + barTrackH + 14)
  ctx.textAlign = 'left'

  // DCF Summary Cards under football field
  const dcfCardsY = ffBoxY + 104
  const marginOfSafety =
    dcf.currentPrice > 0 ? ((dcf.weightedVal - dcf.currentPrice) / dcf.currentPrice) * 100 : 0

  const dcfSummaryItems = [
    { label: 'BEAR CASE', val: `$${dcf.bearVal.toFixed(2)}`, sub: `Rev +${(dcf.bearG * 100).toFixed(0)}%`, col: '#60a5fa' },
    { label: 'BASE CASE', val: `$${dcf.baseVal.toFixed(2)}`, sub: `Rev +${(dcf.baseG * 100).toFixed(0)}%`, col: '#ffffff' },
    { label: 'BULL CASE', val: `$${dcf.bullVal.toFixed(2)}`, sub: `Rev +${(dcf.bullG * 100).toFixed(0)}%`, col: '#c084fc' },
    {
      label: 'WEIGHTED TARGET',
      val: `$${dcf.weightedVal.toFixed(2)}`,
      sub: `${marginOfSafety >= 0 ? '+' : ''}${marginOfSafety.toFixed(1)}% vs Mkt`,
      col: marginOfSafety >= 0 ? '#34d399' : '#f87171',
      bg: marginOfSafety >= 0 ? 'rgba(5, 150, 105, 0.15)' : 'rgba(225, 29, 72, 0.15)',
    },
  ]

  const dcfCardW = (rightW - 32 - 18) / 4
  dcfSummaryItems.forEach((item, i) => {
    const cx = rightX + 16 + i * (dcfCardW + 6)
    drawCard(cx, dcfCardsY, dcfCardW, 68, item.bg || '#18181f', '#2a2a34')

    ctx.font = fontMono(8, 'normal')
    ctx.fillStyle = '#71717a'
    ctx.fillText(item.label, cx + 8, dcfCardsY + 10)

    ctx.font = fontMono(13, 'bold')
    ctx.fillStyle = item.col
    ctx.fillText(item.val, cx + 8, dcfCardsY + 25)

    ctx.font = fontSans(9, 'normal')
    ctx.fillStyle = '#a1a1aa'
    ctx.fillText(item.sub, cx + 8, dcfCardsY + 46)
  })

  // -------------------------------------------------------------
  // 5. SPARKLINES GRID (4 Panels: Revenue, Op Income, FCF, Adj ROIC)
  // -------------------------------------------------------------
  const sparkY = 414
  const sparkH = 260
  const sparkW = (width - 56 - gap * 3) / 4

  const periods = summary.periods || []
  const n = periods.length
  const chronoYears = [...periods].reverse().map((p) => p.Year)

  const getChronological = (metricName: string) => {
    const m = summary.metrics.find((x) => x.Name === metricName)
    if (!m) return Array(n).fill(null)
    return [...m.Values].reverse().map((v) => (v && v.Present ? v.Value : null))
  }

  const sparkSeries = [
    {
      title: 'REVENUE 5-YEAR TREND',
      vals: getChronological('Revenue'),
      format: (v: number) => formatCurrency(v, true),
      stroke: '#6366f1',
      fill: 'rgba(99, 102, 241, 0.12)',
    },
    {
      title: 'OPERATING INCOME (GAAP)',
      vals: getChronological('Operating Income'),
      format: (v: number) => formatCurrency(v, true),
      stroke: '#10b981',
      fill: 'rgba(16, 185, 129, 0.12)',
    },
    {
      title: 'FREE CASH FLOW (FCF)',
      vals: getChronological('Free Cash Flow'),
      format: (v: number) => formatCurrency(v, true),
      stroke: '#2dd4bf',
      fill: 'rgba(45, 212, 191, 0.12)',
    },
    {
      title: 'P&T INTANGIBLE ROIC',
      vals: summary.intangibles.AdjustedROIC
        ? [...summary.intangibles.AdjustedROIC].reverse().map((v) => (v && v.Present ? v.Value : null))
        : Array(n).fill(null),
      format: (v: number) => formatPercent(v),
      stroke: '#a855f7',
      fill: 'rgba(168, 85, 247, 0.12)',
    },
  ]

  sparkSeries.forEach((s, idx) => {
    const sx = 28 + idx * (sparkW + gap)
    drawCard(sx, sparkY, sparkW, sparkH)

    // Header Title
    ctx.font = fontMono(9, 'bold')
    ctx.fillStyle = '#71717a'
    ctx.fillText(s.title, sx + 12, sparkY + 12)

    // Latest Value
    const validVals = s.vals.filter((v): v is number => v !== null)
    const latest = validVals.length > 0 ? validVals[validVals.length - 1] : 0
    const first = validVals.length > 0 ? validVals[0] : 0

    ctx.font = fontMono(16, 'bold')
    ctx.fillStyle = '#ffffff'
    ctx.fillText(s.format(latest), sx + 12, sparkY + 28)

    // Delta or CAGR
    let deltaText = '5-Yr History'
    if (first > 0 && latest > 0 && validVals.length >= 3) {
      const cagr = Math.pow(latest / first, 1 / (validVals.length - 1)) - 1
      deltaText = `${cagr >= 0 ? '+' : ''}${(cagr * 100).toFixed(1)}% CAGR`
    }
    ctx.font = fontMono(9, 'normal')
    ctx.fillStyle = deltaText.includes('+') ? '#34d399' : '#a1a1aa'
    ctx.fillText(deltaText, sx + 12, sparkY + 48)

    // Sparkline Canvas Area
    const chartAreaX = sx + 12
    const chartAreaY = sparkY + 70
    const chartAreaW = sparkW - 24
    const chartAreaH = sparkH - 100

    if (validVals.length >= 2) {
      const min = Math.min(...validVals)
      const max = Math.max(...validVals)
      const range = max - min || 1

      const getPtX = (i: number) => chartAreaX + (i / (validVals.length - 1)) * chartAreaW
      const getPtY = (val: number) => chartAreaY + chartAreaH - ((val - min) / range) * chartAreaH

      // Area fill
      ctx.beginPath()
      ctx.moveTo(getPtX(0), chartAreaY + chartAreaH)
      validVals.forEach((val, i) => {
        ctx.lineTo(getPtX(i), getPtY(val))
      })
      ctx.lineTo(getPtX(validVals.length - 1), chartAreaY + chartAreaH)
      ctx.closePath()
      ctx.fillStyle = s.fill
      ctx.fill()

      // Stroke Line
      ctx.beginPath()
      validVals.forEach((val, i) => {
        const px = getPtX(i)
        const py = getPtY(val)
        if (i === 0) ctx.moveTo(px, py)
        else ctx.lineTo(px, py)
      })
      ctx.strokeStyle = s.stroke
      ctx.lineWidth = 2
      ctx.stroke()

      // Year labels & Points
      validVals.forEach((val, i) => {
        const px = getPtX(i)
        const py = getPtY(val)

        // Dot
        ctx.fillStyle = '#ffffff'
        ctx.beginPath()
        ctx.arc(px, py, 2.5, 0, Math.PI * 2)
        ctx.fill()
        ctx.strokeStyle = s.stroke
        ctx.lineWidth = 1.5
        ctx.stroke()

        // Year label under chart
        if (chronoYears[i]) {
          ctx.font = fontMono(8, 'normal')
          ctx.fillStyle = '#71717a'
          ctx.textAlign = 'center'
          ctx.fillText(`FY${chronoYears[i].toString().slice(-2)}`, px, chartAreaY + chartAreaH + 12)
          ctx.textAlign = 'left'
        }
      })
    }
  })

  // -------------------------------------------------------------
  // 6. FOOTER STAMP
  // -------------------------------------------------------------
  const footerY = 694
  ctx.font = fontMono(9, 'normal')
  ctx.fillStyle = '#52525b'
  ctx.fillText(
    'SEC EDGAR TERMINAL  •  PETERS & TAYLOR (2017) INTANGIBLES FRAMEWORK  •  100% RECONSTRUCTED XBRL DATA',
    28,
    footerY
  )

  ctx.textAlign = 'right'
  const today = new Date().toISOString().split('T')[0]
  ctx.fillText(`GENERATED ${today}  •  2X RETINA PNG`, width - 28, footerY)
  ctx.textAlign = 'left'

  // Convert canvas to Blob
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob)
      else reject(new Error('Canvas to Blob conversion failed'))
    }, 'image/png')
  })
}
