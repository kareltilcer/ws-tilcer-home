import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cs } from '@/i18n/cs'
import type { DocumentDetail } from './api/types'
import { DocumentView } from './DocumentView'

// THE PDF FRAME'S SANDBOX. The token list is a security boundary (D48) that is also
// load-bearing for the reader, and the two pulls are in opposite directions — which
// is why it is pinned rather than left to the comment beside it. Too strict and the
// preview is dead on a phone: Chrome for Android renders no PDF in a frame at all,
// only its own "Open" placeholder, which hands the file to the platform viewer by
// starting a download — refused, silently, when allow-downloads is missing. Too
// loose and the frame stops being isolated: allow-same-origin would put arbitrary
// uploaded bytes inside home's origin, with its cookies and its DOM.
//
// The header half of the same contract lives in the Go test (TestHTTP_PreviewStates).
// Both must carry a token for it to have any effect — the attribute and the response
// CSP are ANDed — so each half is pinned where it is written.

const getDocument = vi.hoisted(() => vi.fn())
vi.mock('./api/endpoints', () => ({
  getDocument,
  pinDocument: vi.fn(),
  unpinDocument: vi.fn(),
}))

vi.mock('@/app/auth', () => ({
  useAuth: () => ({
    canWrite: true,
    isAdmin: false,
    identity: { userId: 'u1', email: 'k@example.com', label: 'Kája', roles: ['write'] },
    logout: () => {},
  }),
}))

const DOC_ID = 'd1'

const doc = (overrides: Partial<DocumentDetail> = {}): DocumentDetail => ({
  id: DOC_ID,
  folder_id: null,
  title: 'Smlouva',
  slug: 'smlouva',
  description: null,
  original_filename: 'smlouva.pdf',
  content_type: 'application/pdf',
  byte_size: 12_345,
  checksum: 'abc',
  preview_kind: 'native',
  preview_status: 'ready',
  position: 'a0',
  archived: false,
  visibility: 'shared',
  owner_id: null,
  created_by: 'u1',
  created_at: '2026-08-30T10:00:00Z',
  updated_at: '2026-08-30T10:00:00Z',
  path: [],
  slug_path: 'smlouva',
  pinned: { household: false, personal: false },
  urls: {
    permalink: `/d/${DOC_ID}`,
    raw: `/api/documents/${DOC_ID}/raw`,
    download: `/api/documents/${DOC_ID}/download`,
    preview: `/api/documents/${DOC_ID}/preview`,
    thumbnail: `/api/documents/${DOC_ID}/thumbnail`,
  },
  ...overrides,
})

function renderDocument() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <DocumentView documentId={DOC_ID} />
    </QueryClientProvider>,
  )
}

const frame = () => screen.queryByTitle(`${cs.documents.previewFrameLabel}: Smlouva`)

describe('DocumentView PDF preview', () => {
  beforeEach(() => {
    getDocument.mockReset()
  })

  it('frames the PDF with the tokens its viewer and the Android placeholder need', async () => {
    getDocument.mockResolvedValue(doc())
    renderDocument()

    await waitFor(() => expect(frame()).not.toBeNull())
    const tokens = frame()!.getAttribute('sandbox')!.split(/\s+/)
    expect(tokens).toContain('allow-scripts')
    expect(tokens).toContain('allow-downloads')
  })

  it('never grants the frame same-origin access', async () => {
    getDocument.mockResolvedValue(doc())
    renderDocument()

    await waitFor(() => expect(frame()).not.toBeNull())
    expect(frame()!.getAttribute('sandbox')).not.toContain('allow-same-origin')
  })

  it('offers the new-tab fallback for the browsers that refuse the frame', async () => {
    getDocument.mockResolvedValue(doc())
    renderDocument()

    const link = await screen.findByRole('link', { name: cs.documents.previewOpenInTab })
    expect(link).toHaveAttribute('href', `/api/documents/${DOC_ID}/preview`)
  })

  // The relaxation is scoped to the PDF renderer. An image is drawn by the app in an
  // <img>, so nothing here should have given the other viewers a frame to relax.
  it('renders an image without a frame at all', async () => {
    getDocument.mockResolvedValue(doc({ content_type: 'image/png', original_filename: 'plan.png' }))
    const { container } = renderDocument()

    await waitFor(() => expect(screen.getByAltText('Smlouva')).toBeInTheDocument())
    expect(container.querySelector('iframe')).toBeNull()
  })
})
