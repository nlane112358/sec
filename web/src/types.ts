export interface Company {
  cik: string
  ticker: string
  name: string
  sic: string
  sic_description: string
}

export interface SummaryPeriod {
  Year: string
  End: string
  Quarter?: string
  Label?: string
  Form?: string
}

export interface SummaryValue {
  Value: number
  Unit: string
  Present: boolean
  Taxonomy?: string
  Concept?: string
  Label?: string
  Form?: string
  FilingDate?: string
  Accession?: string
  PeriodEnd?: string
  Formula?: string
  Calculation?: string
  DocumentURL?: string
}

export interface DiffWord {
  type: 'equal' | 'insert' | 'delete'
  text: string
}

export interface DiffParagraph {
  type: 'equal' | 'added' | 'deleted' | 'modified'
  text: string
  prior_text?: string
  current_text?: string
  words?: DiffWord[]
  heading?: string
}

export interface DiffStats {
  total_current: number
  total_prior: number
  added_paragraphs: number
  deleted_paragraphs: number
  modified_paragraphs: number
  unchanged_paragraphs: number
  similarity_pct: number
}

export interface FilingVersion {
  accession: string
  filing_date: string
  report_date: string
  form: string
  primary_document: string
  year?: string
}

export interface FilingDiffResult {
  ticker: string
  item: '1A' | '7'
  item_title: string
  current_filing: FilingVersion
  prior_filing: FilingVersion
  available_10k_filings: FilingVersion[]
  stats: DiffStats
  diff_items: DiffParagraph[]
}

export interface SummaryMetric {
  Name: string
  Values: SummaryValue[]
}

export interface IntangiblesResult {
  Periods: SummaryPeriod[]
  Currency: string
  IntangibleCapital: SummaryValue[]
  IntangibleAmort: SummaryValue[]
  AdjustedEBIT: SummaryValue[]
  AdjustedInvestedCap: SummaryValue[]
  AdjustedROIC: SummaryValue[]
  AdjustedOwnerEarnings: SummaryValue[]
}

export interface QuarterlyInflection {
  metric: string
  period_label?: string
  latest_value: number
  prior_quarter_value: number
  prior_year_quarter_value: number
  qoq_growth: number
  yoy_growth: number
  prior_yoy_growth: number
  is_accelerating: boolean
}

export interface UniverseItem {
  name: string
  filename: string
  total: number
  synced: number
  description: string
}

export interface SummaryResponse {
  cik: string
  ticker: string
  company: string
  sic: string
  sic_description: string
  view?: 'annual' | 'quarterly' | 'ttm'
  periods: SummaryPeriod[]
  metrics: SummaryMetric[]
  intangibles: IntangiblesResult
  inflections?: QuarterlyInflection[]
}

export interface DCFProjection {
  Year: number
  Revenue: number
  EBIT: number
  NOPAT: number
  DepreciationAmortization: number
  CapitalExpenditure: number
  ChangeInNWC: number
  CapExWedge: number
  FreeCashFlow: number
  PresentValue: number
  HasOperatingDetail: boolean
}

export interface DCFSensitivity {
  DiscountRates: number[]
  TerminalGrowthRates: number[]
  PerShare: number[][]
  Valid: boolean[][]
}

export interface WACCResult {
  RiskFreeRate: number
  Beta: number
  EquityRiskPremium: number
  CostOfEquity: number
  PreTaxCostOfDebt: number
  TaxRate: number
  AfterTaxCostOfDebt: number
  EquityWeight: number
  DebtWeight: number
  Rate: number
}

