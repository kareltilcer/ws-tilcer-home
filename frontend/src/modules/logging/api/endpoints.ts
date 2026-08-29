// Log / logging — the module's API surface (openapi.yaml). Admin-only.

import { apiFetch } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  AuditEventDetail,
  AuditEventDetailPage,
  AuditEventPage,
  StatsResponse,
} from './types'

// ---- Logs (admin) ----
export interface LogFilters {
  from?: string
  to?: string
  module?: string
  actor?: string
  action?: string
  entity_type?: string
  entity_id?: string
  level?: string
  q?: string
  limit?: number
  cursor?: string
}

export const listLogs = (f: LogFilters = {}) => apiFetch<AuditEventPage>(`/api/logs${qs(f as Record<string, string>)}`)

export const getLog = (id: string) => apiFetch<AuditEventDetail>(`/api/logs/${id}`)

export const getEntityTimeline = (type: string, id: string, params: { limit?: number; cursor?: string } = {}) =>
  apiFetch<AuditEventDetailPage>(`/api/logs/entity/${type}/${id}${qs(params)}`)

export const getLogStats = (dimension: string, bucket = 'day', from?: string, to?: string) =>
  apiFetch<StatsResponse>(`/api/logs/stats${qs({ dimension, bucket, from, to })}`)
