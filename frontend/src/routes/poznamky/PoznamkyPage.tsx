import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ChevronDown, ChevronRight, FilePlus2, FolderPlus, Search } from 'lucide-react'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
import * as api from '@/api/endpoints'
import type { Folder, FolderDetail, FolderNode, NoteDetail, NotePage, NoteSummary, NotesTree } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { useAuth } from '@/app/auth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { Spinner } from '@/components/ui/ui'
import { DEFAULT_FOLDER_ICON } from '@/components/common/FolderIconPicker'
import { routes } from '@/app/routes'
import { tail } from '@/lib/lexorank'
import { NoteView } from './NoteView'
import { CreateDialog, DeleteDialog, MoveDialog, RenameDialog, type MoveTarget } from './NotesDialogs'

// ---- tree indexing ----

interface TreeIndex {
  folderById: Map<string, Folder>
  noteById: Map<string, NoteSummary> // note id → summary (for the open note's tab title)
  slugPathById: Map<string, string>
  folderNamePathById: Map<string, string> // folder id → human name path ("Práce / Projekty")
  folderIdByNoteId: Map<string, string | null> // note id → containing folder id (null = root)
  childFolders: Map<string | null, FolderNode[]>
  childNotes: Map<string | null, NoteSummary[]>
  notePositions: Map<string | null, string[]>
  folderPositions: Map<string | null, string[]>
  flatFolders: MoveTarget[] // for the move picker, root first
}

function indexTree(tree: NotesTree): TreeIndex {
  const folderById = new Map<string, Folder>()
  const noteById = new Map<string, NoteSummary>()
  const slugPathById = new Map<string, string>()
  const folderNamePathById = new Map<string, string>()
  const folderIdByNoteId = new Map<string, string | null>()
  const childFolders = new Map<string | null, FolderNode[]>()
  const childNotes = new Map<string | null, NoteSummary[]>()
  const flatFolders: MoveTarget[] = [{ id: null, name: cs.notes.root, depth: 0 }]

  const walk = (nodes: FolderNode[], parentPath: string, parentNamePath: string, depth: number) => {
    for (const node of nodes) {
      const f = node.folder
      folderById.set(f.id, f)
      const path = parentPath ? `${parentPath}/${f.slug}` : f.slug
      const namePath = parentNamePath ? `${parentNamePath} / ${f.name}` : f.name
      slugPathById.set(f.id, path)
      folderNamePathById.set(f.id, namePath)
      childFolders.set(f.id, node.subfolders)
      childNotes.set(f.id, node.notes)
      flatFolders.push({ id: f.id, name: f.name, depth, icon: f.icon || DEFAULT_FOLDER_ICON })
      for (const nt of node.notes) {
        noteById.set(nt.id, nt)
        slugPathById.set(nt.id, path ? `${path}/${nt.slug}` : nt.slug)
        folderIdByNoteId.set(nt.id, f.id)
      }
      walk(node.subfolders, path, namePath, depth + 1)
    }
  }
  walk(tree.roots, '', '', 1)
  childFolders.set(null, tree.roots)
  childNotes.set(null, tree.root_notes)
  for (const nt of tree.root_notes) {
    noteById.set(nt.id, nt)
    slugPathById.set(nt.id, nt.slug)
    folderIdByNoteId.set(nt.id, null)
  }

  const notePositions = new Map<string | null, string[]>()
  const folderPositions = new Map<string | null, string[]>()
  for (const [k, v] of childNotes) notePositions.set(k, v.map((n) => n.position))
  for (const [k, v] of childFolders) folderPositions.set(k, v.map((n) => n.folder.position))
  return { folderById, noteById, slugPathById, folderNamePathById, folderIdByNoteId, childFolders, childNotes, notePositions, folderPositions, flatFolders }
}

function tailOf(positions: string[]): string {
  if (positions.length === 0) return tail('')
  const max = positions.reduce((a, b) => (a > b ? a : b))
  return tail(max)
}

function folderCountLabel(node: FolderNode): string {
  const parts: string[] = []
  if (node.subfolders.length) parts.push(count(node.subfolders.length, PLURAL.folders))
  parts.push(count(node.notes.length, PLURAL.notes))
  return parts.join(' · ')
}

// ---- page ----

type Selection = { noteId: string | null; folderId: string | null }
type CreateState = { kind: 'note' | 'folder' } | null

