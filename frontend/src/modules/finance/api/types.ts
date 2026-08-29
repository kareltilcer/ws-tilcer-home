// Finance (v6) — the module's wire types, mirroring the Go backend JSON
// (openapi.yaml).
//
// Column vocabulary is `fin`'s, carried over verbatim (D83): the two income
// slots keep their names. Only the wire style is home's — snake_case (D92).
//
// The `split` block is DERIVED ON READ by the server and never stored (D82).
// Everything the list and the widget render comes from it; the frontend mirror
// in modules/finance/split.ts exists only to preview a split before it is saved.

export interface FinanceRates {
  personal: number
  operational: number
  fun: number
  no_fun: number
}

export interface FinanceSplit {
  total_income: number
  personal_kaja: number
  personal_andy: number
  to_operational_kaja: number
  to_operational_andy: number
  operational_received: number
  fun_savings: number
  no_fun_savings: number
  /** The remainder; absorbs all rounding and can be negative by up to 2 Kč. The
   *  UI shows 0 Kč with a footnote — the DATA is never clamped. */
  needs: number
}

export interface FinanceMonth {
  id: string
  /** YYYY-MM, unique. */
  month: string
  income_kaja: number
  income_andy: number
  rates: FinanceRates
  split: FinanceSplit
  /** NULL for the rows seeded from `fin`, which recorded no actor. */
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface FinanceMonthPage {
  items: FinanceMonth[]
  next_cursor?: string | null
}

export interface FinanceMonthInput {
  month: string
  income_kaja: number
  income_andy: number
  rates: FinanceRates
}
