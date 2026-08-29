import { describe, expect, it } from 'vitest'

import { findNode, indexTree, subtreeCounts, type IndexableFolder, type IndexableItem } from './foldertree'

// A miniature of both modules' /tree shape. `items` stands in for `notes` /
// `documents`, which is the only field name the two trees genuinely disagree on.
interface TestNode {
  folder: IndexableFolder
  subfolders: TestNode[]
  items: TestItem[]
}
interface TestItem extends IndexableItem {
  title: string
}

const folder = (id: string, name: string, slug: string, position: string, icon = ''): IndexableFolder => ({
  id,
  name,
  slug,
  icon,
  position,
})
const item = (id: string, slug: string, position: string): TestItem => ({ id, slug, position, title: id })
const node = (f: IndexableFolder, subfolders: TestNode[] = [], items: TestItem[] = []): TestNode => ({
  folder: f,
  subfolders,
  items,
})

const opts = { itemsOf: (n: TestNode) => n.items, rootLabel: 'Kořen', defaultIcon: '📁' }

//   (root) ── a.md
//   Práce/                         [work]
//     ├─ porada.md
//     └─ Projekty/                 [proj]
//          ├─ plan.md
//          └─ Archiv/              [arch]  (empty)
//   Recepty/                       [rec]   (empty)
const build = () => {
  const arch = node(folder('arch', 'Archiv', 'archiv', 'a'))
  const proj = node(folder('proj', 'Projekty', 'projekty', 'b'), [arch], [item('plan', 'plan', 'a')])
  const work = node(folder('work', 'Práce', 'prace', 'a', '💼'), [proj], [item('porada', 'porada', 'a')])
  const rec = node(folder('rec', 'Recepty', 'recepty', 'b'))
  return indexTree([work, rec], [item('a', 'a', 'a')], opts)
}

describe('indexTree', () => {
  it('joins slug paths down the tree, for folders and items alike', () => {
    const idx = build()
    expect(idx.slugPathById.get('proj')).toBe('prace/projekty')
    expect(idx.slugPathById.get('plan')).toBe('prace/projekty/plan')
    // A root-level item has no parent segment to join to.
    expect(idx.slugPathById.get('a')).toBe('a')
  })

  it('joins name paths with the separator the breadcrumb renders', () => {
    expect(build().folderNamePathById.get('proj')).toBe('Práce / Projekty')
  })

  it('maps every item to its containing folder, null at the root', () => {
    const idx = build()
    expect(idx.folderIdByItemId.get('plan')).toBe('proj')
    expect(idx.folderIdByItemId.get('a')).toBeNull()
  })

  it('keys children on null for the root scope', () => {
    const idx = build()
    expect(idx.childFolders.get(null)?.map((n) => n.folder.id)).toEqual(['work', 'rec'])
    expect(idx.childItems.get(null)?.map((i) => i.id)).toEqual(['a'])
  })

  it('records positions per parent, in tree order', () => {
    const idx = build()
    expect(idx.folderPositions.get('work')).toEqual(['b'])
    expect(idx.itemPositions.get('work')).toEqual(['a'])
    expect(idx.itemPositions.get('arch')).toEqual([])
  })

  // ⚠ The map documents had and notes did not. It is what lets the move picker
  // refuse a folder's own descendants without walking the tree again.
  it('records the ancestor chain root-first, empty at the top level', () => {
    const idx = build()
    expect(idx.ancestorsById.get('work')).toEqual([])
    expect(idx.ancestorsById.get('proj')).toEqual(['work'])
    expect(idx.ancestorsById.get('arch')).toEqual(['work', 'proj'])
  })

  it('puts the root first in the move picker and substitutes the default icon', () => {
    const idx = build()
    expect(idx.flatFolders[0]).toEqual({ id: null, name: 'Kořen', depth: 0 })
    expect(idx.flatFolders.map((t) => [t.id, t.depth])).toEqual([
      [null, 0],
      ['work', 1],
      ['proj', 2],
      ['arch', 3],
      ['rec', 1],
    ])
    expect(idx.flatFolders.find((t) => t.id === 'work')?.icon).toBe('💼')
    expect(idx.flatFolders.find((t) => t.id === 'rec')?.icon).toBe('📁')
  })

  it('indexes an empty tree without inventing anything but the root row', () => {
    const idx = indexTree<TestNode, TestItem>([], [], opts)
    expect(idx.flatFolders).toEqual([{ id: null, name: 'Kořen', depth: 0 }])
    expect(idx.childFolders.get(null)).toEqual([])
    expect(idx.itemPositions.get(null)).toEqual([])
  })
})

describe('findNode', () => {
  it('finds a node at any depth', () => {
    expect(findNode(build(), 'arch')?.folder.name).toBe('Archiv')
  })

  it('returns null for a folder that is no longer in the tree', () => {
    expect(findNode(build(), 'gone')).toBeNull()
  })
})

describe('subtreeCounts', () => {
  // ⚠ THE WHOLE SUBTREE. Both pages' confirmations depend on this being the
  // descendant total rather than the direct-child count: Práce holds one note
  // directly and two more below it.
  it('counts every descendant folder and item, not the direct children', () => {
    expect(subtreeCounts(build(), 'work')).toEqual({ folders: 2, items: 2 })
  })

  it('counts a leaf as itself', () => {
    expect(subtreeCounts(build(), 'proj')).toEqual({ folders: 1, items: 1 })
    expect(subtreeCounts(build(), 'arch')).toEqual({ folders: 0, items: 0 })
  })

  // The dialog asks before it knows whether a folder is selected, and asks again
  // after a refetch may have removed it. Both must answer zero rather than throw.
  it('returns zeros for null and for an unknown id', () => {
    expect(subtreeCounts(build(), null)).toEqual({ folders: 0, items: 0 })
    expect(subtreeCounts(build(), 'gone')).toEqual({ folders: 0, items: 0 })
  })
})