export function PoznamkyPage() {
  const { canWrite } = useAuth()
  const desktop = useIsDesktop()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const splat = useParams()['*'] ?? ''
  const lastPath = useRef('')

  const tree = useQuery({ queryKey: qk.notesTree, queryFn: () => api.getNotesTree() })
  const [sel, setSel] = useState<Selection>({ noteId: null, folderId: null })
  const [searchQ, setSearchQ] = useState('')
  const [create, setCreate] = useState<CreateState>(null)
  const [move, setMove] = useState<{ kind: 'note' | 'folder'; id: string; title: string } | null>(null)
  const [rename, setRename] = useState<{ id: string; name: string } | null>(null)
  const [del, setDel] = useState<
    { kind: 'note'; id: string; title: string } | { kind: 'folder'; id: string; title: string } | null
  >(null)

  const idx = useMemo(() => (tree.data ? indexTree(tree.data) : null), [tree.data])

  // Tab title reflects the open note/folder ("<name> · Poznámky · home"); falls
  // back to just the module while the tree loads or at the root.
  const openTitle = sel.noteId
    ? idx?.noteById.get(sel.noteId)?.title
    : sel.folderId
      ? idx?.folderById.get(sel.folderId)?.name
      : undefined
  useDocumentTitle(openTitle, cs.notes.title)

  // Where a new note/folder should land. Opening a note clears sel.folderId, so a
  // "New note" while viewing a note would otherwise create at the root — resolve
  // the note's own containing folder instead so it lands next to what's on screen.
  const createParentId = (): string | null =>
    sel.noteId ? idx?.folderIdByNoteId.get(sel.noteId) ?? null : sel.folderId

  const search = useQuery({
    queryKey: qk.noteSearch(searchQ),
    queryFn: () => api.searchNotes(searchQ),
    enabled: searchQ.trim().length > 0,
  })

  // Deep-link: resolve a slug path to a selection (path→id, no redirects).
  useEffect(() => {
    if (!tree.data) return
    if (splat === lastPath.current) return
    if (!splat) {
      setSel({ noteId: null, folderId: null })
      lastPath.current = ''
      return
    }
    // Guard against out-of-order resolves: navigating a→b fires two requests, and
    // if a's resolves last it would clobber b's selection. The cleanup marks a
    // superseded request stale so its result is ignored.
    let cancelled = false
    api
      .resolveNotePath(splat)
      .then((res) => {
        if (cancelled) return
        lastPath.current = splat
        if (res.type === 'note') setSel({ noteId: res.id, folderId: null })
        else setSel({ noteId: null, folderId: res.id })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // Only a genuine 404 means the path is gone (renamed/moved, D32): fall back to
        // root — clear the selection and drop the dead path from the URL so the
        // previously-open note doesn't linger while the address bar shows a 404 path.
        // A transient network/5xx error is recoverable, so leave the URL and current
        // selection intact rather than dumping the user to root on a blip.
        if (err instanceof ApiError && err.status === 404) {
          setSel({ noteId: null, folderId: null })
          lastPath.current = ''
          navigate(routes.poznamky)
        }
      })
    return () => {
      cancelled = true
    }
  }, [splat, tree.data, navigate])

  // `explicitPath` lets a caller supply the slug path directly (e.g. a freshly
  // created note whose row isn't in the tree index yet, so slugPathById can't
  // resolve it) so the URL updates immediately instead of staying on the old path.
  const go = (type: 'note' | 'folder', id: string | null, explicitPath?: string) => {
    if (type === 'note' && id) {
      setSel({ noteId: id, folderId: null })
      const path = explicitPath ?? idx?.slugPathById.get(id)
      if (path) {
        lastPath.current = path
        navigate(`${routes.poznamky}/${path}`)
      } else {
        // Slug path unknown (note not in the tree index yet and no explicit path):
        // select it now, but drop the stale path so the address bar doesn't keep
        // pointing at the previously-open note. The reconcile effect below pushes the
        // real path once a tree refetch indexes it; clearing lastPath to '' matches
        // the root URL so the deep-link effect doesn't re-resolve while we wait.
        lastPath.current = ''
        navigate(routes.poznamky)
      }
    } else {
      setSel({ noteId: null, folderId: id })
      const path = id ? idx?.slugPathById.get(id) : ''
      lastPath.current = path ?? ''
      navigate(path ? `${routes.poznamky}/${path}` : routes.poznamky)
    }
  }

  const refresh = () => void qc.invalidateQueries({ queryKey: qk.notesTree })

  // If the selected note/folder vanished from the tree — its ancestor folder was
  // cascade-deleted, or a background refetch/WS update removed it elsewhere — the
  // delete handler's direct-id check (it only redirects when the deleted id IS the
  // selection) misses it, stranding a stale, still-editable pane on a node that's
  // gone from navigation. Fall back to the root. Guarded on !isFetching so a freshly
  // created note — selected before its refetch lands (see createMut) — isn't cleared
  // during the in-flight window.
  useEffect(() => {
    if (!idx || tree.isFetching) return
    if (sel.noteId && !idx.folderIdByNoteId.has(sel.noteId)) {
      go('folder', null)
      return
    }
    if (sel.folderId && !idx.folderById.has(sel.folderId)) {
      go('folder', null)
      return
    }
    // Keep the URL in sync with the open note/folder's live slug path. Two cases
    // converge here: (a) an item selected before the tree indexed it had its stale
    // path cleared to '' (see go), so the URL is still owed; (b) the open item was
    // renamed or moved — here or elsewhere — so its slug path changed under a URL
    // still pointing at the old slug, which a reload would 404 (path→id, no redirects,
    // D32), dumping the user to root. Once the index knows the current path, push it
    // (replace) so the address bar matches what's open. `path !== lastPath.current`
    // keeps a normal deep-link resolve (URL already canonical) from re-navigating.
    const openId = sel.noteId ?? sel.folderId
    if (openId) {
      const path = idx.slugPathById.get(openId)
      if (path && path !== lastPath.current) {
        lastPath.current = path
        navigate(`${routes.poznamky}/${path}`, { replace: true })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idx, sel.noteId, sel.folderId, tree.isFetching])

  const createMut = useMutation<NoteDetail | FolderDetail, Error, { name: string; icon: string }>({
    mutationFn: ({ name, icon }) =>
      create?.kind === 'folder'
        ? api.createFolder({ name, parent_id: createParentId(), icon })
        : api.createNote({ title: name, folder_id: createParentId() }),
    onSuccess: (res) => {
      refresh()
      setCreate(null)
      if ('body_md' in res) go('note', res.id, res.slug_path) // a note → open it (path from the response, tree not refetched yet)
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  const moveMut = useMutation<NoteDetail | Folder, Error, { targetId: string | null }>({
    mutationFn: ({ targetId }: { targetId: string | null }) => {
      if (!move || !idx) throw new Error('no move')
      return move.kind === 'note'
        ? api.moveNote(move.id, { folder_id: targetId, position: tailOf(idx.notePositions.get(targetId) ?? []) })
        : api.moveFolder(move.id, { parent_id: targetId, position: tailOf(idx.folderPositions.get(targetId) ?? []) })
    },
    onSuccess: () => {
      refresh()
      setMove(null)
    },
    // 422 unprocessable = the backend cycle guard rejected the target (D31); match
    // on the structured error code, not a substring of the message.
    onError: (e) => toast.error(e instanceof ApiError && e.code === 'unprocessable' ? 'Sem složku přesunout nelze.' : cs.notes.saveError),
  })

  const renameMut = useMutation<FolderDetail, Error, { name: string; icon: string }>({
    mutationFn: ({ name, icon }) => {
      if (!rename) throw new Error('no rename')
      return api.updateFolder(rename.id, { name, icon })
    },
    onSuccess: () => {
      refresh()
      setRename(null)
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  const deleteMut = useMutation({
    mutationFn: (cascade: boolean) => {
      if (!del) throw new Error('no delete')
      return del.kind === 'note' ? api.deleteNote(del.id) : api.deleteFolder(del.id, { cascade })
    },
    onSuccess: () => {
      refresh()
      if (del && ((del.kind === 'note' && sel.noteId === del.id) || (del.kind === 'folder' && sel.folderId === del.id))) {
        go('folder', null)
      }
      setDel(null)
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  if (tree.isLoading || !idx) {
    return (
      <div className="grid place-items-center py-16">
        <Spinner />
      </div>
    )
  }
  if (tree.isError) {
    return (
      <div className="grid place-items-center py-16 text-center">
        <div>
          <p className="mb-2 font-bold">{cs.notes.errorTitle}</p>
          <button type="button" onClick={() => tree.refetch()} className="h-9 rounded-md bg-accent px-4 text-sm font-semibold text-accent-fg">
            {cs.common.retry}
          </button>
        </div>
      </div>
    )
  }

  const rootEmpty = tree.data!.roots.length === 0 && tree.data!.root_notes.length === 0

  // Re-resolve the folder's live node (not the snapshot captured at open time)
  // and count its whole subtree, so the confirm dialog reports the true cascade.
  const delNode = del?.kind === 'folder' ? findNode(idx, del.id) : null
  const delCounts = subtreeCounts(delNode)

  // Move targets: a note may go anywhere; a folder may not move into itself or any
  // of its descendants (the backend cycle guard rejects those, so don't offer them).
  const moveTargets = (() => {
    if (!move || move.kind !== 'folder') return idx.flatFolders
    const node = findNode(idx, move.id)
    const blocked = new Set(node ? flatten(node).map((n) => n.folder.id) : [move.id])
    return idx.flatFolders.filter((t) => t.id === null || !blocked.has(t.id))
  })()

  const dialogs = (
    <>
      {create && (
        <CreateDialog
          kind={create.kind}
          location={(() => {
            const p = createParentId()
            return p ? idx.folderById.get(p)?.name ?? cs.notes.root : cs.notes.rootLocation
          })()}
          pending={createMut.isPending}
          onSubmit={(name, icon) => createMut.mutate({ name, icon })}
          onClose={() => setCreate(null)}
        />
      )}
      {rename && (
        <RenameDialog
          currentName={rename.name}
          currentIcon={idx.folderById.get(rename.id)?.icon ?? ''}
          pending={renameMut.isPending}
          onSubmit={(name, icon) => renameMut.mutate({ name, icon })}
          onClose={() => setRename(null)}
        />
      )}
      {move && (
        <MoveDialog
          kind={move.kind}
          title={move.title}
          targets={moveTargets}
          pending={moveMut.isPending}
          onPick={(targetId) => moveMut.mutate({ targetId })}
          onClose={() => setMove(null)}
        />
      )}
      {del && (
        <DeleteDialog
          kind={del.kind}
          title={del.title}
          subfolders={delCounts.folders}
          notes={delCounts.notes}
          pending={deleteMut.isPending}
          onConfirm={(cascade) => deleteMut.mutate(cascade)}
          onClose={() => setDel(null)}
        />
      )}
    </>
  )

  const openMoveNote = (id: string, title: string) => setMove({ kind: 'note', id, title })
  const openDeleteNote = (id: string, title: string) => setDel({ kind: 'note', id, title })
  const noteViewFor = (noteId: string) => (
    <NoteView
      key={noteId}
      noteId={noteId}
      readOnly={!canWrite}
      onMove={(nt) => openMoveNote(nt.id, nt.title)}
      onDelete={(nt) => openDeleteNote(nt.id, nt.title)}
    />
  )

  const shared = {
    idx,
    sel,
    canWrite,
    searchQ,
    setSearchQ,
    search,
    rootEmpty,
    go,
    onCreateNote: () => setCreate({ kind: 'note' }),
    onCreateFolder: () => setCreate({ kind: 'folder' }),
    onRenameFolder: (f: Folder) => setRename({ id: f.id, name: f.name }),
    onMoveFolder: (f: Folder) => setMove({ kind: 'folder', id: f.id, title: f.name }),
    onDeleteFolder: (f: Folder) => setDel({ kind: 'folder', id: f.id, title: f.name }),
    noteViewFor,
  }

  return (
    <div className={cn(desktop ? '-mx-8 -my-8 flex h-[calc(100vh-0px)] flex-col' : 'flex min-h-[70vh] flex-col')}>
      {desktop ? <DesktopView {...shared} /> : <MobileView {...shared} />}
      {dialogs}
    </div>
  )
}

// ---- shared prop shape ----

interface ViewProps {
  idx: TreeIndex
  sel: Selection
  canWrite: boolean
  searchQ: string
  setSearchQ: (q: string) => void
  search: ReturnType<typeof useQuery<NotePage>>
  rootEmpty: boolean
  go: (type: 'note' | 'folder', id: string | null) => void
  onCreateNote: () => void
  onCreateFolder: () => void
  onRenameFolder: (f: Folder) => void
  onMoveFolder: (f: Folder) => void
  onDeleteFolder: (f: Folder) => void
  noteViewFor: (noteId: string) => React.ReactNode
}

// ---- desktop: tree sidebar + pane ----

function DesktopView(p: ViewProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setExpanded((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })

  return (
    <>
      <div className="flex flex-none flex-wrap items-center gap-3 border-b border-border px-5 py-4">
        <div>
          <h1 className="text-lg font-extrabold tracking-tight">{cs.notes.title}</h1>
          <p className="text-[12.5px] text-muted">{cs.notes.subtitle}</p>
        </div>
        <div className="flex-1" />
        <SearchBox value={p.searchQ} onChange={p.setSearchQ} />
        {p.canWrite && (
          <>
            <ToolbarBtn onClick={p.onCreateFolder} icon={<FolderPlus size={14} />}>
              {cs.notes.newFolder}
            </ToolbarBtn>
            <button
              type="button"
              onClick={p.onCreateNote}
              className="inline-flex h-[34px] items-center gap-1.5 rounded-md bg-accent px-3.5 text-[12.5px] font-bold text-accent-fg"
            >
              <FilePlus2 size={14} /> {cs.notes.newNote}
            </button>
          </>
        )}
        {!p.canWrite && (
          <span className="inline-flex h-7 items-center rounded-full border border-border bg-s3 px-3 text-[12px] font-semibold text-muted">
            {cs.notes.readOnly}
          </span>
        )}
      </div>

      <div className="flex min-h-0 flex-1">
        {/* tree */}
        <div className="w-[274px] flex-none overflow-y-auto border-r border-border bg-s1 p-2">
          <p className="px-2 pb-2 pt-1.5 font-mono text-[10px] uppercase tracking-wide text-subtle">{cs.notes.tree}</p>
          <button
            type="button"
            onClick={() => p.go('folder', null)}
            className={cn(
              'flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-[13.5px] font-bold',
              p.sel.folderId === null && p.sel.noteId === null ? 'bg-s2 text-fg' : 'text-fg hover:bg-s2',
            )}
          >
            {cs.notes.root}
          </button>
          <TreeNodes nodes={p.idx.childFolders.get(null) ?? []} rootNotes={p.idx.childNotes.get(null) ?? []} depth={0} expanded={expanded} toggle={toggle} sel={p.sel} go={p.go} />
        </div>

        {/* pane */}
        <div className="flex min-w-0 flex-1 flex-col">
          {p.sel.noteId ? (
            p.noteViewFor(p.sel.noteId)
          ) : (
            <FolderPane {...p} />
          )}
        </div>
      </div>
    </>
  )
}

function TreeNodes({
  nodes,
  rootNotes,
  depth,
  expanded,
  toggle,
  sel,
  go,
}: {
  nodes: FolderNode[]
  rootNotes: NoteSummary[]
  depth: number
  expanded: Set<string>
  toggle: (id: string) => void
  sel: Selection
  go: (t: 'note' | 'folder', id: string | null) => void
}) {
  return (
    <>
      {nodes.map((node) => {
        const f = node.folder
        const open = expanded.has(f.id)
        return (
          <div key={f.id}>
            <div
              className={cn('flex h-8 items-center rounded-md pr-2 hover:bg-s2', sel.folderId === f.id && 'bg-s2')}
              style={{ paddingLeft: 8 + depth * 12 }}
            >
              <button type="button" aria-label={open ? 'Sbalit' : 'Rozbalit'} onClick={() => toggle(f.id)} className="grid h-6 w-5 place-items-center text-subtle">
                {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
              </button>
              <button type="button" onClick={() => go('folder', f.id)} className="flex min-w-0 flex-1 items-center gap-1.5 text-left">
                <span className="flex-none text-[13px] leading-none">{f.icon || DEFAULT_FOLDER_ICON}</span>
                <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-fg">{f.name}</span>
                <span className="flex-none font-mono text-[10px] text-subtle">{node.subfolders.length + node.notes.length}</span>
              </button>
            </div>
            {open && (
              <>
                <TreeNodes nodes={node.subfolders} rootNotes={[]} depth={depth + 1} expanded={expanded} toggle={toggle} sel={sel} go={go} />
                {node.notes.map((nt) => (
                  <NoteRow key={nt.id} nt={nt} depth={depth + 1} active={sel.noteId === nt.id} onClick={() => go('note', nt.id)} />
                ))}
              </>
            )}
          </div>
        )
      })}
      {rootNotes.map((nt) => (
        <NoteRow key={nt.id} nt={nt} depth={depth} active={sel.noteId === nt.id} onClick={() => go('note', nt.id)} />
      ))}
    </>
  )
}

function NoteRow({ nt, depth, active, onClick }: { nt: NoteSummary; depth: number; active: boolean; onClick: () => void }) {
  const pinned = nt.pinned.household || nt.pinned.personal
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn('flex h-8 w-full items-center gap-2 rounded-md pr-2 text-left hover:bg-s2', active && 'bg-s2')}
      style={{ paddingLeft: 8 + depth * 12 }}
    >
      <span className="grid w-5 flex-none place-items-center">
        <span className={cn('h-2 w-2 rounded-full', pinned ? 'bg-accent' : 'bg-border-strong')} />
      </span>
      <span className={cn('min-w-0 flex-1 truncate text-[13px]', active ? 'font-semibold text-fg' : 'text-muted')}>{nt.title}</span>
    </button>
  )
}

// ---- folder pane / browse (desktop) + search ----

function FolderPane(p: ViewProps) {
  const searching = p.searchQ.trim().length > 0
  const folder = p.sel.folderId ? p.idx.folderById.get(p.sel.folderId) ?? null : null
  const childFolders = p.idx.childFolders.get(p.sel.folderId) ?? []
  const childNotes = p.idx.childNotes.get(p.sel.folderId) ?? []
  const empty = childFolders.length === 0 && childNotes.length === 0

  if (p.rootEmpty && !searching) return <EmptyRoot canWrite={p.canWrite} onCreate={p.onCreateNote} />

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-none items-center gap-2 border-b border-border px-5 py-3.5">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          {folder && <span className="flex-none text-lg leading-none">{folder.icon || DEFAULT_FOLDER_ICON}</span>}
          <div className="truncate text-lg font-extrabold tracking-tight">{folder ? folder.name : cs.notes.root}</div>
        </div>
        {folder && p.canWrite && (
          <div className="flex gap-1.5">
            <SmallBtn onClick={() => p.onRenameFolder(folder)}>{cs.notes.rename}</SmallBtn>
            <SmallBtn onClick={() => p.onMoveFolder(folder)}>{cs.notes.move}</SmallBtn>
            <SmallBtn onClick={() => p.onDeleteFolder(folder)} danger>
              {cs.notes.delete}
            </SmallBtn>
          </div>
        )}
      </div>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-5 py-4">
        {searching ? (
          <SearchResults p={p} />
        ) : empty ? (
          <EmptyFolder canWrite={p.canWrite} onCreate={p.onCreateNote} />
        ) : (
          <>
            {childFolders.map((node) => (
              <BrowseFolderRow key={node.folder.id} node={node} onOpen={() => p.go('folder', node.folder.id)} />
            ))}
            {childNotes.map((nt) => (
              <BrowseNoteRow key={nt.id} nt={nt} onOpen={() => p.go('note', nt.id)} />
            ))}
          </>
        )}
      </div>
    </div>
  )
}

function flatten(node: FolderNode): FolderNode[] {
  return [node, ...node.subfolders.flatMap(flatten)]
}

// Re-resolve a folder's live tree node by id. The delete dialog must not trust a
// snapshot captured at open time — a background refetch can reshape the tree and
// leave a stale/absent node, which would understate the deletion scope.
function findNode(idx: TreeIndex, folderId: string): FolderNode | null {
  for (const root of idx.childFolders.get(null) ?? []) {
    for (const n of flatten(root)) {
      if (n.folder.id === folderId) return n
    }
  }
  return null
}

// Count a folder's entire subtree (descendant folders + notes), not just its
// direct children, so cascade=true is sent whenever anything would be deleted.
function subtreeCounts(node: FolderNode | null): { folders: number; notes: number } {
  if (!node) return { folders: 0, notes: 0 }
  let folders = node.subfolders.length
  let notes = node.notes.length
  for (const sub of node.subfolders) {
    const c = subtreeCounts(sub)
    folders += c.folders
    notes += c.notes
  }
  return { folders, notes }
}

function SearchResults({ p }: { p: ViewProps }) {
  if (p.search.isLoading) return <div className="grid place-items-center py-10"><Spinner /></div>
  // A failed search must not fall through to the "no results" empty state — that would
  // tell the user the search succeeded and matched nothing. Show an error + retry.
  if (p.search.isError)
    return (
      <div className="grid place-items-center py-12 text-center">
        <div>
          <p className="mb-2 font-bold">{cs.notes.errorTitle}</p>
          <button
            type="button"
            onClick={() => p.search.refetch()}
            className="h-9 rounded-md bg-accent px-4 text-sm font-semibold text-accent-fg"
          >
            {cs.common.retry}
          </button>
        </div>
      </div>
    )
  const items = p.search.data?.items ?? []
  if (items.length === 0)
    return (
      <div className="grid place-items-center py-12 text-center text-muted">
        <div>
          <div className="mb-1 font-bold text-fg">{cs.notes.noResultsTitle}</div>
          <div className="text-sm">{cs.notes.noResultsBody}</div>
        </div>
      </div>
    )
  return (
    <>
      {items.map((nt) => (
        <BrowseNoteRow key={nt.id} nt={nt} onOpen={() => p.go('note', nt.id)} folderLabel={folderLabelOf(p.idx, nt)} />
      ))}
    </>
  )
}

function folderLabelOf(idx: TreeIndex, nt: NoteSummary): string {
  if (!nt.folder_id) return cs.notes.root
  return idx.folderNamePathById.get(nt.folder_id) ?? cs.notes.root
}

function BrowseFolderRow({ node, onOpen }: { node: FolderNode; onOpen: () => void }) {
  return (
    <button type="button" onClick={onOpen} className="flex w-full items-center gap-3 rounded-xl border border-border bg-s1 px-3 py-3 text-left hover:border-border-strong">
      <span className="grid h-7 w-7 flex-none place-items-center rounded-md bg-s3 text-[15px] leading-none">
        {node.folder.icon || DEFAULT_FOLDER_ICON}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-bold">{node.folder.name}</span>
        <span className="block font-mono text-[11px] text-subtle">{folderCountLabel(node)}</span>
      </span>
      <ChevronRight size={16} className="flex-none text-muted" />
    </button>
  )
}

function BrowseNoteRow({ nt, onOpen, folderLabel }: { nt: NoteSummary; onOpen: () => void; folderLabel?: string }) {
  const pinned = nt.pinned.household || nt.pinned.personal
  return (
    <button type="button" onClick={onOpen} className="flex w-full items-start gap-3 rounded-xl border border-border bg-s1 px-3 py-3 text-left hover:border-border-strong">
      <span className={cn('mt-1 h-2.5 w-2.5 flex-none rounded-full', pinned ? 'bg-accent' : 'bg-border-strong')} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold">{nt.title}</span>
        {folderLabel && <span className="block font-mono text-[10.5px] text-subtle">{folderLabel}</span>}
      </span>
    </button>
  )
}

// ---- mobile: drill-down ----

function MobileView(p: ViewProps) {
  const searching = p.searchQ.trim().length > 0
  const folder = p.sel.folderId ? p.idx.folderById.get(p.sel.folderId) ?? null : null
  const childFolders = p.idx.childFolders.get(p.sel.folderId) ?? []
  const childNotes = p.idx.childNotes.get(p.sel.folderId) ?? []
  const empty = childFolders.length === 0 && childNotes.length === 0

  if (p.sel.noteId) {
    // Opening a note clears sel.folderId, so derive the note's own containing folder
    // to send Back there (not to root); the "Poznámky" chip still jumps to root.
    const noteFolder = p.idx.folderIdByNoteId.get(p.sel.noteId) ?? null
    return (
      <>
        <div className="flex flex-none items-center gap-2 border-b border-border px-2 pb-2.5 pt-1">
          <button type="button" onClick={() => p.go('folder', noteFolder)} aria-label="Zpět" className="grid h-10 w-10 place-items-center rounded-lg border border-border bg-s2 text-fg">
            ‹
          </button>
          <div className="flex-1" />
          <button type="button" onClick={() => p.go('folder', null)} className="h-8 rounded-md border border-border bg-s2 px-3 text-[12px] font-semibold text-muted">
            {cs.notes.root}
          </button>
        </div>
        {p.noteViewFor(p.sel.noteId)}
      </>
    )
  }

  if (p.rootEmpty && !searching) return <EmptyRoot canWrite={p.canWrite} onCreate={p.onCreateNote} />

  return (
    <>
      <div className="flex-none border-b border-border px-1 pb-3">
        <div className="mb-2.5 flex items-center gap-2">
          {folder && (
            <button type="button" onClick={() => p.go('folder', folder.parent_id)} aria-label="Zpět" className="grid h-10 w-10 flex-none place-items-center rounded-lg border border-border bg-s2 text-fg">
              ‹
            </button>
          )}
          <div className="flex min-w-0 flex-1 items-center gap-2">
            {folder && <span className="flex-none text-lg leading-none">{folder.icon || DEFAULT_FOLDER_ICON}</span>}
            <span className="truncate text-lg font-extrabold tracking-tight">{folder ? folder.name : cs.notes.title}</span>
          </div>
          {p.canWrite && (
            <>
              <button type="button" onClick={p.onCreateFolder} aria-label={cs.notes.newFolder} className="grid h-10 w-10 flex-none place-items-center rounded-lg border border-border bg-s2 text-muted">
                <FolderPlus size={16} />
              </button>
              <button type="button" onClick={p.onCreateNote} aria-label={cs.notes.newNote} className="grid h-10 w-10 flex-none place-items-center rounded-lg bg-accent text-accent-fg">
                <FilePlus2 size={16} />
              </button>
            </>
          )}
        </div>
        {folder && p.canWrite && (
          <div className="mb-2.5 flex gap-2">
            <SmallBtn onClick={() => p.onRenameFolder(folder)} full>
              {cs.notes.rename}
            </SmallBtn>
            <SmallBtn onClick={() => p.onMoveFolder(folder)} full>
              {cs.notes.move}
            </SmallBtn>
            <SmallBtn onClick={() => p.onDeleteFolder(folder)} danger full>
              {cs.notes.delete}
            </SmallBtn>
          </div>
        )}
        <SearchBox value={p.searchQ} onChange={p.setSearchQ} full />
      </div>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-1 py-3">
        {searching ? (
          <SearchResults p={p} />
        ) : empty ? (
          <EmptyFolder canWrite={p.canWrite} onCreate={p.onCreateNote} />
        ) : (
          <>
            {childFolders.map((node) => (
              <BrowseFolderRow key={node.folder.id} node={node} onOpen={() => p.go('folder', node.folder.id)} />
            ))}
            {childNotes.map((nt) => (
              <BrowseNoteRow key={nt.id} nt={nt} onOpen={() => p.go('note', nt.id)} />
            ))}
          </>
        )}
      </div>
    </>
  )
}

// ---- small shared bits ----

function SearchBox({ value, onChange, full }: { value: string; onChange: (v: string) => void; full?: boolean }) {
  return (
    <div className={cn('flex h-[38px] items-center gap-2 rounded-md border border-border bg-s2 px-3', full ? 'w-full' : 'min-w-[220px]')}>
      <Search size={14} className="text-subtle" />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={cs.notes.search}
        className="min-w-0 flex-1 bg-transparent text-sm text-fg outline-none placeholder:text-subtle"
      />
    </div>
  )
}

function ToolbarBtn({ onClick, icon, children }: { onClick: () => void; icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex h-[34px] items-center gap-1.5 rounded-md border border-border bg-s2 px-3 text-[12.5px] font-semibold text-fg hover:bg-s3">
      {icon} {children}
    </button>
  )
}

function SmallBtn({ onClick, children, danger, full }: { onClick: () => void; children: React.ReactNode; danger?: boolean; full?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'h-9 rounded-md border px-3 text-[12.5px] font-semibold',
        full && 'flex-1',
        danger ? 'border-danger bg-danger/10 text-danger' : 'border-border bg-s2 text-fg hover:bg-s3',
      )}
    >
      {children}
    </button>
  )
}

function EmptyRoot({ canWrite, onCreate }: { canWrite: boolean; onCreate: () => void }) {
  return (
    <div className="grid flex-1 place-items-center py-16 text-center">
      <div className="max-w-sm">
        <div className="mx-auto mb-4 grid h-13 w-13 place-items-center rounded-xl border border-dashed border-border-strong bg-s2 text-2xl text-subtle">❏</div>
        <p className="mb-1.5 text-lg font-bold">{cs.notes.emptyRootTitle}</p>
        <p className="mb-4 text-sm text-muted text-pretty">{cs.notes.emptyRootBody}</p>
        {canWrite && (
          <button type="button" onClick={onCreate} className="h-10 rounded-md bg-accent px-4 text-sm font-bold text-accent-fg">
            {cs.notes.createNoteFull}
          </button>
        )}
      </div>
    </div>
  )
}

function EmptyFolder({ canWrite, onCreate }: { canWrite: boolean; onCreate: () => void }) {
  return (
    <div className="grid place-items-center py-12 text-center">
      <div>
        <div className="mx-auto mb-3 grid h-11 w-11 place-items-center rounded-xl border border-dashed border-border-strong bg-s2 text-xl text-subtle">❏</div>
        <p className="mb-1 font-semibold">{cs.notes.emptyFolderTitle}</p>
        <p className="mb-3.5 text-sm text-muted text-pretty">{cs.notes.emptyFolderBody}</p>
        {canWrite && (
          <button type="button" onClick={onCreate} className="h-9 rounded-md bg-accent px-4 text-sm font-bold text-accent-fg">
            {cs.notes.noteHere}
          </button>
        )}
      </div>
    </div>
  )
}
