// Finance (v6) — the module's API surface (openapi.yaml).

import { apiFetch } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  FinanceMonth,
  FinanceMonthInput,
  FinanceMonthPage,
} from './types'

//
// Reads are open to every member, `reader` included (D84); writes and the
// PERMANENT delete take the ordinary editor/admin gate server-side.
export const listFinanceMonths = (params: { limit?: number; cursor?: string } = {}) =>
  apiFetch<FinanceMonthPage>(`/api/finance/months${qs(params)}`)

export const getFinanceMonth = (id: string) => apiFetch<FinanceMonth>(`/api/finance/months/${id}`)

export const createFinanceMonth = (body: FinanceMonthInput) =>
  apiFetch<FinanceMonth>('/api/finance/months', { method: 'POST', body })

export const updateFinanceMonth = (id: string, body: Partial<FinanceMonthInput>) =>
  apiFetch<FinanceMonth>(`/api/finance/months/${id}`, { method: 'PATCH', body })

/** Removes the row outright — there is no archive and no restore (D87). */
export const deleteFinanceMonth = (id: string) =>
  apiFetch<void>(`/api/finance/months/${id}`, { method: 'DELETE' })
