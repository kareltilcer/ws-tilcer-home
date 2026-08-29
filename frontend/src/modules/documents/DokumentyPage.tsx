import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  ChevronLeft,
  ChevronRight,
  FolderPlus,
  Grid2x2,
  List,
  Pencil,
  MoveRight,
  Search,
  Trash2,
  Upload,
} from 'lucide-react'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
import * as api from './api/endpoints'
import type { Scope } from '@/api/types'
import type { DocFolder, DocFolderDetail, DocFolderNode, DocumentDetail, DocumentPage, DocumentSummary, DocumentsTree } from './api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { fmtBytes, fmtDateISO } from '@/i18n/format'
import { useAuth } from '@/app/auth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { DEFAULT_FOLDER_ICON } from '@/components/common/FolderIconPicker'
import { routes } from '@/app/routes'
import { parseScopedPath, resolvedKey, scopedRoute } from '@/lib/scope'
import { PrivateMark, RootSwitcher } from '@/components/common/RootSwitcher'
import { PublishDialog } from '@/components/common/PublishDialog'
import { tailOf } from '@/lib/lexorank'
import { findNode, indexTree as indexFolderTree, subtreeCounts, type FolderTreeIndex, type MoveTarget } from '@/lib/foldertree'
import { DocumentView } from './DocumentView'
import { DeleteDialog, MoveDialog } from '@/components/common/TreeDialogs'
import { CreateFolderDialog, EditDocumentDialog, RenameFolderDialog } from './DocumentsDialogs'
import { deleteLabels, hardDeleteOption, moveLabels } from './treeDialogLabels'
import { UploadPanel, useFileDrop, useUploadQueue } from './UploadQueue'
import { kindMeta, statusChip } from './fileKind'

// Dokumenty (design/v4/Home.dc.html — "DOKUMENTY (documents)"): a folder tree plus a
// documents pane, list by default with a grid toggle, mobile drill-down, and a
// standalone DocumentView the dashboard overlay reuses.
//
// Navigation works by SLUG PATH (/dokumenty/smlouvy/energie/cez) resolved to an id
// once, then everything happens by id — the permanent address of a document is its
// id, and its slug path legitimately 404s after a rename or move (D32/D42).

// ---- tree indexing ----

// The index and its two lookups are shared with Poznámky — see src/lib/foldertree.
// What stays here is the naming: `items` in the shared index are documents.
type DocIndex = FolderTreeIndex<DocFolder, DocFolderNode, DocumentSummary>

/**
 * rootLabel names the root the user is standing in — carrier 4 of the five
 * "which tree am I in" carriers (D177/D183).
 *
 * ⚠ Every place that names the root goes through this. They all said "Dokumenty"
 * — the SHARED label — in the private tree, which left this page with four of the
 * five carriers where Poznámky has five, and made a screenshot of the private
 * documents root harder to tell from the shared one than its notes equivalent.
 * The two pages are deliberate mirror images (D40).
 */
function rootLabel(scope: Scope): string {
  return scope === 'private' ? cs.privacy.privateDocuments : cs.documents.root
}

function indexTree(tree: DocumentsTree): DocIndex {
  return indexFolderTree(tree.roots, tree.root_documents, {
    itemsOf: (n) => n.documents,
    rootLabel: cs.documents.rootLocation,
    defaultIcon: DEFAULT_FOLDER_ICON,
  })
}

function folderCountLabel(node: DocFolderNode): string {
  const parts: string[] = []
  if (node.subfolders.length) parts.push(count(node.subfolders.length, PLURAL.folders))
  if (node.documents.length) parts.push(count(node.documents.length, PLURAL.documents))
  return parts.join(' · ') || cs.documents.emptyFolderLabel
}

// ---- page ----

type Selection = { documentId: string | null; folderId: string | null }
type ViewMode = 'list' | 'grid'
const VIEW_KEY = 'home.documents.view'

