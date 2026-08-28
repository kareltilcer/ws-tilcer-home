import type { ReactNode } from 'react'
import { cs } from '@/i18n/cs'
import { Button, Spinner } from '@/components/ui/ui'
import type { DirectoryEntry } from './api/types'

/**
 * The add-member picker's body: the directory's three states, then a grid of one
 * chip per addable member.
 *
 * ⚠ ONE COPY, BECAUSE THERE ARE TWO PICKERS (v10 review). The members panel adds
 * somebody to a room that exists; the create dialog picks the founding members of one
 * that does not. The chip differs — a single-shot add against a toggle — and nothing
 * else did, so the loading state, the empty note, the 44 px touch target and the
 * missing failure branch were all written twice and had to be fixed twice. The chip
 * is the parameter; the states are not.
 *
 * ⚠ A DIRECTORY THAT FAILED TO LOAD IS NOT AN EMPTY ONE — the same distinction
 * ThreadView draws for a thread, and the same reason. Both pickers fell through a
 * failed fetch into `directoryEmpty`, "Zatím se nikdo další nepřihlásil.", which is a
 * claim about the household made on the strength of a 500: it tells the member nobody
 * has ever logged in, and gives them nothing to press. The retry is half the fix.
 */
export function DirectoryPicker({
  directory,
  addable,
  label,
  labelledBy,
  renderChip,
}: {
  /** The directory query. Only the state is used, so the caller keeps the hook. */
  directory: { isPending: boolean; isError: boolean; isFetching: boolean; refetch: () => unknown }
  /** Who this particular picker may offer — the caller decides who is excluded. */
  addable: DirectoryEntry[]
  /** Names the group for a screen reader. Ignored when `labelledBy` is given. */
  label: string
  /** Id of the picker's own visible heading, where it has one. */
  labelledBy?: string
  renderChip: (entry: DirectoryEntry) => ReactNode
}) {
  if (directory.isPending) return <Spinner />

  if (directory.isError) {
    return (
      <div>
        <p className="text-sm text-muted text-pretty">{cs.chat.directoryLoadFailed}</p>
        <Button
          size="sm"
          variant="secondary"
          className="mt-2"
          loading={directory.isFetching}
          onClick={() => void directory.refetch()}
        >
          {cs.chat.retry}
        </Button>
      </div>
    )
  }

  if (addable.length === 0) {
    return <p className="text-sm text-muted text-pretty">{cs.chat.directoryEmpty}</p>
  }

  return (
    // ⚠ role="group" AND A NAME, because `aria-pressed` on each chip says whether it
    // is on and never what it is for — the heading beside the buttons is a heading to
    // the eye and nothing to a screen reader until something points at it.
    <div
      role="group"
      aria-label={labelledBy ? undefined : label}
      aria-labelledby={labelledBy}
      className="flex flex-wrap gap-2"
    >
      {addable.map(renderChip)}
    </div>
  )
}
