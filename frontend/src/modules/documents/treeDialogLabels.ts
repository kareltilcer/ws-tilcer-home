import type { DeleteDialogLabels, HardDeleteOption, MoveDialogLabels } from '@/components/common/TreeDialogs'
import { cs } from '@/i18n/cs'
import { PLURAL } from '@/i18n/plural'

// The Czech for the two dialogs Dokumenty shares with Poznámky
// (components/common/TreeDialogs). Bundles rather than a `cs` namespace, so the
// shared component never reaches into i18n and cannot pick the wrong noun.
export const moveLabels: MoveDialogLabels = {
  folderTitle: cs.documents.moveFolderInto,
  itemTitle: cs.documents.moveDocumentInto,
}

export const deleteLabels: DeleteDialogLabels = {
  cancel: cs.documents.cancel,
  confirm: cs.documents.confirmDelete,
  folderTitle: cs.documents.deleteFolderTitle,
  itemTitle: cs.documents.deleteDocumentTitle,
  itemBody: cs.documents.deleteDocumentBody,
  folderCascade: cs.documents.deleteFolderCascade,
  folderEmpty: cs.documents.deleteFolderEmpty,
  itemPlural: PLURAL.documents,
}

// The admin-only escalation from "archive this row" to "remove the stored file
// too". Opt-in, never the default — Poznámky has no equivalent, which is why the
// shared DeleteDialog takes it as an optional slot rather than a flag.
export const hardDeleteOption: HardDeleteOption = {
  label: cs.documents.hardDeleteLabel,
  hint: cs.documents.hardDeleteHint,
}
