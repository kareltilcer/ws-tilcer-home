// ⚠⚠ THE ONE SCREEN IN THIS APPLICATION THAT CANNOT BE RE-OPENED (v11, D315).
//
// Every other screen in Home can be visited again. This one cannot: only the
// SHA-256 of the secret is stored, so a member who dismisses this dialog without
// copying has lost the token and must mint another. **That is not a failure state
// to apologise for; it is the feature.** But it means this dialog carries a weight
// nothing else in this app carries, and a dialog that looks like every other
// dialog will be dismissed like every other dialog.
//
// So it is deliberately NOT `ResponsiveModal`, which closes on overlay click and
// on Escape and carries a ✕ in its header. Those three dismissals are muscle
// memory, and muscle memory here costs the token. **Hotovo is the only exit.**
//
// ⚠ AND THE SECRET IS RENDERED EXACTLY ONCE, inside the connect snippet. The bare
// token is NOT also shown above it: two copies of one secret on one screen double
// the chance a copy ends up somewhere it should not, and at 375 px they double the
// height of the thing a member is trying to read. The two buttons are two ways to
// copy one rendering.

import { useRef, useState } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/ui'
import { cs } from '@/i18n/cs'
import { mcpConnectSnippet } from '@/api/mcp'
import type { McpTokenCreated } from '@/api/types'

export function RevealTokenDialog({ token, onDone }: { token: McpTokenCreated; onDone: () => void }) {
  const [result, setResult] = useState('')
  const copyRef = useRef<HTMLButtonElement>(null)
  const snippet = mcpConnectSnippet(token.secret)

  // ⚠ A COPY THAT SILENTLY FAILED IS INDISTINGUISHABLE FROM ONE THAT WORKED until
  // the paste — and here the paste is the only chance there will ever be. So the
  // result is reported either way, and the failure names the fallback.
  const copy = async (text: string, ok: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setResult(ok)
    } catch {
      setResult(cs.mcp.copyFailed)
    }
  }

  return (
    // ⚠ `onOpenChange` IS WIRED THE WAY EVERY OTHER DIALOG IN HOME WIRES IT, and
    // that is what makes the three refusals below LOAD-BEARING rather than
    // decorative. A controlled `open` with no handler would also stay open — and
    // would stay open with the refusals deleted, so nothing could tell the two
    // apart. It was written that way first, and the test that was supposed to
    // prove Escape does not dismiss passed with every guard removed.
    //
    // Nothing reaches this handler now: Escape, pointer-down-outside and
    // interact-outside are each prevented, and no Dialog.Close is rendered. The
    // one exit is the Hotovo button, which calls onDone directly.
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onDone()
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[60] bg-black/60" />
        <Dialog.Content
          aria-describedby={undefined}
          // The three dismissals, refused one by one. Radix fires each of these
          // before it would close; preventing the event is what stops it — and
          // with onOpenChange wired above, deleting any one of these three lines
          // really does hand the member a dialog that throws their token away.
          onEscapeKeyDown={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          // ⚠ FOCUS LANDS ON THE COPY BUTTON, NOT ON *Hotovo*. Radix focuses the
          // first focusable child by default, and here that would put the exit
          // under an Enter press somebody makes on reflex.
          onOpenAutoFocus={(e) => {
            e.preventDefault()
            copyRef.current?.focus()
          }}
          className="fixed left-1/2 top-1/2 z-[61] flex max-h-[88vh] w-[min(600px,94vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-accent bg-s1 text-fg shadow-[var(--shadow)] focus:outline-none"
        >
          <div className="border-b border-border px-5 py-3.5">
            <Dialog.Title className="text-base font-extrabold text-pretty">
              {cs.mcp.revealTitle(token.name)}
            </Dialog.Title>
          </div>

          <div className="om-scroll min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4">
            {/* ⚠ THE WARNING IS ABOVE THE VALUE, NOT BELOW IT. A caution read after
                the thing it cautions about is decoration. */}
            <div className="flex items-center gap-2.5 rounded-lg border border-attention/45 bg-attention/10 px-3 py-2.5">
              <TriangleAlert size={16} className="flex-none text-attention" aria-hidden />
              <span className="text-[13.5px] font-bold text-attention">{cs.mcp.revealWarning}</span>
            </div>

            {/* ⚠ IT SCROLLS INSIDE ITS OWN CONTAINER AND NEVER WRAPS. A 48-character
                monospace string is the worst thing yet asked to fit 375 px, and a
                wrapped secret invites a partial selection — which fails with an
                error that says nothing useful.

                ⚠ AND THE CONTAINER IS FOCUSABLE, WHICH THE AXE SWEEP IS WHAT FOUND.
                A scrollable region with no focusable content cannot be scrolled from
                the keyboard at all (WCAG 2.1.1) — so a keyboard-only member could see
                the first two thirds of a secret they will never be shown again, with
                no way to reach the rest. `tabIndex={0}` is the fix; the label is what
                makes the extra focus stop mean something when it is announced. */}
            <div
              tabIndex={0}
              role="group"
              aria-label={cs.mcp.snippetHint}
              className="om-scroll overflow-x-auto rounded-lg border border-border-strong bg-s2 focus-visible:outline-2 focus-visible:outline-focus"
            >
              <pre className="whitespace-pre px-3.5 py-3 font-mono text-[12.5px] leading-relaxed">{snippet}</pre>
            </div>
            <p className="text-[12.5px] text-muted">{cs.mcp.snippetHint}</p>

            <div className="flex flex-wrap items-center gap-2">
              <Button ref={copyRef} variant="primary" onClick={() => void copy(snippet, cs.mcp.copiedConfig)}>
                {cs.mcp.copyConfig}
              </Button>
              {/* Quieter, for a client that wants the token in an env var. */}
              <Button variant="ghost" onClick={() => void copy(token.secret, cs.mcp.copiedToken)}>
                {cs.mcp.copyToken}
              </Button>
            </div>

            {/* ⚠ ANNOUNCED, NOT ONLY SHOWN. The confirmation is the whole of what a
                screen-reader user gets that a sighted one gets from the button
                flashing, and this is the one dialog where "did that work?" cannot
                be answered by trying again later. */}
            <p aria-live="polite" className="min-h-5 text-[12.5px] font-semibold text-good">
              {result}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-3 border-t border-border px-5 py-3.5">
            <span className="min-w-[180px] flex-1 text-[12px] text-muted text-pretty">{cs.mcp.revealFoot}</span>
            <Button variant="secondary" onClick={onDone}>
              {cs.mcp.done}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
