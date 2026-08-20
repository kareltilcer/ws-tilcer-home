// TanStack Query key factory (PRD §7). Centralized so invalidation is consistent.

export const qk = {
  dashboard: ['dashboard'] as const,
  boards: ['boards'] as const,
  boardTree: (id: string, filters?: unknown) => ['board', id, 'tree', filters ?? {}] as const,
  card: (id: string) => ['card', id] as const,
  boardLabels: (id: string) => ['board', id, 'labels'] as const,
  events: (window?: unknown) => ['events', window ?? {}] as const,
  event: (id: string) => ['event', id] as const,
  logs: (filters?: unknown) => ['logs', filters ?? {}] as const,
  logsEntity: (type: string, id: string) => ['logs', 'entity', type, id] as const,
  logsStats: (params?: unknown) => ['logs', 'stats', params ?? {}] as const,
  notesTree: ['notes', 'tree'] as const,
  noteDetail: (id: string) => ['notes', 'detail', id] as const,
  noteSearch: (q: string) => ['notes', 'search', q] as const,
  // Documents (v4). The content endpoints (raw/preview/thumbnail) are addressed as
  // URLs by <img>/<iframe>/anchors and deliberately not query-cached.
  documentsTree: ['documents', 'tree'] as const,
  documentDetail: (id: string) => ['documents', 'detail', id] as const,
  documentsSearch: (q: string) => ['documents', 'search', q] as const,
  // Every search result, whatever the query — the prefix to invalidate when a change
  // affects rows the user may be looking at through search rather than the tree.
  documentsSearchAll: ['documents', 'search'] as const,
  documentsResolve: (path: string) => ['documents', 'resolve', path] as const,
  // Widget payloads arrive inside the dashboard response, so there is no per-widget
  // query to key — invalidate `dashboard` (a prefix of everything under it) instead.

  // v5 — push (every member) and the admin notification config (admin only).
  pushVapid: ['push', 'vapid'] as const,
  pushPrefs: ['push', 'prefs'] as const,
  adminRules: ['admin', 'rules'] as const,
  adminSchedules: ['admin', 'schedules'] as const,
  adminCatalog: ['admin', 'catalog'] as const,
  adminDeliveries: (filters?: unknown) => ['admin', 'deliveries', filters ?? {}] as const,

  // v6 — finance. A few dozen rows fetched in one page, so there is no per-month
  // query: the list IS the cache, and a mutation invalidates it plus `dashboard`
  // (which carries the Rozpočet widget's payload).
  financeMonths: ['finance', 'months'] as const,

  // v7 — garden. Every key sits under the one `garden` prefix so a websocket
  // push can invalidate the whole module in one call, and so the three caches a
  // planting change affects — the plan, its CHECK and the tasks — always move
  // together. A stale check is worse than no check.
  gardenBeds: (includeInactive?: boolean) => ['garden', 'beds', includeInactive ?? false] as const,
  gardenBedHistory: (id: string) => ['garden', 'beds', id, 'history'] as const,
  gardenSeasons: ['garden', 'seasons'] as const,
  gardenSeason: (year: number) => ['garden', 'season', year] as const,
  gardenCheck: (year: number) => ['garden', 'check', year] as const,
  gardenPlantings: (filters?: unknown) => ['garden', 'plantings', filters ?? {}] as const,
  gardenPlanting: (id: string) => ['garden', 'planting', id] as const,
  gardenPlants: (filters?: unknown) => ['garden', 'plants', filters ?? {}] as const,
  gardenPlant: (id: string) => ['garden', 'plant', id] as const,
  gardenVarieties: (plantId: string) => ['garden', 'varieties', plantId] as const,
  gardenTasks: (filters?: unknown) => ['garden', 'tasks', filters ?? {}] as const,
  gardenHarvests: (filters?: unknown) => ['garden', 'harvests', filters ?? {}] as const,
  gardenStorage: (status?: string) => ['garden', 'storage', status ?? ''] as const,
  gardenRules: (scope?: string) => ['garden', 'rules', scope ?? ''] as const,
  gardenSettings: ['garden', 'settings'] as const,
  gardenWeather: ['garden', 'weather'] as const,
  gardenEnums: ['garden', 'enums'] as const,
  /** The prefix a mutation invalidates: everything above hangs off it. */
  gardenAll: ['garden'] as const,
}