export function DokumentyPage() {
  const { canWrite, isAdmin } = useAuth()
  const desktop = useIsDesktop()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const splat = useParams()['*'] ?? ''
  // ⚠ Holds a SCOPE-QUALIFIED key (resolvedKey), never a bare slug path — the same
  // slug path names a different item in each root. See lib/scope.ts.
  const lastPath = useRef('')
  const filePicker = useRef<HTMLInputElement>(null)

  // ⚠ THE ROOT SCOPE COMES FROM THE URL, not from component state (v9, D177/D185).
  // /dokumenty/… is the household tree; /dokumenty/soukrome/… is this member's own.
  // See PoznamkyPage for the full argument — the two pages are deliberately mirror
  // images (D40), so a change to one belongs in the other.
  const { scope, path: slugPath } = parseScopedPath(splat)

  const tree = useQuery({
    queryKey: qk.documentsTree(scope),
    queryFn: () => api.getDocumentsTree(scope),
  })
  const [sel, setSel] = useState<Selection>({ documentId: null, folderId: null })
  const [searchQ, setSearchQ] = useState('')
  const [view, setView] = useState<ViewMode>(() =>
    (localStorage.getItem(VIEW_KEY) as ViewMode | null) === 'grid' ? 'grid' : 'list',
  )
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [createFolder, setCreateFolder] = useState(false)
  const [renameFolder, setRenameFolder] = useState<{ id: string; name: string } | null>(null)
  const [editDoc, setEditDoc] = useState<{ id: string; title: string; description: string } | null>(null)
  const [move, setMove] = useState<{ kind: 'document' | 'folder'; id: string; title: string } | null>(null)
  const [del, setDel] = useState<{ kind: 'document' | 'folder'; id: string; title: string } | null>(null)
  const [publish, setPublish] = useState<{ kind: 'document' | 'folder'; id: string; title: string } | null>(null)

  const idx = useMemo(() => (tree.data ? indexTree(tree.data) : null), [tree.data])

  const setViewMode = (v: ViewMode) => {
    setView(v)
    localStorage.setItem(VIEW_KEY, v)
  }

  const openTitle = sel.documentId
    ? idx?.itemById.get(sel.documentId)?.title
    : sel.folderId
      ? idx?.folderById.get(sel.folderId)?.name
      : undefined
  useDocumentTitle(openTitle, cs.documents.title)

  // Where a new folder / upload should land. Opening a document clears sel.folderId,
  // so resolve the open document's own folder instead of dropping to the root.
  const currentFolderId = (): string | null =>
    sel.documentId ? (idx?.folderIdByItemId.get(sel.documentId) ?? null) : sel.folderId

  const refresh = useCallback(() => {
    void qc.invalidateQueries({ queryKey: qk.documentsAll })
  }, [qc])

  const upload = useUploadQueue({
    folderId: currentFolderId(),
    onUploaded: refresh,
    maxUploadMB: tree.data?.max_upload_mb,
    scope,
  })
  const drop = useFileDrop((files) => {
    if (canWrite) upload.enqueue(files)
  })

  const search = useQuery<DocumentPage>({
    queryKey: qk.documentsSearch(searchQ, scope),
    queryFn: () => api.searchDocuments(searchQ, scope),
    enabled: searchQ.trim().length > 0,
  })

  // Deep-link: resolve a slug path to a selection (path→id, no redirects).
  useEffect(() => {
    if (!tree.data) return
    // ⚠ Scope-qualified, or switching roots at the SAME slug path would look like
    // "nothing changed" and leave the other tree's item selected. See resolvedKey.
    if (resolvedKey(scope, slugPath) === lastPath.current) return
    if (!slugPath) {
      setSel({ documentId: null, folderId: null })
      lastPath.current = resolvedKey(scope, '')
      return
    }
    // Guard against out-of-order resolves: navigating a→b fires two requests, and if
    // a's lands last it would clobber b's selection.
    let cancelled = false
    api
      .resolveDocumentPath(slugPath, scope)
      .then((res) => {
        if (cancelled) return
        lastPath.current = resolvedKey(scope, slugPath)
        if (res.type === 'document') setSel({ documentId: res.id, folderId: null })
        else setSel({ documentId: null, folderId: res.id })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // Only a genuine 404 means the path is gone (renamed/moved): fall back to the
        // root. A transient 5xx leaves the URL and selection alone.
        if (err instanceof ApiError && err.status === 404) {
          setSel({ documentId: null, folderId: null })
          lastPath.current = resolvedKey(scope, '')
          navigate(scopedRoute(routes.dokumenty, scope))
        }
      })
    return () => {
      cancelled = true
    }
  }, [slugPath, scope, tree.data, navigate])

  const go = (type: 'document' | 'folder', id: string | null, explicitPath?: string) => {
    if (type === 'document' && id) {
      setSel({ documentId: id, folderId: null })
      const path = explicitPath ?? idx?.slugPathById.get(id)
      if (path) {
        lastPath.current = resolvedKey(scope, path)
        navigate(scopedRoute(routes.dokumenty, scope, path))
      } else {
        // Not in the tree index yet: select it now and drop the stale path so the
        // address bar stops pointing at the previously-open document.
        lastPath.current = resolvedKey(scope, '')
        navigate(scopedRoute(routes.dokumenty, scope))
      }
    } else {
      setSel({ documentId: null, folderId: id })
      const path = id ? idx?.slugPathById.get(id) : ''
      lastPath.current = resolvedKey(scope, path ?? '')
      navigate(scopedRoute(routes.dokumenty, scope, path ?? ''))
    }
  }

  // Keep the selection and the URL honest: a selected item that vanished (cascade
  // delete, or a change made on another device) falls back to the root, and an open
  // item whose slug path changed gets the URL rewritten — otherwise a reload would
  // 404 on the stale path.
  useEffect(() => {
    if (!idx || tree.isFetching) return
    // ⚠ And not while the current address is still UNRESOLVED — after a cross-root
    // hop the selection describes the previous address, so "not in the tree" means
    // "not looked up yet". See the Poznámky twin.
    if (resolvedKey(scope, slugPath) !== lastPath.current) return
    if (sel.documentId && !idx.folderIdByItemId.has(sel.documentId)) {
      go('folder', null)
      return
    }
    if (sel.folderId && !idx.folderById.has(sel.folderId)) {
      go('folder', null)
      return
    }
    const openId = sel.documentId ?? sel.folderId
    if (openId) {
      const path = idx.slugPathById.get(openId)
      if (path && resolvedKey(scope, path) !== lastPath.current) {
        lastPath.current = resolvedKey(scope, path)
        navigate(scopedRoute(routes.dokumenty, scope, path), { replace: true })
      }
    }
    // scope/slugPath are here so the new unresolved-address guard is re-evaluated
    // when the address changes, not only when the selection does.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idx, sel.documentId, sel.folderId, tree.isFetching, scope, slugPath])

  const createFolderMut = useMutation({
    mutationFn: ({ name, icon }: { name: string; icon: string }) =>
      // v9: honoured only at the root — with a parent the parent's scope governs
      // and a disagreement is a 422 (D177).
      api.createDocumentFolder({ name, parent_id: currentFolderId(), icon, scope }),
    onSuccess: () => {
      refresh()
      setCreateFolder(false)
    },
    onError: () => toast.error(cs.documents.saveError),
  })

  const renameFolderMut = useMutation({
    mutationFn: ({ name, icon }: { name: string; icon: string }) => {
      if (!renameFolder) throw new Error('no rename')
      return api.updateDocumentFolder(renameFolder.id, { name, icon })
    },
    onSuccess: () => {
      refresh()
      setRenameFolder(null)
    },
    onError: () => toast.error(cs.documents.saveError),
  })

  const editDocMut = useMutation({
    mutationFn: (values: { title: string; description: string }) => {
      if (!editDoc) throw new Error('no edit')
      return api.updateDocument(editDoc.id, {
        title: values.title,
        // An emptied field is sent as "" — the server stores that as NULL. Sending
        // null instead would decode as "field absent" and the clear would be dropped.
        description: values.description,
      })
    },
    onSuccess: (doc) => {
      refresh()
      void qc.invalidateQueries({ queryKey: qk.documentDetail(doc.id) })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      setEditDoc(null)
    },
    onError: () => toast.error(cs.documents.saveError),
  })

  const moveMut = useMutation<DocumentDetail | DocFolder, Error, { targetId: string | null }>({
    mutationFn: ({ targetId }: { targetId: string | null }) => {
      if (!move || !idx) throw new Error('no move')
      return move.kind === 'document'
        ? api.moveDocument(move.id, {
            folder_id: targetId,
            position: tailOf(idx.itemPositions.get(targetId) ?? []),
          })
        : api.moveDocumentFolder(move.id, {
            parent_id: targetId,
            position: tailOf(idx.folderPositions.get(targetId) ?? []),
          })
    },
    onSuccess: () => {
      refresh()
      // The permanent link is unaffected by a move — worth saying, because moving a
      // file elsewhere usually does break links.
      if (move?.kind === 'document') toast.success(cs.documents.moveDone)
      setMove(null)
    },
    // 422 = the backend cycle guard refused the target.
    onError: (e) =>
      toast.error(e instanceof ApiError && e.code === 'unprocessable' ? 'Sem složku přesunout nelze.' : cs.documents.saveError),
  })

  // Publish (v9, D182). Owner-only and private-only.
  //
  // ⚠ Declared HERE, with the other mutations and above the loading/error early
  // returns: hooks must run in the same order on every render, and putting this
  // below them blanks the page with "rendered more hooks than during the previous
  // render". That mistake was made once in PoznamkyPage and caught only by opening
  // the app — no test would have seen it.
  const publishMut = useMutation<DocumentDetail | DocFolderDetail, Error, void>({
    mutationFn: () => {
      if (!publish) throw new Error('no publish')
      return publish.kind === 'folder'
        ? api.publishDocumentFolder(publish.id)
        : api.publishDocument(publish.id)
    },
    onSuccess: () => {
      // The item has LEFT this tree: gone from the private root, new in the shared
      // one. Both scopes are stale, so invalidate the module prefix.
      void qc.invalidateQueries({ queryKey: qk.documentsAll })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      toast.success(cs.privacy.published)
      setPublish(null)
      go('folder', null)
    },
    onError: () => toast.error(cs.privacy.publishError),
  })

  const deleteMut = useMutation({
    mutationFn: ({ cascade, hard }: { cascade: boolean; hard: boolean }) => {
      if (!del) throw new Error('no delete')
      return del.kind === 'document'
        ? api.deleteDocument(del.id, hard)
        : api.deleteDocumentFolder(del.id, { cascade, hard })
    },
    onSuccess: (_res, vars) => {
      refresh()
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      if (del && ((del.kind === 'document' && sel.documentId === del.id) || (del.kind === 'folder' && sel.folderId === del.id))) {
        go('folder', null)
      }
      // Folder and document deletes share this mutation, so the confirmation has to
      // name what was actually deleted.
      const done =
        del?.kind === 'folder'
          ? vars.hard
            ? cs.documents.hardDeleteFolderDone
            : cs.documents.softDeleteFolderDone
          : vars.hard
            ? cs.documents.hardDeleteDone
            : cs.documents.softDeleteDone
      toast.success(done)
      setDel(null)
    },
    onError: (e) => {
      // archived_descendants: the folder still holds archived rows somewhere in its
      // subtree, so the backend refused a hard delete that would have purged them (and
      // their files). The tree never shows those rows, so the confirm above could not
      // have counted them — refusing is the only honest answer, and cascade cannot help.
      if (e instanceof ApiError && e.code === 'archived_descendants') {
        toast.error(cs.documents.deleteFolderArchivedConflict)
        return
      }
      // conflict: the folder is not empty and we sent cascade=false, which means the
      // tree the confirm computed that from is STALE — someone added a child from
      // another device. Refetch it, so the reopened dialog counts what is really there
      // and the retry carries the cascade this one was missing; without that the same
      // request repeats forever.
      if (e instanceof ApiError && e.code === 'conflict') {
        refresh()
        toast.error(cs.documents.deleteFolderStaleConflict)
        return
      }
      toast.error(cs.documents.saveError)
    },
  })

  // Move targets exclude the moved folder and its descendants, so the UI cannot even
  // offer the cycle the backend would reject.
  const moveTargets = (): MoveTarget[] => {
    if (!idx || !move) return []
    if (move.kind === 'document') return idx.flatFolders
    return idx.flatFolders.filter((t) => t.id !== move.id && !(t.id && (idx.ancestorsById.get(t.id) ?? []).includes(move.id)))
  }

  if (tree.isLoading) {
    return (
      <div className="grid min-h-[320px] place-items-center">
        <Spinner />
      </div>
    )
  }
  if (tree.isError || !idx) {
    return (
      <div className="grid min-h-[320px] place-items-center px-4 text-center">
        <div className="max-w-sm rounded-2xl border border-danger/50 bg-danger/5 p-7">
          <div className="mb-1.5 font-bold">{cs.documents.errorTitle}</div>
          <Button className="mt-3" onClick={() => void tree.refetch()}>
            {cs.common.retry}
          </Button>
        </div>
      </div>
    )
  }

  const searching = searchQ.trim().length > 0
  // What the publish confirmation counts — the whole subtree, computed once. See
  // subtreeCounts for why direct children are the wrong (and dangerous) number.
  const publishCounts = publish?.kind === 'folder' ? subtreeCounts(idx, publish.id) : null
  const props: ViewProps = {
    idx,
    scope,
    sel,
    view,
    setViewMode,
    searchQ,
    setSearchQ,
    searching,
    search,
    canWrite,
    isAdmin,
    expanded,
    // The CALLER passes what it is currently showing, because the map does not hold
    // every folder: a depth-0 folder renders open with no entry at all, and negating
    // the missing entry would write back the state it was already in — one dead click
    // before a root folder collapses, while nested ones close on the first.
    toggleExpanded: (id: string, isOpen: boolean) => setExpanded((prev) => ({ ...prev, [id]: !isOpen })),
    go,
    onCreateFolder: () => setCreateFolder(true),
    onPickFiles: () => filePicker.current?.click(),
    onRenameFolder: (f) => setRenameFolder({ id: f.id, name: f.name }),
    onEditDocument: (id, title, description) => setEditDoc({ id, title, description }),
    onMove: (kind, id, title) => setMove({ kind, id, title }),
    onDelete: (kind, id, title) => setDel({ kind, id, title }),
    onPublish: (kind, id, title) => setPublish({ kind, id, title }),
    dropping: drop.dragging,
  }

  const delNode = del?.kind === 'folder' ? findNode(idx, del.id) : null

  return (
    <div
      className="relative -mx-4 -my-5 flex min-h-[calc(100vh-8rem)] flex-col md:-mx-8 md:-my-8 md:h-[calc(100vh-0px)]"
      {...(canWrite && desktop ? drop.handlers : {})}
    >
      {/* Hidden picker: the mobile path and the desktop "Nahrát dokument" button both
          route through it, so there is one upload entry point. */}
      <input
        ref={filePicker}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          upload.enqueue(Array.from(e.target.files ?? []))
          e.target.value = '' // allow re-picking the same file
        }}
      />

      {desktop ? <DesktopView {...props} /> : <MobileView {...props} />}

      {drop.dragging && canWrite && (
        <div className="pointer-events-none absolute inset-3 z-20 grid place-items-center rounded-2xl border-2 border-dashed border-accent bg-accent-soft/60">
          <span className="text-sm font-bold text-accent">{cs.documents.uploadDropHint}</span>
        </div>
      )}

      {upload.open && (
        <UploadPanel items={upload.items} active={upload.active} onRemove={upload.remove} onClose={upload.close} />
      )}

      {publish && (
        <PublishDialog
          kind={publish.kind === 'folder' ? 'folder' : 'document'}
          title={publish.title}
          itemCount={publishCounts?.items}
          folderCount={publishCounts?.folders}
          pending={publishMut.isPending}
          onConfirm={() => publishMut.mutate()}
          onClose={() => setPublish(null)}
        />
      )}

      {createFolder && (
        <CreateFolderDialog
          location={currentFolderId() ? (idx.folderNamePathById.get(currentFolderId()!) ?? '') : cs.documents.rootLocation}
          pending={createFolderMut.isPending}
          onSubmit={(name, icon) => createFolderMut.mutate({ name, icon })}
          onClose={() => setCreateFolder(false)}
        />
      )}
      {renameFolder && (
        <RenameFolderDialog
          currentName={renameFolder.name}
          currentIcon={idx.folderById.get(renameFolder.id)?.icon ?? ''}
          pending={renameFolderMut.isPending}
          onSubmit={(name, icon) => renameFolderMut.mutate({ name, icon })}
          onClose={() => setRenameFolder(null)}
        />
      )}
      {editDoc && (
        <EditDocumentDialog
          currentTitle={editDoc.title}
          currentDescription={editDoc.description}
          pending={editDocMut.isPending}
          onSubmit={(values) => editDocMut.mutate(values)}
          onClose={() => setEditDoc(null)}
        />
      )}
      {move && (
        <MoveDialog
          isFolder={move.kind === 'folder'}
          title={move.title}
          targets={moveTargets()}
          labels={moveLabels}
          pending={moveMut.isPending}
          onPick={(targetId) => moveMut.mutate({ targetId })}
          onClose={() => setMove(null)}
        />
      )}
      {del && (
        <DeleteDialog
          isFolder={del.kind === 'folder'}
          title={del.title}
          subfolders={delNode?.subfolders.length ?? 0}
          items={delNode?.documents.length ?? 0}
          labels={deleteLabels}
          hardDelete={isAdmin ? hardDeleteOption : undefined}
          pending={deleteMut.isPending}
          onConfirm={(opts) => deleteMut.mutate(opts)}
          onClose={() => setDel(null)}
        />
      )}
    </div>
  )
}

