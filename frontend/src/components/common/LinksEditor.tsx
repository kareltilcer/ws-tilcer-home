import { useState } from 'react'
import { ExternalLink, Plus, Trash2 } from 'lucide-react'
import { Button, Input } from '@/components/ui/ui'

export interface EditableLink {
  id: string
  url: string
  title: string | null
}

// LinksEditor is the shared link list+editor for cards AND events (identical
// {url, title?} shape). Enforces the http/https scheme allowlist client-side,
// mirroring the API's 422.
export function LinksEditor({
  links,
  onAdd,
  onRemove,
  readOnly,
}: {
  links: EditableLink[]
  onAdd: (url: string, title: string) => void
  onRemove: (id: string) => void
  readOnly?: boolean
}) {
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [error, setError] = useState('')

  const submit = () => {
    const trimmed = url.trim()
    if (!/^https?:\/\/.+/i.test(trimmed)) {
      setError('Zadejte platnou http(s) adresu.')
      return
    }
    onAdd(trimmed, title.trim())
    setUrl('')
    setTitle('')
    setError('')
  }

  return (
    <div className="space-y-2">
      {links.length === 0 && <p className="text-sm text-subtle">Žádné odkazy.</p>}
      <ul className="space-y-1.5">
        {links.map((l) => (
          <li key={l.id} className="flex items-center gap-2 rounded-md border border-border bg-s2 px-3 py-2">
            <ExternalLink size={15} className="flex-none text-muted" aria-hidden />
            <a
              href={l.url}
              target="_blank"
              rel="noopener noreferrer"
              className="min-w-0 flex-1 truncate text-sm text-accent hover:underline"
            >
              {l.title || l.url}
            </a>
            {!readOnly && (
              <button
                type="button"
                onClick={() => onRemove(l.id)}
                className="flex-none text-subtle hover:text-danger"
                aria-label="Odebrat odkaz"
              >
                <Trash2 size={15} aria-hidden />
              </button>
            )}
          </li>
        ))}
      </ul>
      {!readOnly && (
        <div className="space-y-1.5 pt-1">
          <div className="flex flex-col gap-1.5 sm:flex-row">
            <Input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://…"
              aria-label="Adresa odkazu"
            />
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Popisek (nepovinné)"
              aria-label="Popisek odkazu"
              className="sm:max-w-[40%]"
            />
            <Button size="sm" variant="secondary" onClick={submit} className="flex-none">
              <Plus size={14} aria-hidden /> Přidat
            </Button>
          </div>
          {error && <p className="text-[13px] text-danger">{error}</p>}
        </div>
      )}
    </div>
  )
}
