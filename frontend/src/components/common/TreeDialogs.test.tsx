import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PLURAL } from '@/i18n/plural'
import { DeleteDialog, MoveDialog, type DeleteDialogLabels, type MoveDialogLabels } from './TreeDialogs'

// The two dialogs Poznámky and Dokumenty now share. What is pinned here is the
// part that DRIFTED while there were two copies: the 44px touch target and the
// aria-hidden on the decorative marks reached the Dokumenty copy only, and the
// hard-delete escalation exists on one side and must stay absent on the other.

const moveLabels: MoveDialogLabels = { folderTitle: 'Přesunout složku do…', itemTitle: 'Přesunout položku do…' }

const deleteLabels: DeleteDialogLabels = {
  cancel: 'Zrušit',
  confirm: 'Smazat',
  folderTitle: (t) => `Smazat složku „${t}“?`,
  itemTitle: (t) => `Smazat položku „${t}“?`,
  itemBody: 'Položku nelze vrátit zpět.',
  folderCascade: 'Smaže se i obsah složky.',
  folderEmpty: 'Prázdnou složku lze bezpečně smazat.',
  itemPlural: PLURAL.notes,
}

const targets = [
  { id: null, name: 'Kořen', depth: 0 },
  { id: 'work', name: 'Práce', depth: 1, icon: '💼' },
]

describe('MoveDialog', () => {
  it('titles itself by what is moving', () => {
    const { unmount } = render(
      <MoveDialog isFolder title="Porada" targets={targets} labels={moveLabels} pending={false} onPick={() => {}} onClose={() => {}} />,
    )
    expect(screen.getByText('Přesunout složku do…')).toBeInTheDocument()
    unmount()
    render(<MoveDialog isFolder={false} title="Porada" targets={targets} labels={moveLabels} pending={false} onPick={() => {}} onClose={() => {}} />)
    expect(screen.getByText('Přesunout položku do…')).toBeInTheDocument()
  })

  // ⚠ THE DRIFT. min-h-11 is the 44px touch target; it reached Dokumenty and never
  // Poznámky. Adopting the shared dialog is what gives both rows the same size.
  it('gives every target row the 44px touch target', () => {
    render(<MoveDialog isFolder title="Porada" targets={targets} labels={moveLabels} pending={false} onPick={() => {}} onClose={() => {}} />)
    for (const name of ['Kořen', 'Práce']) {
      expect(screen.getByRole('button', { name }).className).toContain('min-h-11')
    }
  })

  // The icon repeats the name beside it, and the ▸ stands in for an icon that is
  // not there. A screen reader that announces both reads every row twice.
  it('hides the decorative icon from assistive technology', () => {
    render(<MoveDialog isFolder title="Porada" targets={targets} labels={moveLabels} pending={false} onPick={() => {}} onClose={() => {}} />)
    for (const name of ['Kořen', 'Práce']) {
      const row = screen.getByRole('button', { name })
      expect(row.querySelector('[aria-hidden]')).not.toBeNull()
      // The name itself stays readable — it is what the row is announced as.
      expect(row).toHaveAccessibleName(name)
    }
  })

  it('indents by depth and reports the picked id, null for the root', async () => {
    const onPick = vi.fn()
    render(<MoveDialog isFolder title="Porada" targets={targets} labels={moveLabels} pending={false} onPick={onPick} onClose={() => {}} />)
    await userEvent.click(screen.getByText('Kořen'))
    expect(onPick).toHaveBeenCalledWith(null)
    await userEvent.click(screen.getByText('Práce'))
    expect(onPick).toHaveBeenCalledWith('work')
  })

  it('refuses picks while a move is in flight', async () => {
    const onPick = vi.fn()
    render(<MoveDialog isFolder title="Porada" targets={targets} labels={moveLabels} pending onPick={onPick} onClose={() => {}} />)
    await userEvent.click(screen.getByText('Práce'))
    expect(onPick).not.toHaveBeenCalled()
  })
})

describe('DeleteDialog', () => {
  const base = {
    title: 'Porada',
    labels: deleteLabels,
    pending: false,
    onConfirm: () => {},
    onClose: () => {},
  }

  it('shows the item body for an item, and confirms without a cascade', async () => {
    const onConfirm = vi.fn()
    render(<DeleteDialog {...base} isFolder={false} subfolders={0} items={0} onConfirm={onConfirm} />)
    expect(screen.getByText('Smazat položku „Porada“?')).toBeInTheDocument()
    expect(screen.getByText('Položku nelze vrátit zpět.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Smazat' }))
    expect(onConfirm).toHaveBeenCalledWith({ cascade: false, hard: false })
  })

  it('calls an empty folder safe and still sends cascade:false', async () => {
    const onConfirm = vi.fn()
    render(<DeleteDialog {...base} isFolder subfolders={0} items={0} onConfirm={onConfirm} />)
    expect(screen.getByText('Prázdnou složku lze bezpečně smazat.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Smazat' }))
    expect(onConfirm).toHaveBeenCalledWith({ cascade: false, hard: false })
  })

  // The counts are what the CALLER passed. Whether they describe the subtree or the
  // direct children is each page's decision, not this component's.
  it('spells the cascade counts with the module noun and sends cascade:true', async () => {
    const onConfirm = vi.fn()
    render(<DeleteDialog {...base} isFolder subfolders={2} items={5} onConfirm={onConfirm} />)
    expect(screen.getByText('2 složky · 5 poznámek')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Smazat' }))
    expect(onConfirm).toHaveBeenCalledWith({ cascade: true, hard: false })
  })

  it('omits a zero count rather than writing "0"', () => {
    render(<DeleteDialog {...base} isFolder subfolders={0} items={3} />)
    expect(screen.getByText('3 poznámky')).toBeInTheDocument()
  })

  // ⚠ Poznámky has no hard delete. The slot being ABSENT is the guarantee that
  // sharing this dialog did not hand it one.
  it('offers no hard delete unless the module supplies the option', () => {
    render(<DeleteDialog {...base} isFolder={false} subfolders={0} items={0} />)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })

  it('escalates to a hard delete only when the box is ticked', async () => {
    const onConfirm = vi.fn()
    render(
      <DeleteDialog
        {...base}
        isFolder={false}
        subfolders={0}
        items={0}
        hardDelete={{ label: 'Smazat trvale i soubor', hint: 'Nevratné.' }}
        onConfirm={onConfirm}
      />,
    )
    const box = screen.getByRole('checkbox')
    expect(box).not.toBeChecked()
    await userEvent.click(screen.getByRole('button', { name: 'Smazat' }))
    expect(onConfirm).toHaveBeenLastCalledWith({ cascade: false, hard: false })
    await userEvent.click(box)
    await userEvent.click(screen.getByRole('button', { name: 'Smazat' }))
    expect(onConfirm).toHaveBeenLastCalledWith({ cascade: false, hard: true })
  })

  it('closes without confirming', async () => {
    const onConfirm = vi.fn()
    const onClose = vi.fn()
    render(<DeleteDialog {...base} isFolder={false} subfolders={0} items={0} onConfirm={onConfirm} onClose={onClose} />)
    await userEvent.click(screen.getByRole('button', { name: 'Zrušit' }))
    expect(onClose).toHaveBeenCalled()
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
