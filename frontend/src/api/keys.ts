import type { Scope } from './types'

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
  // ⚠ v9: THE SCOPE IS PART OF THE KEY, on every notes/documents read.
  //
  // This is the single most likely bug in the whole version, and the reason is
  // that the wrong version still works: two scopes sharing one cache key look
  // fine until the moment somebody switches roots and TanStack serves the other
  // tree from cache. The persisted cache then writes it to disk, where it
  // outlives the session — a private title cached under a key that says nothing
  // about privacy.
  //
  // A scope-less key would also survive review, because nothing about it reads as
  // wrong. Hence: no notes/documents key below takes fewer arguments than it did
  // before v9, and the compiler enforces it.
  notesTree: (scope: Scope) => ['notes', 'tree', scope] as const,
  noteDetail: (id: string) => ['notes', 'detail', id] as const,
  noteSearch: (q: string, scope: Scope) => ['notes', 'search', q, scope] as const,
  // No `notesResolve`/`documentsResolve` key: slug-path resolution is a bare
  // api.resolveNotePath call inside an effect, not a query, so a key for it would
  // describe a cache that does not exist — and invite somebody to reason about
  // invalidating one.
  /** The prefix a notes mutation invalidates: both scopes hang off it. */
  notesAll: ['notes'] as const,
  // Documents (v4). The content endpoints (raw/preview/thumbnail) are addressed as
  // URLs by <img>/<iframe>/anchors and deliberately not query-cached.
  documentsTree: (scope: Scope) => ['documents', 'tree', scope] as const,
  documentDetail: (id: string) => ['documents', 'detail', id] as const,
  documentsSearch: (q: string, scope: Scope) => ['documents', 'search', q, scope] as const,
  // No `documentsSearchAll`: it existed because the old `documentsTree` key was a
  // SIBLING of the search key, so refreshing the tree left search results stale.
  // documentsAll below is a prefix of both, so one invalidation now covers them.
  /** The prefix a documents mutation invalidates: both scopes hang off it. */
  documentsAll: ['documents'] as const,
  // Widget payloads arrive inside the dashboard response, so there is no per-widget
  // query to key — invalidate `dashboard` (a prefix of everything under it) instead.

  // v5 — push (every member) and the admin notification config (admin only).
  pushVapid: ['push', 'vapid'] as const,
  pushPrefs: ['push', 'prefs'] as const,
  adminRules: ['admin', 'rules'] as const,
  adminSchedules: ['admin', 'schedules'] as const,
  adminCatalog: ['admin', 'catalog'] as const,
  adminDeliveries: (filters?: unknown) => ['admin', 'deliveries', filters ?? {}] as const,
  // v9 — Administrace's two storage screens. The snapshot is a PROJECTION with a
  // 60-second server-side cache, so the client keeps no long staleTime of its own:
  // two caches over one number is how a page ends up disagreeing with itself.
  adminStorage: ['admin', 'storage'] as const,
  adminPrivateItems: (filters?: unknown) => ['admin', 'private-items', filters ?? {}] as const,
  /** The prefix a purge invalidates: every filter combination hangs off it. */
  adminPrivateItemsAll: ['admin', 'private-items'] as const,

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

  // v8 — Elektřina. Everything sits under the one `electricity` prefix, and every
  // mutation invalidates ALL of it rather than the collection it touched.
  //
  // That is deliberate and not laziness: the summary, the intervals and the
  // history are three views of ONE computation, so any write to any of the five
  // entities can move all three. A fresh reading list beside a stale summary is
  // worse than a spinner — it is a number that has quietly stopped being true,
  // and nothing on the screen would say so.
  electricityReadings: (cursor?: string) => ['electricity', 'readings', cursor ?? ''] as const,
  electricityTariffs: ['electricity', 'tariffs'] as const,
  electricityAdvances: ['electricity', 'advances'] as const,
  electricityPayments: ['electricity', 'payments'] as const,
  electricityPeriods: ['electricity', 'periods'] as const,
  electricitySummary: (periodId?: string) => ['electricity', 'summary', periodId ?? ''] as const,
  electricityIntervals: (periodId?: string) => ['electricity', 'intervals', periodId ?? ''] as const,
  electricityHistory: (from?: string, to?: string) =>
    ['electricity', 'history', from ?? '', to ?? ''] as const,
  /** The prefix every electricity mutation invalidates. */
  electricityAll: ['electricity'] as const,

  // v10 — Chat.
  //
  // ⚠ EVERY KEY BELOW THAT DESCRIBES CONVERSATION CONTENT CARRIES THE CONVERSATION
  // ID AS ITS OWN SEGMENT. This is the v9 `scope` lesson in a module where the
  // payload IS the content: a key shared across two conversations looks fine until
  // the moment somebody switches rooms and TanStack serves the other thread from
  // cache — which here means other people's messages under a heading that names
  // yours. The compiler is what enforces it: none of these functions is callable
  // without the id.
  //
  // ⚠ AND NONE OF IT IS PERSISTED. Chat is excluded from the PWA persister
  // entirely (platform/pwa/persist.ts) — the route renders an offline state rather
  // than a stale thread, because message bodies and other members' names on a
  // shared laptop's disk are worth less than the offline convenience.
  // ⚠ THE RESOURCE COMES BEFORE THE ID, AND THAT IS NOT COSMETIC. The obvious
  // nesting — ['chat','conversation',id,'messages'] — makes chatConversation(id) a
  // PREFIX of chatMessages(id), and TanStack invalidates by prefix: advancing the
  // read marker would then refetch the entire thread, on every message, in every
  // open tab. It was found by counting requests in the browser, and it is invisible
  // in code review because both keys look correct on their own.
  chatConversations: (state?: string) => ['chat', 'conversations', state ?? 'active'] as const,
  chatConversation: (id: string) => ['chat', 'conversation', id] as const,
  chatMessages: (id: string) => ['chat', 'messages', id] as const,
  chatMembers: (id: string) => ['chat', 'members', id] as const,
  chatSearch: (q: string, conversationID?: string) =>
    ['chat', 'search', q, conversationID ?? ''] as const,
  chatDirectory: ['chat', 'directory'] as const,
  /**
   * ⚠ NOT AN INVALIDATION TARGET, AND NO CHAT MUTATION MAY USE IT. It is the
   * common prefix the key-shape test asserts every chat key hangs off, and the
   * prefix persist.ts refuses to write to disk — nothing more.
   *
   * Invalidating `['chat']` refetches every open thread — each at `limit = held`,
   * so every page of history the member loaded is re-fetched — plus the directory
   * and every cached search result the session has accumulated. That was the
   * default once; `invalidateLists` in modules/chat/api/hooks.ts is what replaced
   * it, and it names the two listings a room's own state can actually change.
   */
  chatAll: ['chat'] as const,
}
