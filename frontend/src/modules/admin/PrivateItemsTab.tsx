import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Eye } from 'lucide-react'
import { qk } from '@/api/keys'
import { deleteDocument, deleteDocumentFolder, deleteNote, deleteFolder, listPrivateItems } from '@/api/endpoints'
import type { PrivateItem } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { fmtDateISO, fmtMeasuredBytes, fmtStorageBytes } from '@/i18n/format'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'

/**
 * Administrace → Soukromé položky — the purge screen (v9, D198, D212, D215).
 *
 * ⚠ THIS IS THE MOST DELICATE SCREEN IN v9, and it is uncomfortably close to being
 * the private-file browser the whole feature exists to prevent. That discomfort is
 * the DESIGN CONSTRAINT, not an objection to it.
 *
 * It exists because "an admin may hard-delete a foreign private item" (D181) is
 * useless if nothing in the app can name the thing to delete. So it names it —
 * id, owner, kind, size, dates — and nothing else. No title, no filename, no
 * content type, no thumbnail, no preview, no download, and NO SEARCH BOX.
 *
 * Everything below is chosen to keep it feeling like a MAINTENANCE TOOL rather
 * than a file manager: no grid view, no preview column, sorting only by size and
 * recency, purge confirmed by TYPING THE FULL IDENTIFIER, and a visible note that
 * opening the list is itself recorded. If browsing it ever starts to feel
 * pleasant, the design has gone wrong.
 *
 * ⚠ The tab is present WHETHER OR NOT anything is listed (D215): hiding it would
 * hide the SCREEN, not the CAPABILITY — an admin can permanently delete another
 * member's private item either way — and a power that exists but is invisible is
 * worse than one that is stated.
 */