interface ViewProps {
  idx: DocIndex
  sel: Selection
  /** Which root is on screen — see DokumentyPage for why it comes from the URL. */
  scope: Scope
  view: ViewMode
  setViewMode: (v: ViewMode) => void
  searchQ: string
  setSearchQ: (v: string) => void
  searching: boolean
  search: ReturnType<typeof useQuery<DocumentPage>>
  canWrite: boolean
  isAdmin: boolean
  expanded: Record<string, boolean>
  toggleExpanded: (id: string, isOpen: boolean) => void
  go: (type: 'document' | 'folder', id: string | null, explicitPath?: string) => void
  onCreateFolder: () => void
  onPickFiles: () => void
  onRenameFolder: (f: DocFolder) => void
  onEditDocument: (id: string, title: string, description: string) => void
  onMove: (kind: 'document' | 'folder', id: string, title: string) => void
  onDelete: (kind: 'document' | 'folder', id: string, title: string) => void
  /** v9: owner-only publish of a private document or folder subtree (D182). */
  onPublish: (kind: 'document' | 'folder', id: string, title: string) => void
  dropping: boolean
}

// ---- desktop: tree sidebar + pane ----

function DesktopView(p: ViewProps) {
  const openFolder = p.sel.folderId ? findNode(p.idx, p.sel.folderId) : null
  return (
    <>
      {/* Title row and root strip share ONE block, and the strip draws the rule at
          its foot (see RootSwitcher) — the Poznámky twin (D40): one behaviour, two
          implementations, kept identical. */}
      <div className="flex-none">
        <div className="flex flex-wrap items-center gap-3 px-5 pb-3 pt-4">
          <div>
            {/* The five carriers of "which tree am I in" — see RootSwitcher. */}
            <h1 className="text-lg font-extrabold tracking-tight">
              {p.scope === 'private' ? cs.privacy.privateDocuments : cs.documents.title}
            </h1>
            <p className="text-[12.5px] text-muted">
              {p.scope === 'private' ? cs.privacy.subtitleDocuments : cs.documents.subtitle}
            </p>
          </div>
          <div className="flex-1" />
          <SearchBox value={p.searchQ} onChange={p.setSearchQ} scope={p.scope} />
          <ViewToggle view={p.view} onChange={p.setViewMode} />
          {p.canWrite ? (
            <>
              <Button size="sm" onClick={p.onCreateFolder}>
                <FolderPlus size={14} aria-hidden /> {cs.documents.newFolder}
              </Button>
              <Button size="sm" variant="primary" onClick={p.onPickFiles}>
                <Upload size={14} aria-hidden /> {cs.documents.upload}
              </Button>
            </>
          ) : (
            <span className="inline-flex h-7 items-center rounded-full border border-border bg-s3 px-2.5 text-[12px] font-semibold text-muted">
              {cs.documents.readOnly}
            </span>
          )}
        </div>
        {/* Carrier 2: the switcher, on its own full-width row under the title. */}
        <RootSwitcher
          scope={p.scope}
          base={routes.dokumenty}
          sharedLabel={cs.documents.title}
          privateLabel={cs.privacy.privateDocuments}
          className="px-5"
        />
      </div>

      <div className="flex min-h-0 flex-1">
        {/* Carrier 3: the tree container is tinted with a left rail in the
            private root — always in peripheral vision, never read explicitly. */}
        <aside
          className={cn(
            'om-scroll w-[274px] flex-none overflow-y-auto border-r border-border bg-s1 p-2',
            p.scope === 'private' && 'border-l-[3px] border-l-vis-private bg-vis-private-soft/25',
          )}
        >
          <div className="px-2 pb-2 pt-1.5 font-mono text-[10px] uppercase tracking-wide text-subtle">{cs.documents.tree}</div>
          <button
            type="button"
            onClick={() => p.go('folder', null)}
            className={cn(
              'flex h-9 w-full items-center gap-2 rounded-lg px-2 text-left text-[13px] font-semibold',
              !p.sel.folderId && !p.sel.documentId ? 'bg-s2 text-accent' : 'text-fg hover:bg-s2',
            )}
          >
            <span className="w-4 text-center text-subtle" aria-hidden>
              ▦
            </span>
            {rootLabel(p.scope)}
          </button>
          <TreeNodes nodes={p.idx.childFolders.get(null) ?? []} documents={p.idx.childItems.get(null) ?? []} depth={0} p={p} />
        </aside>

        <section className="flex min-w-0 flex-1 flex-col">
          <Breadcrumb p={p} />
          {p.sel.documentId ? (
            <DocumentView
              key={p.sel.documentId}
              documentId={p.sel.documentId}
              onRename={(d) => p.onEditDocument(d.id, d.title, d.description ?? '')}
              onMove={(d) => p.onMove('document', d.id, d.title)}
              onDelete={(d) => p.onDelete('document', d.id, d.title)}
              onPublish={(d) => p.onPublish('document', d.id, d.title)}
              onGone={() => p.go('folder', null)}
            />
          ) : (
            <FolderPane p={p} node={openFolder} />
          )}
        </section>
      </div>
    </>
  )
}

