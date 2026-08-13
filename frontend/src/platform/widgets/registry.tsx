import type { ComponentType } from 'react'
import { PravedelamWidget } from '@/modules/todo/widgets/PravedelamWidget'
import { PripominkyWidget } from '@/modules/events/widgets/PripominkyWidget'
import { TentoMesicWidget } from '@/modules/events/widgets/TentoMesicWidget'
import { PripnuteWidget } from '@/modules/notes/widgets/PripnuteWidget'
import { PripnuteDokumentyWidget } from '@/modules/documents/widgets/PripnuteDokumentyWidget'
import type { WidgetComponentProps } from './shared'

export type { WidgetComponentProps } from './shared'

// widgetRegistry maps a widget key to the component that renders its payload —
// the frontend mirror of the backend widget catalog. Modules contribute their
// components here by key; the dashboard host looks a component up per rendered
// widget and hardcodes none (FR-M2 / FR-D). An unknown key renders nothing.
export const widgetRegistry: Record<string, ComponentType<WidgetComponentProps>> = {
  'todo.pravedelam': PravedelamWidget,
  'events.pripominky': PripominkyWidget,
  'events.tento-mesic': TentoMesicWidget,
  'notes.pripnute': PripnuteWidget,
  'documents.pripnute': PripnuteDokumentyWidget,
}
