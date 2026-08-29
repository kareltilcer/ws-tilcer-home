// Per-device Web Push (v5) — the half of push that belongs to EVERY member,
// reader included: the VAPID key, this device's subscription, the per-category
// preferences and the "does this device actually work?" test.
//
// ⚠ NOT THE ADMIN HALF. Broadcasts, trigger rules and scheduled summaries are
// admin-only and live in src/modules/admin/api/endpoints.ts. The split is the
// same one the backend draws: platform/push carries the transport, and the admin
// module decides who gets sent what.
//
// It stays in src/api/ because its consumers are platform/push/usePush.ts and
// platform/settings/NastaveniPage.tsx — no module owns it.

import { apiFetch } from './client'
import type {
  PushCategories,
  PushPreferences,
  PushSubscriptionInfo,
  PushTestResult,
} from './types'

export const getVapidKey = () => apiFetch<{ key: string }>('/api/push/vapid-key')

export const registerPushSubscription = (body: {
  endpoint: string
  keys: { p256dh: string; auth: string }
  user_agent?: string
}) => apiFetch<PushSubscriptionInfo>('/api/push/subscriptions', { method: 'POST', body })

export const removePushSubscription = (endpoint: string) =>
  apiFetch<void>('/api/push/subscriptions', { method: 'DELETE', body: { endpoint } })

/** sendPushTest pushes to the CALLER's own devices only, bypassing their mutes —
 *  the "does this device actually work?" answer the settings panel promises. */
export const sendPushTest = () =>
  apiFetch<PushTestResult>('/api/push/test', { method: 'POST' })

export const getPushPreferences = () => apiFetch<PushPreferences>('/api/push/preferences')

export const updatePushPreferences = (body: {
  enabled?: boolean
  categories?: Partial<PushCategories>
}) => apiFetch<PushPreferences>('/api/push/preferences', { method: 'PATCH', body })