export function PrivateItemsTab() {
  const qc = useQueryClient()
  const [sort, setSort] = useState<'recent' | 'size'>('recent')
  const [module, setModule] = useState<'' | 'notes' | 'documents'>('')
  const [purge, setPurge] = useState<PrivateItem | null>(null)

  const filters = { sort, module: module || undefined }
  // ⚠ PAGED, because the screen exists to ACT on the list. The server caps a page
  // at 50; without following `next_cursor` an admin simply could not reach item 51,
  // and in the default `recent` view nothing would even say the list was short —
  // on the one screen that reclaims space and removes a departed member's files.
  //
  // Each page costs one `admin.private_items.view` event, which is right: paging is
  // a person choosing to look further. What must not happen is a load nobody asked
  // for, hence no refetchInterval, no prefetch, and NONE of TanStack's automatic
  // refetches — that would fill the audit record with noise saying nothing about
  // who actually looked.
  //
  // ⚠ SUPPRESSING refetchOnWindowFocus ALONE WAS NOT ENOUGH, and the two that were
  // left at their defaults are the ones that fire most. AdministracePage renders
  // its tabs as `{tab === 'private' && <PrivateItemsTab />}`, so every hop to
  // Úložiště and back UNMOUNTS and REMOUNTS this component — and with staleTime 0
  // (the default) `refetchOnMount` replays EVERY loaded page's cursor. An admin who
  // paged three deep, glanced at the other tab and came back wrote three more
  // `admin.private_items.view` events without asking to look at anything;
  // `refetchOnReconnect` did the same on any network blip. That is the exact
  // failure the purge handler below refuses to cause, arriving through the door
  // nobody watched — and it corrupts the one audit record this screen exists to
  // produce.
  //
  // staleTime Infinity is what makes "a page load is a person choosing to look"
  // true: the data is refetched only by an explicit fetchNextPage, or by the
  // resetQueries a purge triggers.
  const q = useInfiniteQuery({
    queryKey: qk.adminPrivateItems(filters),
    queryFn: ({ pageParam }) => listPrivateItems({ ...filters, cursor: pageParam as string | undefined }),
    initialPageParam: undefined as string | undefined,
    // `size` is single-page by design (a keyset cursor is an id, and an id does not
    // locate a position in a size ordering), so it never advances — the note below
    // the toolbar says so rather than offering a button that does nothing.
    getNextPageParam: (last) => (sort === 'size' ? undefined : (last.next_cursor ?? undefined)),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
    refetchOnReconnect: false,
  })
  // TotalBytes covers every matching item, not just the pages loaded, so it comes
  // off any page and stays the complete figure the screen acts on.
  const items = q.data?.pages.flatMap((p) => p.items) ?? []
  const totalBytes = q.data?.pages[0]?.total_bytes ?? null

  const purgeMut = useMutation({
    mutationFn: (it: PrivateItem) => {
      // ⚠ DELETION GOES THROUGH THE OWNING MODULE'S ROUTE (D198) — `admin` has no
      // delete path of its own, so the audit action stays the module's. The folder
      // routes carry cascade=true because that is what actually reclaims a private
      // subtree (D212).
      switch (it.kind) {
        case 'document':
          return deleteDocument(it.id, true)
        case 'document_folder':
          return deleteDocumentFolder(it.id, { hard: true, cascade: true })
        case 'note':
          return deleteNote(it.id, true)
        case 'note_folder':
          return deleteFolder(it.id, { hard: true, cascade: true })
        default:
          // note_image has no delete route and should not: an image belongs to its
          // note and goes when the note does (D204/D212). Unreachable — the row
          // renders no purge control.
          throw new Error('note_image is not deletable')
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.adminStorage })
      // Through the factory, not a hand-written array: keys.ts is the one place a
      // prefix is defined, and a literal here would silently stop matching the day
      // it changes — leaving a purged row on screen until a manual refetch.
      //
      // ⚠ RESET, not invalidate: invalidating an infinite query replays every
      // loaded page's stored cursor — N requests and N `admin.private_items.view`
      // events for one purge, exactly the "load nobody asked for" the header
      // comment forbids, against cursors the deletion just shifted. Resetting
      // refetches page one only; paging further stays a person choosing to look.
      void qc.resetQueries({ queryKey: qk.adminPrivateItemsAll })
      // ⚠ AND THE OWNING MODULE'S OWN CACHES. The listing is NOT filtered to
      // foreign items — an admin's own private notes and documents appear in it —
      // so a purge here can delete a row that is sitting in this very session's
      // notes/documents tree and on Nástěnka as a personal pin. Without these, the
      // admin walks to /poznamky/soukrome and finds the note still listed, opening
      // it to a 404. Every other delete path in the app invalidates the module
      // prefix plus `dashboard` (see PoznamkyPage's publishMut); this one now does
      // too. Both prefixes go out regardless of kind: one purge is cheap, and a
      // switch here would be a third place that has to know which kinds belong to
      // which module.
      void qc.invalidateQueries({ queryKey: qk.notesAll })
      void qc.invalidateQueries({ queryKey: qk.documentsAll })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      toast.success(cs.privateItems.purged)
      setPurge(null)
    },
    onError: () => toast.error(cs.privateItems.purgeError),
  })

  return (
    <div className="space-y-4">
      <header>
        <h3 className="text-base font-extrabold tracking-tight">{cs.privateItems.title}</h3>
        <p className="mt-0.5 text-[12.5px] text-muted text-pretty">{cs.privateItems.subtitle}</p>
        {/* Visible, always. "Who looked" is the answer this screen owes the
            household, and saying so up front is part of the deterrent. */}
        <p className="mt-1.5 inline-flex items-center gap-1.5 rounded-lg border border-info bg-info-soft px-2 py-1 text-[11.5px] font-semibold">
          <Eye size={13} aria-hidden />
          {cs.privateItems.audited}
        </p>
      </header>

      <div className="flex flex-wrap items-center gap-1.5">
        <Chip active={module === ''} onClick={() => setModule('')}>
          {cs.privateItems.filterAll}
        </Chip>
        <Chip active={module === 'notes'} onClick={() => setModule('notes')}>
          {cs.notes.title}
        </Chip>
        <Chip active={module === 'documents'} onClick={() => setModule('documents')}>
          {cs.documents.title}
        </Chip>
        <div className="flex-1" />
        {/* Sorting only by size and recency — no name, no type, nothing that would
            make this pleasant to browse. */}
        <Chip active={sort === 'recent'} onClick={() => setSort('recent')}>
          {cs.privateItems.sortRecent}
        </Chip>
        <Chip active={sort === 'size'} onClick={() => setSort('size')}>
          {cs.privateItems.sortSize}
        </Chip>
      </div>

      {q.isLoading ? (
        <div className="grid min-h-[180px] place-items-center">
          <Spinner />
        </div>
      ) : q.isError || !q.data ? (
        <div className="rounded-2xl border border-danger/50 bg-danger/5 p-6 text-center">
          <div className="mb-1.5 font-bold">{cs.electricity.error.loadFailed}</div>
          <Button className="mt-3" onClick={() => void q.refetch()}>
            {cs.common.retry}
          </Button>
        </div>
      ) : items.length === 0 ? (
        <EmptyState />
      ) : (
        <>
          {/* ⚠ Gated on the MODE alone. `!q.hasNextPage` used to be here too, and
              it was dead: getNextPageParam returns undefined for `size`, so
              hasNextPage is never true in this mode and the term always passed.
              With the copy asserting truncation, a complete three-item list was
              told it had been cut short — on the screen whose job is deciding what
              to purge. The note now states the MODE (this sort does not page,
              the total still covers everything), which is true at any length. */}
          {sort === 'size' && (
            <p className="text-[11.5px] text-muted text-pretty">{cs.privateItems.sortSizeTruncated}</p>
          )}
          <div className="overflow-x-auto rounded-2xl border border-border">
            <table className="w-full min-w-[560px] text-left text-[12.5px]">
              <thead className="bg-s2 font-mono text-[10.5px] uppercase tracking-wide text-subtle">
                <tr>
                  <th className="px-3 py-2 font-normal">{cs.privateItems.colId}</th>
                  <th className="px-3 py-2 font-normal">{cs.privateItems.colOwner}</th>
                  <th className="px-3 py-2 font-normal">{cs.privateItems.colKind}</th>
                  <th className="px-3 py-2 text-right font-normal">{cs.privateItems.colSize}</th>
                  <th className="px-3 py-2 font-normal">{cs.privateItems.colCreated}</th>
                  <th className="px-3 py-2" />
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <Row key={it.id} it={it} onPurge={() => setPurge(it)} />
                ))}
              </tbody>
            </table>
          </div>
          {q.hasNextPage && (
            <div className="flex justify-center">
              <Button variant="secondary" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {cs.privateItems.loadMore}
              </Button>
            </div>
          )}
          <div className="flex items-center gap-2 px-1 text-[12px]">
            {/* ⚠ Qualified while more pages exist: the byte total on the right
                covers ALL matching items, so a bare loaded-rows count beside it
                would read as the complete inventory. */}
            <span className="text-muted">
              {q.hasNextPage
                ? `${cs.privateItems.shownCount} ${count(items.length, PLURAL.items)}`
                : count(items.length, PLURAL.items)}
            </span>
            <div className="flex-1" />
            <span className="font-semibold">
              {/* Null renders as *nezměřeno*, never as `0 B` (D193): a zero on a
                  screen about reclaiming space reads as good news. */}
              {cs.privateItems.totalBytes} {fmtMeasuredBytes(totalBytes)}
            </span>
          </div>
        </>
      )}

      {purge && (
        <PurgeDialog
          it={purge}
          pending={purgeMut.isPending}
          onConfirm={() => purgeMut.mutate(purge)}
          onClose={() => setPurge(null)}
        />
      )}
    </div>
  )
}