function TreeNodes({
  nodes,
  documents,
  depth,
  p,
}: {
  nodes: DocFolderNode[]
  documents: DocumentSummary[]
  depth: number
  p: ViewProps
}) {
  return (
    <div>
      {nodes.map((node) => {
        const f = node.folder
        const open = p.expanded[f.id] ?? depth === 0
        const active = p.sel.folderId === f.id
        return (
          <div key={f.id}>
            <button
              type="button"
              onClick={() => {
                p.toggleExpanded(f.id, open)
                p.go('folder', f.id)
              }}
              aria-expanded={open}
              className={cn(
                'flex h-9 w-full items-center gap-2 rounded-lg pr-2 text-left text-[13px]',
                active ? 'bg-s2 text-accent' : 'text-fg hover:bg-s2',
              )}
              style={{ paddingLeft: 8 + depth * 14 }}
            >
              <span className="w-3.5 flex-none text-center text-[11px] text-subtle" aria-hidden>
                {node.subfolders.length + node.documents.length > 0 ? (open ? '▾' : '▸') : '·'}
              </span>
              <span className="flex-none text-[13px] leading-none" aria-hidden>{f.icon || DEFAULT_FOLDER_ICON}</span>
              <span className="min-w-0 flex-1 truncate font-semibold">{f.name}</span>
            </button>
            {open && (
              <TreeNodes nodes={node.subfolders} documents={node.documents} depth={depth + 1} p={p} />
            )}
          </div>
        )
      })}
      {documents.map((d) => (
        <TreeDocumentRow key={d.id} d={d} depth={depth} active={p.sel.documentId === d.id} onClick={() => p.go('document', d.id)} />
      ))}
    </div>
  )
}

