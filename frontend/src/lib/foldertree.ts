/**
 * The folder-tree index Poznámky and Dokumenty both build from their /tree
 * response, and the two lookups that read it.
 *
 * ⚠ THIS IS NOT THE notes⇄documents MERGE. The two modules stay two modules
 * (v4 D40, and the recorded decision in `documents/scope.go`); what is shared here
 * is the part that never knew what a note or a document was in the first place —
 * walking a tree of folders, keying its rows by id, and joining the slug segments
 * on the way down. `indexTree` was written twice and drifted: one copy grew an
 * ancestor map the other never got, so the two pages ended up computing "may this
 * folder move here?" two different ways.
 *
 * The index holds ITEMS, not notes or documents. Each module names its own row
 * type and reaches its node's array through `itemsOf`, which is the only thing
 * the two trees genuinely spell differently (`node.notes` / `node.documents`).
 */

/** One row of the move picker's flat folder list; `id: null` is the root entry. */
export type MoveTarget = { id: string | null; name: string; depth: number; icon?: string }

/** The folder columns the index reads. `Folder` and `DocFolder` both satisfy it. */
export interface IndexableFolder {
  id: string
  name: string
  slug: string
  icon: string
  position: string
}

/** The item columns the index reads. `NoteSummary` and `DocumentSummary` both satisfy it. */
export interface IndexableItem {
  id: string
  slug: string
  position: string
}

export interface FolderTreeIndex<TFolder extends IndexableFolder, TNode, TItem> {
  folderById: Map<string, TFolder>
  itemById: Map<string, TItem>
  /** id → slug path, for folders AND items ("smlouvy/energie/cez"). */
  slugPathById: Map<string, string>
  /** folder id → human name path ("Práce / Projekty"). */
  folderNamePathById: Map<string, string>
  /** item id → containing folder id; null means the root. */
  folderIdByItemId: Map<string, string | null>
  childFolders: Map<string | null, TNode[]>
  childItems: Map<string | null, TItem[]>
  itemPositions: Map<string | null, string[]>
  folderPositions: Map<string | null, string[]>
  /** folder id → its ancestor chain, root first. Empty for a root-level folder. */
  ancestorsById: Map<string, string[]>
  /** Move-picker targets, root first, in tree order. */
  flatFolders: MoveTarget[]
}

export interface IndexOptions<TNode, TItem> {
  /** The items directly inside a node — `n.notes` / `n.documents`. */
  itemsOf: (node: TNode) => TItem[]
  /** What the move picker calls the root ("Poznámky" / "Kořenová složka"). */
  rootLabel: string
  /** Icon for a folder that stored none, so the picker never renders a blank cell. */
  defaultIcon: string
}

/**
 * indexTree walks the tree once and builds every lookup the page needs.
 *
 * ⚠ ONE WALK, deliberately. The page re-derives the index on every /tree response
 * and then answers "what is this folder's slug path", "which folder holds this
 * item", "what is the next position among these siblings" per render — each of
 * which is a full descent if it is not a map lookup.
 */
export function indexTree<
  TNode extends { folder: IndexableFolder; subfolders: TNode[] },
  TItem extends IndexableItem,
