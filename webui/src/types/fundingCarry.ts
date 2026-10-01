export interface FundingCarryOverview {
  income_basis: 'gross_account_scoped_futures_funding'
  total_income_24h: number
  total_income_7d: number
  total_income_30d: number
  total_income_all: number
  annualized_yield: number
  active_bots: number
  total_capital_deployed: number
}

export interface FundingCarrySymbol {
  symbol: string
  bot_id: string
  status: string
  capital: number
}

export interface FundingCarryDailyIncome {
  date: string
  income: number
}

export interface FundingCarryDashboardResponse {
  overview: FundingCarryOverview
  symbols: FundingCarrySymbol[]
  daily_income: FundingCarryDailyIncome[]
}

export interface FundingIncomeRecord {
  symbol: string
  income: number
  asset: string
  trade_time: string
}

export interface FundingIncomeHistoryResponse {
  income_basis: 'gross_account_scoped_futures_funding'
  records: FundingIncomeRecord[]
  total: number
}

export interface BatchCreateFundingRequest {
  exchange: string
  symbols: string[]
  total_capital: number
  allocation: string
  strategy_config: Record<string, unknown>
}

export interface BatchCreateFundingResponse {
  created: string[]
  errors?: string[]
}
