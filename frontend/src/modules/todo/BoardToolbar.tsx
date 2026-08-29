import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { MoreHorizontal, Plus, Tag, Pencil, Trash2 } from 'lucide-react'
import type { Board } from '@/api/types'
import { cn } from '@/lib/utils'
import type { SortMode } from './ordering'

// BoardToolbar — the board's top control row (v1): board switcher, create board,
// board options menu (rename · labels · delete), read-only badge, sort toggle,
// and add-column. Purely presentational; the board owns the state and mutations.
export function BoardToolbar({
  boards,
  activeBoardId,
  canWrite,
  isReader,
  sortMode,
  onSelectBoard,
  onCreateBoard,
  onRenameBoard,
  onManageLabels,
  onDeleteBoard,
  onSetSort,
  onAddColumn,
}: {
  boards: Board[]
  activeBoardId: string | null
  canWrite: boolean
  isReader: boolean
  sortMode: SortMode
  onSelectBoard: (id: string) => void
  onCreateBoard: () => void
  onRenameBoard: () => void
  onManageLabels: () => void
  onDeleteBoard: () => void
  onSetSort: (mode: SortMode) => void
  onAddColumn: () => void
}) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      {boards.length > 0 && (
        <div className="flex flex-wrap gap-1 rounded-lg border border-border bg-s2 p-1">
          {boards.map((b) => (
            <button
              key={b.id}
              type="button"
              onClick={() => onSelectBoard(b.id)}
              className={cn(
                'rounded-md px-3 py-1.5 text-[13px] font-semibold transition-colors',
                b.id === activeBoardId ? 'bg-accent text-accent-fg' : 'text-muted hover:text-fg',
              )}
            >
              {b.name}
            </button>
          ))}
        </div>
      )}

      {canWrite && (
        <button
          type="button"
          onClick={onCreateBoard}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-dashed border-border-strong px-3 text-[12.5px] font-bold text-muted hover:border-accent hover:text-fg"
        >
          <Plus size={14} aria-hidden /> Tabule
        </button>
      )}

      {canWrite && activeBoardId && (
        <DropdownMenu.Root>
          <DropdownMenu.Trigger asChild>
            <button
              type="button"
              aria-label="Možnosti nástěnky"
              className="grid h-8 w-8 place-items-center rounded-lg border border-border bg-s2 text-muted hover:text-fg"
            >
              <MoreHorizontal size={16} aria-hidden />
            </button>
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              align="start"
              sideOffset={4}
              className="z-50 w-52 rounded-lg border border-border-strong bg-s1 p-1 shadow-[var(--shadow)]"
            >
              <MenuItem onSelect={onRenameBoard} icon={<Pencil size={14} aria-hidden />}>
                Přejmenovat tabuli
              </MenuItem>
              <MenuItem onSelect={onManageLabels} icon={<Tag size={14} aria-hidden />}>
                Spravovat štítky
              </MenuItem>
              <MenuItem onSelect={onDeleteBoard} icon={<Trash2 size={14} aria-hidden />} danger>
                Smazat tabuli
              </MenuItem>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      )}

      {isReader && (
        <span className="inline-flex h-7 items-center gap-1.5 rounded-full border border-border bg-s3 px-3 text-[12px] font-semibold text-muted">
          ◔ Jen pro čtení
        </span>
      )}

      <div className="flex-1" />

      {activeBoardId && (
        <div className="flex gap-1 rounded-lg border border-border bg-s2 p-1 text-[12px]">
          <SortBtn active={sortMode === 'manual'} onClick={() => onSetSort('manual')}>
            Ruční pořadí
          </SortBtn>
          <SortBtn active={sortMode === 'priority'} onClick={() => onSetSort('priority')}>
            Dle priority
          </SortBtn>
        </div>
      )}

      {canWrite && activeBoardId && (
        <button
          type="button"
          onClick={onAddColumn}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-accent-soft px-3 text-[12.5px] font-bold text-accent hover:bg-accent hover:text-accent-fg"
        >
          <Plus size={14} aria-hidden /> Nový sloupec
        </button>
      )}
    </div>
  )
}

function SortBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn('rounded-md px-2.5 py-1 font-semibold transition-colors', active ? 'bg-accent text-accent-fg' : 'text-muted hover:text-fg')}
    >
      {children}
    </button>
  )
}

function MenuItem({
  onSelect,
  icon,
  danger,
  children,
}: {
  onSelect: () => void
  icon: React.ReactNode
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <DropdownMenu.Item
      onSelect={onSelect}
      className={cn(
        'flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-[13px] outline-none',
        danger ? 'text-danger data-[highlighted]:bg-danger/10' : 'text-fg data-[highlighted]:bg-s3',
      )}
    >
      <span className="text-subtle">{icon}</span>
      {children}
    </DropdownMenu.Item>
  )
}
