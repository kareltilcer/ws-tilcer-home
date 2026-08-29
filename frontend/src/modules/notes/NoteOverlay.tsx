import * as Dialog from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { NoteView } from './NoteView'

// NoteOverlay opens a pinned note in an overlay dialog ON Nástěnka — the user
// never navigates away to Poznámky (the explicit FR-P8 requirement). It reuses
// the standalone NoteView edge-to-edge (full height, no extra padding) and marks
// edits/unpin from inside it with via="dashboard" for cross-module attribution.
export function NoteOverlay({
  noteId,
  readOnly,
  onClose,
}: {
  noteId: string
  readOnly?: boolean
  onClose: () => void
}) {
  const desktop = useIsDesktop()
  return (
    <Dialog.Root open onOpenChange={(o) => !o && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content
          className={cn(
            'fixed z-50 flex flex-col overflow-hidden bg-s1 text-fg shadow-[var(--shadow)] focus:outline-none',
            desktop
              ? 'left-1/2 top-1/2 h-[80vh] w-[min(680px,94vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border'
              : 'inset-x-0 bottom-0 top-10 rounded-t-lg border-t border-border',
          )}
        >
          <div className="flex flex-none items-center justify-between gap-3 border-b border-border px-4 py-2">
            <Dialog.Title className="font-mono text-[10.5px] uppercase tracking-wide text-subtle">
              Poznámka · z Nástěnky
            </Dialog.Title>
            <Dialog.Close
              className="grid h-8 w-8 place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
              aria-label="Zavřít"
            >
              <X size={16} aria-hidden />
            </Dialog.Close>
          </div>
          <NoteView key={noteId} noteId={noteId} readOnly={readOnly} via="dashboard" />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