>(
  roots: TNode[],
  rootItems: TItem[],
  opts: IndexOptions<TNode, TItem>,
): FolderTreeIndex<TNode['folder'], TNode, TItem> {
  type TFolder = TNode['folder']
  const folderById = new Map<string, TFolder>()
  const itemById = new Map<string, TItem>()
  const slugPathById = new Map<string, string>()
  const folderNamePathById = new Map<string, string>()
  const folderIdByItemId = new Map<string, string | null>()
  const childFolders = new Map<string | null, TNode[]>()
  const childItems = new Map<string | null, TItem[]>()
  const ancestorsById = new Map<string, string[]>()
  const flatFolders: MoveTarget[] = [{ id: null, name: opts.rootLabel, depth: 0 }]

  const walk = (nodes: TNode[], parentPath: string, parentNamePath: string, ancestors: string[], depth: number) => {
    for (const node of nodes) {
      const f = node.folder as TFolder
      const items = opts.itemsOf(node)
      folderById.set(f.id, f)
      const path = parentPath ? `${parentPath}/${f.slug}` : f.slug
      const namePath = parentNamePath ? `${parentNamePath} / ${f.name}` : f.name
      slugPathById.set(f.id, path)
      folderNamePathById.set(f.id, namePath)
      ancestorsById.set(f.id, ancestors)
      childFolders.set(f.id, node.subfolders)
      childItems.set(f.id, items)
      flatFolders.push({ id: f.id, name: f.name, depth, icon: f.icon || opts.defaultIcon })
      for (const it of items) {
        itemById.set(it.id, it)
        slugPathById.set(it.id, path ? `${path}/${it.slug}` : it.slug)
        folderIdByItemId.set(it.id, f.id)
      }
      walk(node.subfolders, path, namePath, [...ancestors, f.id], depth + 1)
    }
  }
  walk(roots, '', '', [], 1)

  childFolders.set(null, roots)
  childItems.set(null, rootItems)
  for (const it of rootItems) {
    itemById.set(it.id, it)
    slugPathById.set(it.id, it.slug)
    folderIdByItemId.set(it.id, null)
  }

  const itemPositions = new Map<string | null, string[]>()
  const folderPositions = new Map<string | null, string[]>()
  for (const [k, v] of childItems) itemPositions.set(k, v.map((i) => i.position))
  for (const [k, v] of childFolders) folderPositions.set(k, v.map((n) => n.folder.position))

  return {
    folderById,
    itemById,
    slugPathById,
    folderNamePathById,
    folderIdByItemId,
    childFolders,
    childItems,
    itemPositions,
    folderPositions,
    ancestorsById,
    flatFolders,
  }
}

/**
 * findNode re-resolves a folder's live tree node by id.
 *
 * ⚠ Callers must NOT hold a node captured when a dialog opened. A background
 * refetch reshapes the tree, and a stale node understates what a delete is about
 * to take. Look it up again at render time; null means it is gone, which is
 * itself the honest answer.
 */
export function findNode<TFolder extends IndexableFolder, TNode extends { folder: TFolder; subfolders: TNode[] }, TItem>(
  idx: FolderTreeIndex<TFolder, TNode, TItem>,
  folderId: string,
): TNode | null {
  const search = (nodes: TNode[]): TNode | null => {
    for (const n of nodes) {
      if (n.folder.id === folderId) return n
      const hit = search(n.subfolders)
      if (hit) return hit
    }
    return null
  }
  return search(idx.childFolders.get(null) ?? [])
}

/**
 * subtreeCounts totals a folder's WHOLE SUBTREE — every descendant folder and
 * item, at any depth — and returns zeros for a folder that is not in the index.
 *
 * ⚠ Direct children are the wrong number for a confirmation, and dangerously so.
 * Both the delete cascade and the publish walk the entire subtree in one
 * transaction, and publish has no undo (D182), so a folder holding nothing
 * directly but one subfolder of forty reads as "0 items in 1 subfolder" — the
 * dialog understating the exact thing it exists to state.
 */
export function subtreeCounts<TFolder extends IndexableFolder, TNode extends { folder: TFolder; subfolders: TNode[] }, TItem>(
  idx: FolderTreeIndex<TFolder, TNode, TItem>,
  folderId: string | null,
): { folders: number; items: number } {
  if (folderId === null) return { folders: 0, items: 0 }
  let items = (idx.childItems.get(folderId) ?? []).length
  let folders = 0
  for (const child of idx.childFolders.get(folderId) ?? []) {
    const sub = subtreeCounts(idx, child.folder.id)
    folders += 1 + sub.folders
    items += sub.items
  }
  return { folders, items }
}
