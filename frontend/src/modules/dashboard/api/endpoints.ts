// Nástěnka / dashboard — the widget HOST's API surface (openapi.yaml).
//
// It owns no feature data: these four calls read the layout and the catalog, and
// resolve one widget instance. What a widget RENDERS comes from the module that
// contributed it, through the payload types in src/api/types.ts.

import { apiFetch } from '@/api/client'
import type { Dashboard, LayoutItem, LayoutItemInput, WidgetCatalogEntry, WidgetInstance } from '@/api/types'

// ---- Dashboard host (widget host) ----
export const getDashboard = () => apiFetch<Dashboard>('/api/dashboard')

export const getDashboardCatalog = () => apiFetch<WidgetCatalogEntry[]>('/api/dashboard/catalog')

export const saveDashboardLayout = (items: LayoutItemInput[]) =>
  apiFetch<LayoutItem[]>('/api/dashboard/layout', { method: 'PUT', body: items })

export const getWidget = (key: string) =>
  apiFetch<WidgetInstance>(`/api/dashboard/widgets/${encodeURIComponent(key)}`)
