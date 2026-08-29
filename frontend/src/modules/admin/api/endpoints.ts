// Administrace / admin (v5, v9) — the module's API surface (openapi.yaml):
// Web Push broadcasts, the audit-key trigger rules, the scheduled summaries and
// the two Úložiště screens.
//
// ⚠ The per-device push calls are NOT here — see src/api/push.ts. Those belong to
// every member; these are admin-only.

import { apiFetch } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  Audience,
  NotificationCatalog,
  NotificationDeliveryPage,
  NotificationRule,
  NotificationRulePage,
  NotificationSchedule,
  NotificationSchedulePage,
  PrivateItemPage,
  SendResult,
  StorageSnapshot,
  StorageThresholds,
  StorageThresholdsUpdate,
} from './types'

/**
 * The storage snapshot. `refresh` bypasses the server's 60-second cache (D195).
 *
 * ⚠ A 200 does NOT mean everything was measurable: check `database.bytes_available`
 * and `blobs.available` before rendering a figure, and render null as *nezměřeno*,
 * never as 0.
 */
export const getStorageSnapshot = (refresh = false) =>
  apiFetch<StorageSnapshot>(`/api/admin/storage${qs({ refresh: refresh || undefined })}`)

/**
 * The two chat storage thresholds (v10, D236/D263).
 *
 * ⚠ THERE IS DELIBERATELY NO GET. The current values ride the snapshot above, so a
 * second endpoint would be a second answer to one question — and the two would
 * disagree for exactly as long as one of their caches was warmer than the other.
 * The PUT returns what it saved, which is what the fields render back.
 *
 * ⚠ A VALUE BELOW CURRENT USAGE IS SAVED, NOT REFUSED (D237/D244). Nothing in v10
 * is ever blocked by a threshold — the whole register is warn-only — so the screen
 * says what it just switched on rather than arguing about it.
 */
export const setStorageThresholds = (body: StorageThresholdsUpdate) =>
  apiFetch<StorageThresholds>('/api/admin/storage/thresholds', { method: 'PUT', body })

/**
 * The purge screen's listing (D198).
 *
 * ⚠ OPENING IT IS AUDITED server-side — `admin.private_items.view`, the only read
 * in Home that writes an audit event. The screen says so out loud; calling this
 * function speculatively (a prefetch, a poll) would put noise in that record.
 *
 * ⚠ `sort: 'size'` is SINGLE-PAGE by design: a keyset cursor is an id, and an id
 * does not locate a position in a size ordering. `total_bytes` still covers every
 * matching item, so the figure the screen acts on stays complete.
 */
export const listPrivateItems = (
  params: {
    owner_user_id?: string
    module?: 'notes' | 'documents'
    sort?: 'recent' | 'size'
    limit?: number
    cursor?: string
  } = {},
) => apiFetch<PrivateItemPage>(`/api/admin/storage/private-items${qs(params)}`)

const adminBase = '/api/admin/notifications'

export const getNotificationCatalog = () => apiFetch<NotificationCatalog>(`${adminBase}/catalog`)

export const sendBroadcast = (body: { title: string; body: string; url?: string; audience: Audience }) =>
  apiFetch<SendResult>(`${adminBase}/broadcast`, { method: 'POST', body })

export const listNotificationRules = (params: { enabled?: boolean; limit?: number; cursor?: string } = {}) =>
  apiFetch<NotificationRulePage>(`${adminBase}/rules${qs(params)}`)

export const getNotificationRule = (id: string) => apiFetch<NotificationRule>(`${adminBase}/rules/${id}`)

export const createNotificationRule = (body: Record<string, unknown>) =>
  apiFetch<NotificationRule>(`${adminBase}/rules`, { method: 'POST', body })

export const updateNotificationRule = (id: string, body: Record<string, unknown>) =>
  apiFetch<NotificationRule>(`${adminBase}/rules/${id}`, { method: 'PATCH', body })

export const deleteNotificationRule = (id: string) =>
  apiFetch<void>(`${adminBase}/rules/${id}`, { method: 'DELETE' })

export const testNotificationRule = (id: string) =>
  apiFetch<SendResult>(`${adminBase}/rules/${id}/test`, { method: 'POST' })

export const listNotificationSchedules = (params: { enabled?: boolean; limit?: number; cursor?: string } = {}) =>
  apiFetch<NotificationSchedulePage>(`${adminBase}/schedules${qs(params)}`)

export const getNotificationSchedule = (id: string) => apiFetch<NotificationSchedule>(`${adminBase}/schedules/${id}`)

export const createNotificationSchedule = (body: Record<string, unknown>) =>
  apiFetch<NotificationSchedule>(`${adminBase}/schedules`, { method: 'POST', body })

export const updateNotificationSchedule = (id: string, body: Record<string, unknown>) =>
  apiFetch<NotificationSchedule>(`${adminBase}/schedules/${id}`, { method: 'PATCH', body })

export const deleteNotificationSchedule = (id: string) =>
  apiFetch<void>(`${adminBase}/schedules/${id}`, { method: 'DELETE' })

export const testNotificationSchedule = (id: string) =>
  apiFetch<SendResult>(`${adminBase}/schedules/${id}/test`, { method: 'POST' })

export interface DeliveryFilters {
  kind?: string
  status?: string
  rule_id?: string
  user?: string
  from?: string
  to?: string
  limit?: number
  cursor?: string
}

export const listDeliveries = (f: DeliveryFilters = {}) =>
  apiFetch<NotificationDeliveryPage>(`${adminBase}/deliveries${qs(f as Record<string, string>)}`)