function Row({ it, onPurge }: { it: PrivateItem; onPurge: () => void }) {
  const deletable = it.kind !== 'note_image'
  return (
    <tr className="border-t border-border">
      {/* The id is the only handle this screen offers, so it is mono and whole. */}
      <td className="px-3 py-2 font-mono text-[11px] text-muted">{it.id}</td>
      <td className="px-3 py-2">{it.owner_label ?? it.owner_user_id}</td>
      <td className="px-3 py-2">
        <span className="text-muted">{kindLabel(it.kind)}</span>
        {!deletable && (
          // Says WHY there is no button, rather than leaving a blank cell or
          // offering a control that 405s (D212).
          <span className="mt-0.5 block text-[11px] text-subtle text-pretty">{cs.privateItems.imageNotDeletable}</span>
        )}
      </td>
      <td className="px-3 py-2 text-right font-mono tabular-nums">{fmtStorageBytes(it.byte_size)}</td>
      <td className="px-3 py-2 font-mono text-[11px] text-subtle">{fmtDateISO(it.created_at.slice(0, 10))}</td>
      <td className="px-3 py-2 text-right">
        {deletable && (
          <Button size="sm" variant="danger" onClick={onPurge}>
            {cs.privateItems.purge}
          </Button>
        )}
      </td>
    </tr>
  )
}

