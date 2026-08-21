// Elektřina / electricity (v8) — the wire types (openapi.yaml 0.10.0).
//
// TWO UNIT CONVENTIONS RUN THROUGH EVERY FIELD NAME, and the names are the
// documentation:
//
//   *_haler — money in INTEGER haléře. This is home's first module with
//             two money precisions: totals display as whole koruny (21 560 Kč)
//             while unit prices and fees do not (4 858,65 Kč/MWh).
//   *_dkwh  — energy in INTEGER TENTHS of kWh. Karel's meter has no decimal, so
//             the form takes whole kWh and multiplies by 10 on submit; the tenth
//             is kept in storage so a future decimal meter needs no migration.
//
// No float ever reaches the money path, server-side or here.

/** ElectricityMoney — the VT/NT split. `vt_haler + nt_haler` ALWAYS equals
 *  `total_haler` exactly: NT is derived by subtraction rather than rounded on its
 *  own (PRD D158), so the two lines on screen can never fail to add up to the
 *  headline above them. Never design a layout that would need to explain a gap. */
export interface ElMoney {
  vt_haler: number
  nt_haler: number
  total_haler: number
}

export interface ElReading {
  id: string
  read_on: string
  vt_dkwh: number
  nt_dkwh: number
  note: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface ElTariff {
  id: string
  effective_from: string
  price_vt_haler: number
  price_nt_haler: number
  monthly_fee_haler: number
  /** Derived by the server: the day before the next version, null for the current one. */
  effective_to: string | null
  note: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface ElAdvance {
  id: string
  effective_from: string
  amount_haler: number
  due_day: number
  note: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface ElPayment {
  id: string
  month: string
  amount_haler: number
  paid_on: string | null
  note: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface ElPeriod {
  id: string
  starts_on: string
  ends_on: string
  /** While false the UI badges the end date "předpokládaný konec" (PRD D153) —
   *  suppliers frequently do not state one, and a prediction has to project
   *  somewhere. */
  ends_on_confirmed: boolean
  invoiced_total_haler: number | null
  invoiced_balance_haler: number | null
  invoiced_vt_dkwh: number | null
  invoiced_nt_dkwh: number | null
  invoiced_at: string | null
  note: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

/** `period_start` — no reading on the period's first day, so there is no baseline
 *  and the period shows no money at all (D140). `tariff_change` — a ceník change
 *  falls strictly inside a reading interval, so everything from that date on is
 *  unpriceable while everything before it stays valid (D137). */
export type ElBlockingKind = 'period_start' | 'tariff_change'

export interface ElBlocking {
  kind: ElBlockingKind
  /** Pre-fills the reading form — THE DATE ONLY, never a value. */
  required_reading_on: string
  message_cs: string
}

export interface ElSpan {
  from_on: string
  to_on: string
  days: number
  vt_dkwh: number
  nt_dkwh: number
  energy: ElMoney
  fees_haler: number
  total_haler: number
  avg_vt_dkwh_per_day: number | null
  avg_nt_dkwh_per_day: number | null
}

export type ElMonthSource = 'payment' | 'schedule' | 'none'

export interface ElCountedMonth {
  month: string
  amount_haler: number
  source: ElMonthSource
  due_on: string
  is_due: boolean
  due_clamped: boolean
  paid_on: string | null
}

export interface ElAdvancesSummary {
  counted_months: ElCountedMonth[]
  expected_total_haler: number
  due_so_far_haler: number
  months_counted: number
  months_due: number
}

export interface ElHeadroom {
  advance_haler: number
  monthly_fee_haler: number
  energy_budget_haler: number
  kwh_all_vt_dkwh: number
  kwh_all_nt_dkwh: number
  /** The 30 % VT / 70 % NT blend the screen leads with (D162). A STATED
   *  HEURISTIC, never a measurement — the copy says so. */
  kwh_mix_dkwh: number
}

export interface ElInvoiceComparison {
  /** True while the closing odečet is missing, so the computed side is still part
   *  prediction — the ordinary sequence, since the invoice arrives before anyone
   *  walks to the meter cupboard. The panel hedges on this rather than printing a
   *  forecast as *spočteno* and blaming the gap on the supplier's odečet. */
  computed_is_forecast: boolean
  computed_total_haler: number
  invoiced_total_haler: number
  diff_haler: number
  computed_vt_dkwh: number | null
  computed_nt_dkwh: number | null
  invoiced_vt_dkwh: number | null
  invoiced_nt_dkwh: number | null
  diff_vt_dkwh: number | null
  diff_nt_dkwh: number | null
}

/** `ok` — actual plus forecast. `complete` — the closing reading dated ends_on+1
 *  exists, so the period is entirely actual and the page says *skutečnost* rather
 *  than *predikce* (D157). `blocked` — a required reading is missing; figures
 *  before the gap are still valid. `insufficient_data` — no average yet, or no
 *  ceník; the headroom stands in. */
export type ElStatus = 'ok' | 'complete' | 'blocked' | 'insufficient_data'

export type ElReason =
  | 'need_second_reading'
  | 'second_reading_same_day'
  | 'no_tariff'
  | 'missing_opening_reading'
  | 'tariff_change_inside_interval'

export interface ElSummary {
  period: ElPeriod
  status: ElStatus
  reason?: ElReason
  blocking: ElBlocking[]
  last_reading_on: string | null
  days_since_last_reading: number | null
  actual?: ElSpan
  forecast: ElSpan | null
  /** ⚠ NULL — never 0 — in `insufficient_data` and `blocked` (PRD D161). A 0 Kč
   *  nedoplatek is a lie that looks like good news, so the absence is carried by
   *  the type: `cost_total_haler == null` is the ONLY correct test for "is there
   *  a number to show", and `!cost_total_haler` would wrongly swallow a genuine
   *  zero balance. */
  cost_total_haler: number | null
  energy_total_haler: number | null
  fee_total_haler: number | null
  advances: ElAdvancesSummary
  balance_haler: number | null
  recommended_advance_kc: number | null
  recommended_advance_haler: number | null
  headroom?: ElHeadroom
  invoice_comparison: ElInvoiceComparison | null
}

/** One reading-to-reading interval. `energy` is ENERGY ONLY — poplatky are
 *  chunked by (měsíc × ceník) and belong to no interval (D143), which is why the
 *  Odečty row labels its Kč *energie*. */
export interface ElInterval {
  from_on: string
  to_on: string
  days: number
  vt_dkwh: number | null
  nt_dkwh: number | null
  energy: ElMoney | null
  tariff_id: string | null
  tariff_effective_from: string | null
  blocked: boolean
  blocking_date: string | null
}

export interface ElHistoryMonth {
  month: string
  /** APPROXIMATE by construction (D138): an interval's kWh are spread evenly
   *  across its days so month columns can be drawn at all. */
  vt_dkwh: number
  nt_dkwh: number
  /** EXACT. An allocation of already-exact interval costs by day count (D159),
   *  never a repricing of the approximate kWh above. The two kinds of number must
   *  never share a visual treatment. */
  energy_haler: number
  fees_haler: number
  total_haler: number
  is_approximate: boolean
}

export interface ElPage<T> {
  items: T[]
  next_cursor: string | null
}

export interface ElIntervalList {
  items: ElInterval[]
}

export interface ElHistory {
  months: ElHistoryMonth[]
}

// ---- inputs ----

export interface ElReadingInput {
  read_on?: string
  vt_dkwh?: number
  nt_dkwh?: number
  note?: string | null
}

export interface ElTariffInput {
  effective_from?: string
  price_vt_haler?: number
  price_nt_haler?: number
  monthly_fee_haler?: number
  note?: string | null
}

export interface ElAdvanceInput {
  effective_from?: string
  amount_haler?: number
  due_day?: number
  note?: string | null
}

export interface ElPaymentInput {
  month?: string
  amount_haler?: number
  paid_on?: string | null
  note?: string | null
}

export interface ElPeriodInput {
  starts_on?: string
  /** An explicit null re-applies the server's starts_on + 1 rok − 1 den default
   *  (D153), on edit as well as create — so that rule has one implementation
   *  rather than two that could drift. Omitting the key leaves the date alone. */
  ends_on?: string | null
  ends_on_confirmed?: boolean
  invoiced_total_haler?: number | null
  invoiced_balance_haler?: number | null
  invoiced_vt_dkwh?: number | null
  invoiced_nt_dkwh?: number | null
  invoiced_at?: string | null
  note?: string | null
}
