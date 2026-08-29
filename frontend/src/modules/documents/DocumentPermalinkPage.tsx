import { useParams } from 'react-router-dom'
import { DocumentView } from './DocumentView'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { cs } from '@/i18n/cs'

// /d/{id} — the PERMANENT link to a document (D42).
//
// It works by id, so it survives renames and moves that change the slug path, and
// it is household-only like every other documents route: an unauthenticated visitor
// is sent to the login screen by the session bootstrap, exactly as with /dokumenty.
// This is what "Kopírovat odkaz" copies.
export function DocumentPermalinkPage() {
  const { id = '' } = useParams()
  useDocumentTitle(cs.documents.title)
  return (
    <div className="-mx-4 -my-5 flex min-h-[calc(100vh-8rem)] flex-col md:-mx-8 md:-my-8">
      <DocumentView key={id} documentId={id} />
    </div>
  )
}
