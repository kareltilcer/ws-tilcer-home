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
}