function TreeDocumentRow({
  d,
  depth,
  active,
  onClick,
}: {
  d: DocumentSummary
  depth: number
  active: boolean
  onClick: () => void
}) {
  const meta = kindMeta(d.content_type)
  const pinned = d.pinned.household || d.pinned.personal
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn('flex h-9 w-full items-center gap-2 rounded-lg pr-2 text-left text-[13px]', active ? 'bg-s2 text-accent' : 'text-fg hover:bg-s2')}
      style={{ paddingLeft: 8 + depth * 14 }}
    >
      <span className={cn('grid h-[18px] w-6 flex-none place-items-center rounded font-mono text-[8px] font-bold', meta.tile)} aria-hidden>
        {meta.icon}
      </span>
      <span className="min-w-0 flex-1 truncate">{d.title}</span>
      {pinned && (
        <span
          className={cn('h-2 w-2 flex-none rounded-full', d.pinned.household ? 'bg-accent' : 'border border-muted')}
          aria-label={d.pinned.household ? cs.documents.badgeHousehold : cs.documents.badgePersonal}
        />
      )}
    </button>
  )
}

function Breadcrumb({ p }: { p: ViewProps }) {
  const openId = p.sel.documentId ?? p.sel.folderId
  const chain: { id: string | null; name: string }[] = [{ id: null, name: rootLabel(p.scope) }]
  if (openId) {
    const folderId = p.sel.documentId ? (p.idx.folderIdByItemId.get(p.sel.documentId) ?? null) : p.sel.folderId
    if (folderId) {
      for (const ancestorId of [...(p.idx.ancestorsById.get(folderId) ?? []), folderId]) {
        const f = p.idx.folderById.get(ancestorId)
        if (f) chain.push({ id: f.id, name: f.name })
      }
    }
  }
  return (
    <nav className="flex flex-none flex-wrap items-center gap-1 border-b border-border px-5 py-2.5 text-[12.5px]">
      {chain.map((c, i) => (
        <span key={c.id ?? '__root__'} className="flex items-center gap-1">
          {i > 0 && (
            <span className="text-subtle" aria-hidden>
              ›
            </span>
          )}
          <button type="button" onClick={() => p.go('folder', c.id)} className="rounded px-1 py-0.5 text-muted hover:text-fg">
            {c.name}
          </button>
        </span>
      ))}
    </nav>
  )
}

