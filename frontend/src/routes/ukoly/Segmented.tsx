import { cn } from '@/lib/utils'

// Segmented — a single-choice pill control shared by the column create/edit UIs.
export function Segmented<T extends string | number>({ options, value, onPick }: { options: [T, string][]; value: T; onPick: (v: T) => void }) {
  return (
    <div className="flex gap-1 rounded-lg border border-border bg-s2 p-1">
      {options.map(([v, label]) => (
        <button
          key={String(v)}
          type="button"
          onClick={() => onPick(v)}
          className={cn(
            'h-8 flex-1 rounded-md text-[12px] font-semibold transition-colors',
            v === value ? 'bg-accent text-accent-fg' : 'text-muted hover:text-fg',
          )}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
