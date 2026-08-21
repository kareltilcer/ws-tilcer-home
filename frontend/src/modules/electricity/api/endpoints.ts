// Elektřina / electricity (v8) — the module's API surface (openapi.yaml 0.10.0).
//
// Thirteen paths and deliberately no more. There is no /recompute (nothing is
// stored, so nothing can be stale), no /close (periods never lock, D139) and no
// /import (there is no history to back-fill).
//
// ⚠ CURSORS ARE DATES, NOT UUIDs (PRD D149). `readings` keysets on `read_on`,
// `tariffs` and `advances` on `effective_from`, `periods` on `starts_on`, and
// `payments` on its YYYY-MM month key. The server 422s a malformed cursor rather
// than silently serving page one — pass a UUID where a date belongs and SQLite
// would compare it lexically, returning an empty page that looks like "no more
// readings".

import { apiFetch } from '@/api/client'
import type {
  ElAdvance,
  ElAdvanceInput,
  ElHistory,
  ElIntervalList,
  ElPage,
  ElPayment,
  ElPaymentInput,
  ElPeriod,
  ElPeriodInput,
  ElReading,
  ElReadingInput,
  ElSummary,
  ElTariff,
  ElTariffInput,
} from './types'

const base = '/api/electricity'

function qs(params: Record<string, string | undefined>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v) p.set(k, v)
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

// ---- readings ----

export function listReadings(cursor?: string): Promise<ElPage<ElReading>> {
  return apiFetch(`${base}/readings${qs({ cursor })}`)
}
export function createReading(body: ElReadingInput): Promise<ElReading> {
  return apiFetch(`${base}/readings`, { method: 'POST', body })
}
export function updateReading(id: string, body: ElReadingInput): Promise<ElReading> {
  return apiFetch(`${base}/readings/${id}`, { method: 'PATCH', body })
}
export function deleteReading(id: string): Promise<void> {
  return apiFetch(`${base}/readings/${id}`, { method: 'DELETE' })
}

// ---- tariffs ----

export function listTariffs(): Promise<ElPage<ElTariff>> {
  return apiFetch(`${base}/tariffs`)
}
export function createTariff(body: ElTariffInput): Promise<ElTariff> {
  return apiFetch(`${base}/tariffs`, { method: 'POST', body })
}
export function updateTariff(id: string, body: ElTariffInput): Promise<ElTariff> {
  return apiFetch(`${base}/tariffs/${id}`, { method: 'PATCH', body })
}
/** 409 when the version is the only one effective at a period's first day
 *  (D160). Deleting a MIDDLE version is allowed and legitimately reprices its
 *  days — the audit event records it. */
export function deleteTariff(id: string): Promise<void> {
  return apiFetch(`${base}/tariffs/${id}`, { method: 'DELETE' })
}

// ---- advances + payments ----

export function listAdvances(): Promise<ElPage<ElAdvance>> {
  return apiFetch(`${base}/advances`)
}
export function createAdvance(body: ElAdvanceInput): Promise<ElAdvance> {
  return apiFetch(`${base}/advances`, { method: 'POST', body })
}
export function updateAdvance(id: string, body: ElAdvanceInput): Promise<ElAdvance> {
  return apiFetch(`${base}/advances/${id}`, { method: 'PATCH', body })
}
export function deleteAdvance(id: string): Promise<void> {
  return apiFetch(`${base}/advances/${id}`, { method: 'DELETE' })
}

export function listPayments(): Promise<ElPage<ElPayment>> {
  return apiFetch(`${base}/payments`)
}
export function createPayment(body: ElPaymentInput): Promise<ElPayment> {
  return apiFetch(`${base}/payments`, { method: 'POST', body })
}
export function updatePayment(id: string, body: ElPaymentInput): Promise<ElPayment> {
  return apiFetch(`${base}/payments/${id}`, { method: 'PATCH', body })
}
export function deletePayment(id: string): Promise<void> {
  return apiFetch(`${base}/payments/${id}`, { method: 'DELETE' })
}

// ---- periods ----

export function listPeriods(): Promise<ElPage<ElPeriod>> {
  return apiFetch(`${base}/periods`)
}
export function createPeriod(body: ElPeriodInput): Promise<ElPeriod> {
  return apiFetch(`${base}/periods`, { method: 'POST', body })
}
export function updatePeriod(id: string, body: ElPeriodInput): Promise<ElPeriod> {
  return apiFetch(`${base}/periods/${id}`, { method: 'PATCH', body })
}
export function deletePeriod(id: string): Promise<void> {
  return apiFetch(`${base}/periods/${id}`, { method: 'DELETE' })
}

// ---- computed (all three are projections of ONE loaded snapshot) ----

/** 404 when there is no period at all — the honest first-run answer is "create
 *  one", not an empty dashboard. */
export function getSummary(periodId?: string): Promise<ElSummary> {
  return apiFetch(`${base}/summary${qs({ period_id: periodId })}`)
}
export function getIntervals(periodId?: string): Promise<ElIntervalList> {
  return apiFetch(`${base}/intervals${qs({ period_id: periodId })}`)
}
export function getHistory(from?: string, to?: string): Promise<ElHistory> {
  return apiFetch(`${base}/history${qs({ from, to })}`)
}