function FolderPane({ p, node }: { p: ViewProps; node: DocFolderNode | null }) {
  const folders = node ? node.subfolders : (p.idx.childFolders.get(null) ?? [])
  const documents = node ? node.documents : (p.idx.childItems.get(null) ?? [])
  const isRoot = !node
  const empty = folders.length === 0 && documents.length === 0

  if (p.searching) return <SearchResults p={p} />

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-none items-center gap-2 border-b border-border px-5 py-3.5">
        <div className="min-w-0 flex-1">
          <h2 className="flex items-center gap-2 truncate text-lg font-extrabold tracking-tight">
            {node && <span className="flex-none leading-none" aria-hidden>{node.folder.icon || DEFAULT_FOLDER_ICON}</span>}
            <span className="truncate">{node ? node.folder.name : cs.documents.title}</span>
          </h2>
          {node && <p className="font-mono text-[11px] text-subtle">{folderCountLabel(node)}</p>}
        </div>
        {node && p.canWrite && (
          <>
            <Button
              size="sm"
              className="w-9 px-0"
              title={cs.documents.rename}
              aria-label={cs.documents.rename}
              onClick={() => p.onRenameFolder(node.folder)}
            >
              <Pencil size={14} aria-hidden />
            </Button>
            <Button size="sm" onClick={() => p.onMove('folder', node.folder.id, node.folder.name)}>
              <MoveRight size={14} aria-hidden /> {cs.documents.move}
            </Button>
            {/* Publish sits in the item's own controls, owner-only, and only in
                the private tree — there is nothing to publish in the shared one. */}
            {node.folder.visibility === 'private' && (
              <Button size="sm" onClick={() => p.onPublish('folder', node.folder.id, node.folder.name)}>
                <Upload size={14} aria-hidden /> {cs.privacy.publish}
              </Button>
            )}
            <Button
              size="sm"
              variant="danger"
              className="w-9 px-0"
              title={cs.documents.delete}
              aria-label={cs.documents.delete}
              onClick={() => p.onDelete('folder', node.folder.id, node.folder.name)}
            >
              <Trash2 size={14} aria-hidden />
            </Button>
          </>
        )}
      </div>

      <div className="om-scroll min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {empty ? (
          <EmptyState root={isRoot} scope={p.scope} canWrite={p.canWrite} onUpload={p.onPickFiles} />
        ) : (
          <>
            {folders.length > 0 && (
              <div className="mb-3.5 space-y-2">
                {folders.map((f) => (
                  <FolderRow key={f.folder.id} node={f} onOpen={() => p.go('folder', f.folder.id)} />
                ))}
              </div>
            )}
            <DocumentCollection documents={documents} view={p.view} onOpen={(id) => p.go('document', id)} />
          </>
        )}
      </div>
    </div>
  )
}

// ---- mobile: drill-down ----

