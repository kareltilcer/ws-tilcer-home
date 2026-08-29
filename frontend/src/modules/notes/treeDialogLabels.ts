import type { DeleteDialogLabels, MoveDialogLabels } from '@/components/common/TreeDialogs'
import { cs } from '@/i18n/cs'
import { PLURAL } from '@/i18n/plural'

// The Czech for the two dialogs Poznámky shares with Dokumenty
// (components/common/TreeDialogs). Bundles rather than a `cs` namespace, so the
// shared component never reaches into i18n and cannot pick the wrong noun.
export const moveLabels: MoveDialogLabels = {
  folderTitle: cs.notes.moveFolderInto,
  itemTitle: cs.notes.moveNoteInto,
}

export const deleteLabels: DeleteDialogLabels = {
  cancel: cs.notes.cancel,
  confirm: cs.notes.confirmDelete,
  folderTitle: cs.notes.deleteFolderTitle,
  itemTitle: cs.notes.deleteNoteTitle,
  itemBody: cs.notes.deleteNoteBody,
  folderCascade: cs.notes.deleteFolderCascade,
  folderEmpty: cs.notes.deleteFolderEmpty,
  itemPlural: PLURAL.notes,
}