export interface DCFResult {
  Company: string
  Ticker: string
  BaseYear: string
  Currency: string
  Mode: string
  NormalizeCapEx: boolean
  GrowthRate: number
  GrowthSource: string
  HistoricalFCFCAGR: number
  HasHistoricalFCFCAGR: boolean
  HistoricalRevenueCAGR: number
  HasHistoricalRevenueCAGR: boolean
  OperatingMargin: number
  EffectiveTaxRate: number
  TaxSource: string
  CapExToRevenue: number
  DAToRevenue: number
  NWCToRevenue: number
  NWCSource: string
  DiscountRate: number
  DiscountSource: string
  WACC?: WACCResult
  TerminalRate: number
  Years: number
  BaseFCF: number
  Projections: DCFProjection[]
  PVExplicitCashFlows: number
  TerminalValue: number
  PVTerminalValue: number
  EnterpriseValue: number
  Cash: number
  Debt: number
  DebtSource: string
  EquityValue: number
  SharesOutstanding: number
  ValuePerShare: number
  Sensitivity: DCFSensitivity
}

export interface FilingItem {
  cik: string
  accession: string
  form: string
  filing_date: string
  report_date: string
  primary_document: string
  document_title: string
  url: string
}

export interface DatabaseStats {
  total_companies: number
  total_filings: number
  total_facts: number
  database_path: string
}

export interface ReverseDCFResult {
  Ticker: string
  Company: string
  Mode: string
  Unit: string
  MarketCap: number
  Debt: number
  Cash: number
  EnterpriseValue: number
  BaseCashFlow: number
  BaseRevenue: number
  BaseOwnerMargin: number
  OwnerEarningsAdjusted: boolean
  WACC: number
  DiscountRate: number
  CostOfEquity: number
  AfterTaxCostOfDebt: number
  EffectiveTaxRate: number
  TerminalRate: number
  ForecastYears: number
  ImpliedGrowth: number
  HistoricalCAGR: number
  HasHistoricalCAGR: boolean
  AlphaSpread: number
  IntrinsicAtHistCAGR: number
  MarginOfSafety: number
}

export interface RankedCompany {
  ticker: string
  company: string
  score: number
  year: string
  trend_years: number
  revenue: number
  currency: string
  roic: number
  has_roic: boolean
  roe: number
  has_roe: boolean
  operating_margin: number
  operating_consistency: number
  has_operating_margin: boolean
  fcf_margin: number
  owner_earnings_margin: number
  has_owner_earnings: boolean
  has_sbc_adjustment: boolean
  revenue_cagr: number
  eps_growth: number
  has_eps_growth: boolean
  balance_sheet_quality: number
  has_balance_sheet: boolean
  book_value_growth: number
  has_book_value_growth: boolean
  share_reduction: number
  has_share_trend: boolean
  split_adjusted: boolean
  sector: string
  peer_group: string
}

export interface RankResult {
  companies: RankedCompany[]
  considered: number
  excluded: number
  missing: string[]
  financial_included: number
  financial_excluded: number
  nonfinancial_included: number
  unclassified_included: number
  sic_unavailable: number
  sbc_adjusted: number
  sbc_fallback: number
  missing_solvency_data: number
}

export interface ModernGAAPCandidate {
  ticker: string
  company: string
  sic: string
  sic_description: string
  currency: string
  market_cap: number
  has_market_cap: boolean
  gaap_revenue: number
  gaap_net_income: number
  gaap_operating_income: number
  gaap_operating_margin: number
  gaap_roic: number
  has_gaap_roic: boolean
  gaap_pe: number
  has_gaap_pe: boolean
  gaap_unprofitable: boolean
  intangible_capital: number
  adjusted_ebit: number
  adjusted_ebit_margin: number
  adjusted_invested_cap: number
  adjusted_roic: number
  has_adjusted_roic: boolean
  avg_adjusted_roic: number
  roic_delta: number
  surging_roic: boolean
  owner_earnings: number
  adjusted_owner_earnings: number
  owner_earnings_yield: number
  has_owner_earnings_yield: boolean
  arbitrage_score: number
  arbitrage_signal: string
}

export interface QuoteResponse {
  ticker: string
  price: number
  high_52?: number
  low_52?: number
  currency: string
  as_of: string
}

export interface RawFactItem {
  taxonomy: string
  concept: string
  label: string
  period_end: string
  unit: string
  value: number
  form: string
  filing_date: string
  accession: string
}

export interface AuditResponse {
  cik: string
  ticker: string
  company: string
  raw_facts: RawFactItem[]
}