function MobileView(p: ViewProps) {
  const node = p.sel.folderId ? findNode(p.idx, p.sel.folderId) : null

  if (p.sel.documentId) {
    return (
      <div className="flex min-h-[70vh] flex-col">
        <button
          type="button"
          onClick={() => p.go('folder', p.idx.folderIdByItemId.get(p.sel.documentId!) ?? null)}
          className="flex h-11 items-center gap-1.5 px-4 text-[13px] font-semibold text-muted"
        >
          <ChevronLeft size={16} aria-hidden /> {rootLabel(p.scope)}
        </button>
        <DocumentView
          key={p.sel.documentId}
          documentId={p.sel.documentId}
          onRename={(d) => p.onEditDocument(d.id, d.title, d.description ?? '')}
          onMove={(d) => p.onMove('document', d.id, d.title)}
          onDelete={(d) => p.onDelete('document', d.id, d.title)}
          onPublish={(d) => p.onPublish('document', d.id, d.title)}
          onGone={() => p.go('folder', null)}
        />
      </div>
    )
  }

  const folders = node ? node.subfolders : (p.idx.childFolders.get(null) ?? [])
  const documents = node ? node.documents : (p.idx.childItems.get(null) ?? [])
  const empty = folders.length === 0 && documents.length === 0

  return (
    <div className="px-4 py-5">
      {/* ⚠ Carrier 2 on MOBILE — see the Poznámky twin (D40). Rendered only in
          DesktopView, /dokumenty/soukrome had no way in and no way out at 375 px,
          which is the viewport RootSwitcher's own doc names as the design problem
          it exists to solve. Above the heading, so it is present in the empty
          private root a member meets on day one. */}
      <RootSwitcher
        scope={p.scope}
        base={routes.dokumenty}
        sharedLabel={cs.documents.title}
        privateLabel={cs.privacy.privateDocuments}
        mobile
        className="mb-3"
      />
      <h1 className="flex items-center gap-2 text-xl font-extrabold tracking-tight">
        {node && <span className="flex-none leading-none" aria-hidden>{node.folder.icon || DEFAULT_FOLDER_ICON}</span>}
        <span className="min-w-0 truncate">{node ? node.folder.name : rootLabel(p.scope)}</span>
      </h1>
      <p className="mt-0.5 text-[13px] text-muted">
        {node
          ? folderCountLabel(node)
          : p.scope === 'private'
            ? cs.privacy.subtitleDocuments
            : cs.documents.subtitle}
      </p>

      <div className="mt-3 flex items-center gap-2">
        <SearchBox value={p.searchQ} onChange={p.setSearchQ} full scope={p.scope} />
        <ViewToggle view={p.view} onChange={p.setViewMode} />
      </div>

      {p.canWrite && (
        <div className="mt-2.5 flex gap-2">
          <Button size="sm" variant="primary" className="flex-1" onClick={p.onPickFiles}>
            <Upload size={14} aria-hidden /> {cs.documents.uploadShort}
          </Button>
          <Button size="sm" className="flex-1" onClick={p.onCreateFolder}>
            <FolderPlus size={14} aria-hidden /> {cs.documents.newFolder}
          </Button>
        </div>
      )}

      {node && (
        <button
          type="button"
          onClick={() => p.go('folder', p.idx.folderById.get(node.folder.id)?.parent_id ?? null)}
          className="mt-3 flex h-11 items-center gap-1.5 text-[13px] font-semibold text-muted"
        >
          <ChevronLeft size={16} aria-hidden /> {rootLabel(p.scope)}
        </button>
      )}

      <div className="mt-3">
        {p.searching ? (
          <SearchResults p={p} />
        ) : empty ? (
          <EmptyState root={!node} scope={p.scope} canWrite={p.canWrite} onUpload={p.onPickFiles} />
        ) : (
          <>
            {folders.length > 0 && (
              <div className="mb-3 space-y-2">
                {folders.map((f) => (
                  <FolderRow key={f.folder.id} node={f} onOpen={() => p.go('folder', f.folder.id)} />
                ))}
              </div>
            )}
            <DocumentCollection documents={documents} view={p.view} onOpen={(id) => p.go('document', id)} />
          </>
        )}
      </div>
    </div>
  )
}

// ---- shared pieces ----

function SearchBox({
  value,
  onChange,
  full,
  scope,
}: {
  value: string
  onChange: (v: string) => void
  full?: boolean
  scope: Scope
}) {
  // The scope is named in the placeholder (D184): search reaches one root only,
  // so the field says which — otherwise "nothing found" reads as "it is gone".
  const label = scope === 'private' ? cs.privacy.searchPrivateDocuments : cs.privacy.searchSharedDocuments
  return (
    <div className={cn('flex h-9 items-center gap-2 rounded-lg border border-border bg-s2 px-3', full ? 'flex-1' : 'min-w-[210px]')}>
      <Search size={14} className="flex-none text-subtle" aria-hidden />
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={label}
        aria-label={label}
        className="h-auto border-0 bg-transparent px-0 text-[13px] focus-visible:outline-none"
      />
    </div>
  )
}

function ViewToggle({ view, onChange }: { view: ViewMode; onChange: (v: ViewMode) => void }) {
  return (
    <div className="flex gap-0.5 rounded-lg border border-border bg-s2 p-0.5">
      <button
        type="button"
        onClick={() => onChange('list')}
        aria-pressed={view === 'list'}
        title={cs.documents.viewList}
        aria-label={cs.documents.viewList}
        className={cn('grid h-8 w-9 place-items-center rounded-md', view === 'list' ? 'bg-s3 text-fg' : 'text-muted')}
      >
        <List size={15} aria-hidden />
      </button>
      <button
        type="button"
        onClick={() => onChange('grid')}
        aria-pressed={view === 'grid'}
        title={cs.documents.viewGrid}
        aria-label={cs.documents.viewGrid}
        className={cn('grid h-8 w-9 place-items-center rounded-md', view === 'grid' ? 'bg-s3 text-fg' : 'text-muted')}
      >
        <Grid2x2 size={15} aria-hidden />
      </button>
    </div>
  )
}

function FolderRow({ node, onOpen }: { node: DocFolderNode; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex min-h-11 w-full items-center gap-3 rounded-xl border border-border bg-s1 px-3 py-3 text-left hover:border-border-strong"
    >
      <span className="grid h-[30px] w-[30px] flex-none place-items-center rounded-lg bg-s3 text-[16px] leading-none" aria-hidden>
        {node.folder.icon || DEFAULT_FOLDER_ICON}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-bold text-fg">{node.folder.name}</span>
        <span className="block font-mono text-[11px] text-subtle">{folderCountLabel(node)}</span>
      </span>
      <ChevronRight size={16} className="flex-none text-subtle" aria-hidden />
    </button>
  )
}

function DocumentCollection({
  documents,
  view,
  onOpen,
  folderLabelOf,
}: {
  documents: DocumentSummary[]
  view: ViewMode
  onOpen: (id: string) => void
  folderLabelOf?: (d: DocumentSummary) => string | undefined
}) {
  if (documents.length === 0) return null
  if (view === 'grid') {
    return (
      <div className="grid grid-cols-2 gap-3 md:grid-cols-[repeat(auto-fill,minmax(190px,1fr))]">
        {documents.map((d) => (
          <DocumentCard key={d.id} d={d} onOpen={() => onOpen(d.id)} />
        ))}
      </div>
    )
  }
  return (
    <div className="space-y-2">
      {documents.map((d) => (
        <DocumentRow key={d.id} d={d} onOpen={() => onOpen(d.id)} folderLabel={folderLabelOf?.(d)} />
      ))}
    </div>
  )
}

