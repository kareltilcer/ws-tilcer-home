import type { ReactNode } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useIsDesktop } from '@/hooks/useMediaQuery'

// ResponsiveModal renders a centered Dialog on desktop and a full-height bottom
// Sheet on mobile — the same behaviour the card and event details reuse
// (design brief D). Both close on overlay click / Esc.
export function ResponsiveModal({
  open,
  onOpenChange,
  title,
  children,
  footer,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  children: ReactNode
  footer?: ReactNode
}) {
  const desktop = useIsDesktop()
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50 data-[state=open]:animate-[om-fadein_150ms]" />
        <Dialog.Content
          className={cn(
            'fixed z-50 flex flex-col bg-s1 text-fg shadow-[var(--shadow)] focus:outline-none',
            desktop
              ? 'left-1/2 top-1/2 max-h-[85vh] w-[min(640px,92vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border'
              : 'inset-x-0 bottom-0 max-h-[92vh] rounded-t-lg border-t border-border',
          )}
        >
          <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-3.5">
            <Dialog.Title className="min-w-0 flex-1 truncate text-base font-bold">{title}</Dialog.Title>
            <Dialog.Close
              className="grid h-8 w-8 flex-none place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
              aria-label="Zavřít"
            >
              <X size={18} aria-hidden />
            </Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4 om-scroll">{children}</div>
          {footer && <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">{footer}</div>}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