/**
 * ⚠ A DESIGNED SCREEN, not a fallback (D215). The tab is present whether or not
 * anything is listed, so this state has a job: say what the tool is for, that it
 * never shows titles, and that opening it is recorded.
 *
 * It is the one place in the app where D181's asymmetry — an admin may delete what
 * they may not read — is explained to the household rather than merely implemented.
 */
function EmptyState() {
  return (
    <div className="rounded-2xl border border-dashed border-border-strong bg-s1 px-6 py-10 text-center">
      <div className="mx-auto max-w-md">
        <div className="mb-1.5 text-base font-bold">{cs.privateItems.emptyTitle}</div>
        <p className="text-[13px] leading-snug text-muted text-pretty">{cs.privateItems.emptyBody}</p>
      </div>
    </div>
  )
}

/**
 * The purge confirmation.
 *
 * ⚠ CONFIRMED BY TYPING THE FULL IDENTIFIER, deliberately inconvenient. This is
 * the one action in Home that destroys something the person doing it has never
 * been allowed to see, and it cannot be undone by anybody — so a single click is
 * the wrong shape for it. `danger` here IS right: unlike publish, this really does
 * destroy data.
 */
function PurgeDialog({
  it,
  pending,
  onConfirm,
  onClose,
}: {
  it: PrivateItem
  pending: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const [typed, setTyped] = useState('')
  const matches = typed.trim() === it.id
  const isFolder = it.kind === 'note_folder' || it.kind === 'document_folder'

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={cs.privateItems.purgeTitle}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.notes.cancel}
          </Button>
          <Button variant="danger" loading={pending} disabled={!matches} onClick={onConfirm}>
            {cs.privateItems.purge}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-sm text-muted text-pretty">{cs.privateItems.purgeBody}</p>
        {isFolder && <p className="text-sm font-semibold text-fg">{cs.privateItems.purgeCascade}</p>}
        <div>
          <label htmlFor="purge-confirm" className="mb-1 block text-[12.5px] font-semibold">
            {cs.privateItems.purgeConfirmPrompt}
          </label>
          <p className="mb-1.5 select-all break-all rounded-lg border border-border bg-s2 px-2 py-1.5 font-mono text-[11.5px]">
            {it.id}
          </p>
          <Input
            id="purge-confirm"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            className="font-mono text-[12px]"
          />
        </div>
      </div>
    </ResponsiveModal>
  )
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'inline-flex min-h-9 items-center rounded-lg border px-3 text-[12.5px] font-semibold',
        active ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-s2 text-muted',
      )}
    >
      {children}
    </button>
  )
}

function kindLabel(kind: PrivateItem['kind']): string {
  switch (kind) {
    case 'note':
      return cs.privateItems.kindNote
    case 'document':
      return cs.privateItems.kindDocument
    case 'note_folder':
      return cs.privateItems.kindNoteFolder
    case 'document_folder':
      return cs.privateItems.kindDocumentFolder
    default:
      return cs.privateItems.kindNoteImage
  }
}