function DocumentRow({ d, onOpen, folderLabel }: { d: DocumentSummary; onOpen: () => void; folderLabel?: string }) {
  const meta = kindMeta(d.content_type)
  const chip = statusChip(d.content_type, d.preview_kind, d.preview_status)
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex min-h-11 w-full items-center gap-3 rounded-xl border border-border bg-s1 px-3 py-3 text-left hover:border-border-strong"
    >
      <span className={cn('grid h-9 w-9 flex-none place-items-center rounded-lg font-mono text-[9.5px] font-bold', meta.tile)} aria-hidden>
        {meta.icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="min-w-0 truncate text-sm font-semibold text-fg">{d.title}</span>
          {/* A private hit in a result list carries the lock AND the word (D183). */}
          {d.visibility === 'private' && <PrivateMark />}
        </span>
        <span className="mt-0.5 flex flex-wrap items-center gap-x-2 font-mono text-[11px] text-subtle">
          <span>{fmtBytes(d.byte_size)}</span>
          <span aria-hidden>·</span>
          <span>{fmtDateISO(d.updated_at.slice(0, 10))}</span>
          {folderLabel && (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">{folderLabel}</span>
            </>
          )}
        </span>
      </span>
      {chip && (
        <span className={cn('hidden flex-none rounded-full px-2 py-0.5 text-[10.5px] font-semibold sm:inline-flex', chip.className)}>
          {chip.label}
        </span>
      )}
      {(d.pinned.household || d.pinned.personal) && (
        <span
          className={cn('h-2.5 w-2.5 flex-none rounded-full', d.pinned.household ? 'bg-accent' : 'border border-muted')}
          aria-label={d.pinned.household ? cs.documents.badgeHousehold : cs.documents.badgePersonal}
        />
      )}
    </button>
  )
}

function DocumentCard({ d, onOpen }: { d: DocumentSummary; onOpen: () => void }) {
  const meta = kindMeta(d.content_type)
  const chip = statusChip(d.content_type, d.preview_kind, d.preview_status)
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex flex-col overflow-hidden rounded-xl border border-border bg-s1 text-left hover:border-border-strong"
    >
      <span className="relative grid h-[104px] place-items-center border-b border-border bg-s2">
        {d.thumbnail_url ? (
          // The thumbnail arrives asynchronously; until then the type tile stands in.
          <img src={d.thumbnail_url} alt="" className="h-full w-full object-cover" loading="lazy" />
        ) : (
          <span className={cn('grid h-11 w-11 place-items-center rounded-[9px] font-mono text-[10px] font-bold', meta.tile)} aria-hidden>
            {meta.icon}
          </span>
        )}
        {(d.pinned.household || d.pinned.personal) && (
          <span
            className={cn('absolute right-2 top-2 h-2.5 w-2.5 rounded-full', d.pinned.household ? 'bg-accent' : 'border border-muted bg-s1')}
            aria-label={d.pinned.household ? cs.documents.badgeHousehold : cs.documents.badgePersonal}
          />
        )}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-1 px-2.5 py-2.5">
        <span className="line-clamp-2 text-[13px] font-semibold text-fg">{d.title}</span>
        <span className="font-mono text-[10.5px] text-subtle">{fmtBytes(d.byte_size)}</span>
        {chip && (
          <span className={cn('mt-0.5 inline-flex w-fit rounded-full px-2 py-0.5 text-[10px] font-semibold', chip.className)}>
            {chip.label}
          </span>
        )}
      </span>
    </button>
  )
}

function SearchResults({ p }: { p: ViewProps }) {
  const items = p.search.data?.items ?? []
  if (p.search.isLoading) {
    return (
      <div className="grid min-h-[160px] place-items-center">
        <Spinner />
      </div>
    )
  }
  return (
    <div className="px-0 py-1">
      <p className="mb-2.5 font-mono text-[10.5px] text-subtle">{cs.documents.searchScope}</p>
      {items.length === 0 ? (
        <div className="grid place-items-center px-3 py-11 text-center">
          <div>
            <div className="mb-1 font-bold text-fg">{cs.documents.noResultsTitle}</div>
            <p className="text-[13px] text-muted text-pretty">{cs.documents.noResultsBody}</p>
          </div>
        </div>
      ) : (
        <DocumentCollection
          documents={items}
          view={p.view}
          onOpen={(id) => p.go('document', id)}
          folderLabelOf={(d) => (d.folder_id ? p.idx.folderNamePathById.get(d.folder_id) : rootLabel(p.scope))}
        />
      )}
    </div>
  )
}

// EmptyState is THREE states, not two.
//
// ⚠ The EMPTY PRIVATE ROOT is the state every member meets on day one, and it is
// the only place the feature ever gets to say what it is for — which is why the
// copy for it was written. Falling back to the shared "Zatím žádné dokumenty"
// there says nothing about who can see the tree, and leaves Dokumenty out of step
// with Poznámky, whose EmptyRoot has done this since the branch started (D40: one
// behaviour, two implementations).
function EmptyState({
  root,
  scope,
  canWrite,
  onUpload,
}: {
  root: boolean
  scope: Scope
  canWrite: boolean
  onUpload: () => void
}) {
  const priv = root && scope === 'private'
  return (
    <div className="grid place-items-center px-4 py-10 text-center">
      <div className="max-w-md">
        <div
          className={cn(
            'mx-auto mb-4 grid h-13 w-13 place-items-center rounded-[13px] border border-dashed bg-s2 text-2xl',
            priv ? 'border-vis-private text-vis-private' : 'border-border-strong text-subtle',
          )}
          aria-hidden
        >
          {priv ? '🔒' : '▦'}
        </div>
        <div className="mb-1.5 text-lg font-bold">
          {priv ? cs.privacy.emptyDocumentsTitle : root ? cs.documents.emptyRootTitle : cs.documents.emptyFolderTitle}
        </div>
        <p className="mb-4 text-[13.5px] text-muted text-pretty">
          {priv ? cs.privacy.emptyDocumentsBody : root ? cs.documents.emptyRootBody : cs.documents.emptyFolderBody}
        </p>
        {canWrite && (
          <Button variant="primary" onClick={onUpload}>
            <Upload size={16} aria-hidden /> {cs.documents.upload}
          </Button>
        )}
      </div>
    </div>
  )
}
