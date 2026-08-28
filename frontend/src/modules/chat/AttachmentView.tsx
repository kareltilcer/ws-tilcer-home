import { useState } from 'react'
import { Download, FileText, Film, Image as ImageIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { fmtBytes, fmtDate } from '@/i18n/format'
import { attachmentURL, thumbnailURL } from './api/endpoints'
import type { Attachment } from './api/types'

/**
 * The three attachment states, and two of them are text (D243/D246).
 *
 * ⚠ `live` SHOWS THE FILE · `removed` IS A SETTLED ABSENCE · `moved` IS A FACT.
 * None of the three is an error, and none of them should be drawn as one: a removed
 * file is not a failed load, and a moved file is not a warning. The `removed` state
 * borrows v9's `--unmeasured` register — the treatment for a thing that is
 * deliberately not there — because it is the same idea, and reusing it keeps one
 * meaning rather than inventing a second.
 *
 * ⚠ AN IMAGE RESERVES ITS BOX BEFORE THE BYTES ARRIVE. That is what the recorded
 * intrinsic dimensions are for and it is a design requirement rather than polish: a
 * thread that jumps while somebody is reading it is the most-noticed bug in any
 * chat (HANDOFF-design §v10). `aspect-ratio` from width/height does it with no
 * layout thrash and no second render.
 */
export function AttachmentView({ attachment: a }: { attachment: Attachment }) {
  if (a.state === 'removed') return <RemovedAttachment attachment={a} />
  if (a.state === 'moved') return <MovedAttachment attachment={a} />
  if (a.kind === 'image') return <ImageAttachment attachment={a} />
  if (a.kind === 'video') return <VideoAttachment attachment={a} />
  return <FileAttachment attachment={a} />
}

/**
 * ⚠ THE FALLBACK STEPS DOWN TWICE, and collapsing it to one step lost an image that
 * was perfectly readable. A missing THUMBNAIL and a missing ORIGINAL are different
 * failures: the first happens whenever cwebp was unavailable at upload, or the
 * derived object was lost, and `/raw` still serves the full picture — so dropping
 * straight to an icon-and-filename row threw away the thing the reserved box was
 * sized for. Only when the ORIGINAL will not load is there nothing left to render.
 */
function ImageAttachment({ attachment: a }: { attachment: Attachment }) {
  const [step, setStep] = useState<'thumb' | 'full' | 'gone'>(
    a.has_thumbnail ? 'thumb' : 'full',
  )
  if (step === 'gone') return <FileAttachment attachment={a} />
  return (
    <a
      href={attachmentURL(a.id)}
      target="_blank"
      rel="noreferrer"
      className="block overflow-hidden rounded-md border border-border/60 bg-s2"
      // ⚠ THE RESERVED BOX. Without the ratio the bubble is 0 px tall until the
      // image decodes and the whole thread jumps under the reader's finger.
      style={a.width && a.height ? { aspectRatio: `${a.width} / ${a.height}` } : undefined}
    >
      <img
        // `key` forces a fresh element on the step down, so the browser actually
        // re-requests instead of keeping the errored one and never firing load.
        key={step}
        src={step === 'thumb' ? thumbnailURL(a.id) : attachmentURL(a.id)}
        alt={a.original_filename}
        loading="lazy"
        width={a.width ?? undefined}
        height={a.height ?? undefined}
        onError={() => setStep((s) => (s === 'thumb' ? 'full' : 'gone'))}
        className="h-full w-full object-cover"
      />
    </a>
  )
}

/**
 * ⚠ AN IPHONE `.mov` STORES FINE AND MAY NOT PLAY, and there is no transcoding in
 * v10 and no plan for one (D227). So the fallback is DESIGNED — a download and one
 * sentence saying why — rather than a broken player with a slash through it, which
 * is what a browser draws when it cannot decode the stream.
 */
function VideoAttachment({ attachment: a }: { attachment: Attachment }) {
  const [unplayable, setUnplayable] = useState(false)
  if (unplayable) {
    return (
      <div className="rounded-md border border-border/60 bg-s2 p-3">
        <div className="mb-1 flex items-center gap-2 text-sm font-semibold">
          <Film size={16} className="flex-none text-muted" aria-hidden />
          <span className="min-w-0 truncate">{a.original_filename}</span>
        </div>
        <p className="mb-2 text-xs text-muted text-pretty">{cs.chat.videoUnplayable}</p>
        <DownloadLink attachment={a} />
      </div>
    )
  }
  return (
    <video
      controls
      preload="metadata"
      onError={() => setUnplayable(true)}
      className="max-h-80 w-full rounded-md border border-border/60 bg-black"
    >
      <source src={attachmentURL(a.id)} type={a.content_type} />
    </video>
  )
}

/**
 * Icon, filename, size, download.
 *
 * ⚠ A PDF OPENS IN THE BROWSER'S OWN VIEWER — there is no preview pipeline in chat
 * (D227) — so the affordance is "opens elsewhere" rather than a preview pane.
 */
function FileAttachment({ attachment: a }: { attachment: Attachment }) {
  const Icon = a.kind === 'video' ? Film : a.kind === 'image' ? ImageIcon : FileText
  return (
    <a
      href={attachmentURL(a.id)}
      target="_blank"
      rel="noreferrer"
      className="flex items-center gap-2.5 rounded-md border border-border/60 bg-s2 px-3 py-2 hover:border-accent/50"
    >
      <Icon size={18} className="flex-none text-muted" aria-hidden />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold">{a.original_filename}</span>
        <span className="block text-[11px] tabular-nums text-muted">{fmtBytes(a.byte_size)}</span>
      </span>
      <Download size={15} className="flex-none text-muted" aria-hidden />
    </a>
  )
}

function DownloadLink({ attachment: a }: { attachment: Attachment }) {
  return (
    <a
      href={attachmentURL(a.id, { download: true })}
      className="inline-flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs font-semibold hover:bg-s3"
    >
      <Download size={13} aria-hidden />
      {cs.chat.download}
    </a>
  )
}

/**
 * The epitaph (D243).
 *
 * ⚠ IT KEEPS THE FILENAME AND THE SIZE ON PURPOSE, and names who removed it and
 * when. The thread stays legible, a member can ask for the file again knowing
 * exactly what it was, and the clean-up is attributed. Only the bytes went.
 */
function RemovedAttachment({ attachment: a }: { attachment: Attachment }) {
  return (
    <div
      className={cn(
        'flex items-start gap-2.5 rounded-md border border-dashed border-border px-3 py-2',
        'text-att-removed',
      )}
    >
      <FileText size={16} className="mt-0.5 flex-none" aria-hidden />
      <div className="min-w-0 text-xs text-pretty">
        <span className="font-semibold">{a.original_filename}</span>
        {' · '}
        <span className="tabular-nums">{fmtBytes(a.byte_size)}</span>
        <span className="block">
          {cs.chat.attachmentRemoved}
          {a.cleaned_by_label && ` · ${a.cleaned_by_label}`}
          {a.cleaned_at && `, ${fmtDate(new Date(a.cleaned_at))}`}
        </span>
      </div>
    </div>
  )
}

/**
 * A moved file still renders — from Dokumenty — with a quiet marker saying where it
 * lives now (D246).
 *
 * ⚠ NOT A WARNING. Nothing went wrong and nothing is missing: the bytes moved into
 * the household tree, which is the accepted publish (D245) the dialog stated before
 * it happened. The marker is neutral body-adjacent text, never a badge.
 */
function MovedAttachment({ attachment: a }: { attachment: Attachment }) {
  const href = a.document_path ?? undefined
  return (
    <div className="rounded-md border border-border/60 bg-s2 px-3 py-2">
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className={cn('flex items-center gap-2.5', !href && 'pointer-events-none opacity-70')}
      >
        <FileText size={18} className="flex-none text-muted" aria-hidden />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold">{a.original_filename}</span>
          <span className="block text-[11px] tabular-nums text-muted">{fmtBytes(a.byte_size)}</span>
        </span>
        <Download size={15} className="flex-none text-muted" aria-hidden />
      </a>
      <p className="mt-1.5 text-[11px] text-muted">{cs.chat.attachmentMoved}</p>
    </div>
  )
}
