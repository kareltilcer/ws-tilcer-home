import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, Copy, Sparkles, X } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { Button, Input, Spinner, Textarea } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { importPlants, promptTemplate } from '../api/endpoints'
import { toastGardenError, useInvalidateGarden } from '../api/hooks'
import type { GardenImportResult } from '../api/types'

// PROMPT → MODEL → PREVIEW → APPLY.
//
// This is the escape hatch from a forty-field form, and its whole job is to make
// pasting machine output feel SAFE rather than reckless. Three things do that:
//
//  1. dry_run is the default and the preview is not skippable. Nothing is
//     written until somebody has seen what will change.
//  2. THE FIELD-LEVEL DIFF on an update — old → new, per field — so "the model
//     helped" and "the model overwrote what I checked last spring" are visibly
//     different outcomes.
//  3. UNMAPPED FIELDS ARE REPORTED, never silently dropped. A field we do not
//     understand is stated as such; an unmappable ENUM is a refusal naming the
//     field and the value, because a quietly defaulted enum is a wrong crop
//     nobody ever notices.
//
// A twenty-crop array comes back as twenty rows, so three failures cost the
// other seventeen nothing.

type Step = 'prompt' | 'paste' | 'preview'

export function ImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const invalidate = useInvalidateGarden()
  const [step, setStep] = useState<Step>('prompt')
  const [pasted, setPasted] = useState('')
  const [model, setModel] = useState('')
  const [copied, setCopied] = useState(false)
  const [result, setResult] = useState<GardenImportResult | null>(null)

  const template = useQuery({
    queryKey: [...qk.gardenAll, 'prompt'],
    queryFn: () => promptTemplate(),
    enabled: open,
  })

  const parseBody = () => {
    try {
      return JSON.parse(pasted) as unknown
    } catch {
      toast.error('Vložený text není platný JSON.')
      return null
    }
  }

  const preview = useMutation({
    mutationFn: (body: unknown) => importPlants(body, { dryRun: true, model: model || undefined }),
    onSuccess: (res) => {
      setResult(res)
      setStep('preview')
    },
    onError: toastGardenError,
  })

  const apply = useMutation({
    mutationFn: (body: unknown) => importPlants(body, { dryRun: false, model: model || undefined }),
    onSuccess: (res) => {
      invalidate()
      toast.success(cs.garden.previewSummary(res.summary.created, res.summary.updated, res.summary.rejected))
      reset()
      onOpenChange(false)
    },
    onError: toastGardenError,
  })

  const reset = () => {
    setStep('prompt')
    setPasted('')
    setModel('')
    setResult(null)
    setCopied(false)
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => {
        if (!o) reset()
        onOpenChange(o)
      }}
      title={cs.garden.promptTitle}
    >
      {step === 'prompt' && (
        <div className="space-y-3">
          <p className="text-[13px] text-muted text-pretty">{cs.garden.promptBody}</p>

          {template.isPending ? (
            <div className="grid min-h-[120px] place-items-center">
              <Spinner />
            </div>
          ) : (
            <>
              <pre className="max-h-64 overflow-auto rounded-md border border-border bg-s2 p-3 font-mono text-[11px] whitespace-pre-wrap">
                {template.data?.prompt}
              </pre>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="secondary"
                  onClick={async () => {
                    await navigator.clipboard.writeText(template.data?.prompt ?? '')
                    setCopied(true)
                    setTimeout(() => setCopied(false), 2000)
                  }}
                >
                  {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
                  {copied ? cs.garden.promptCopied : cs.garden.promptCopy}
                </Button>
                <div className="flex-1" />
                <Button variant="primary" onClick={() => setStep('paste')}>
                  {cs.garden.promptPaste}
                </Button>
              </div>
            </>
          )}
        </div>
      )}

      {step === 'paste' && (
        <div className="space-y-3">
          <Textarea
            rows={10}
            value={pasted}
            onChange={(e) => setPasted(e.target.value)}
            placeholder='{ "name_cs": "rajče", … }'
            className="font-mono text-[12px]"
            autoFocus
          />
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.promptModel}</span>
            <Input value={model} onChange={(e) => setModel(e.target.value)} />
          </label>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setStep('prompt')}>
              {cs.finance.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={!pasted.trim() || preview.isPending}
              onClick={() => {
                const body = parseBody()
                if (body) preview.mutate(body)
              }}
            >
              {cs.garden.copyPreview}
            </Button>
          </div>
        </div>
      )}

      {step === 'preview' && result && (
        <div className="space-y-3">
          <p className="text-[12.5px] font-semibold">
            {cs.garden.previewSummary(result.summary.created, result.summary.updated, result.summary.rejected)}
          </p>
          <p className="text-[11.5px] text-subtle">Zatím se nic neuložilo.</p>

          <ul className="space-y-2">
            {result.items.map((item) => (
              <li
                key={item.index}
                className={cn(
                  'rounded-md border px-3 py-2.5',
                  item.ok ? 'border-border bg-s2' : 'border-danger/45 bg-danger/5',
                )}
              >
                <div className="flex items-center gap-2">
                  {item.ok ? (
                    <Check size={14} className="flex-none text-good" aria-hidden />
                  ) : (
                    <X size={14} className="flex-none text-danger" aria-hidden />
                  )}
                  <span className="min-w-0 flex-1 truncate text-[13px] font-semibold">
                    {item.name ?? `#${item.index + 1}`}
                  </span>
                  <span className="flex-none text-[11px] font-semibold text-muted">
                    {item.action === 'create'
                      ? cs.garden.previewCreate
                      : item.action === 'update'
                        ? cs.garden.previewUpdate
                        : cs.garden.previewReject}
                  </span>
                </div>

                {/* The field-level diff: what will CHANGE on a crop that already
                    exists, so "helped" and "overwrote" look different. */}
                {item.diff && Object.keys(item.diff).length > 0 && (
                  <dl className="mt-1.5 space-y-0.5">
                    {Object.entries(item.diff).map(([field, change]) => (
                      <div key={field} className="flex gap-2 font-mono text-[11px]">
                        <dt className="w-[150px] flex-none truncate text-subtle">{field}</dt>
                        <dd className="min-w-0 flex-1">
                          <span className="text-muted line-through">{change.old ?? '—'}</span>
                          <span className="mx-1 text-subtle">→</span>
                          <span className="font-semibold">{change.new ?? '—'}</span>
                        </dd>
                      </div>
                    ))}
                  </dl>
                )}

                {item.unmapped_fields && item.unmapped_fields.length > 0 && (
                  <p className="mt-1.5 text-[11.5px] text-subtle text-pretty">
                    {cs.garden.previewUnmapped}: {item.unmapped_fields.join(', ')} —{' '}
                    {cs.garden.previewUnmappedHint}
                  </p>
                )}

                {item.errors && item.errors.length > 0 && (
                  <ul className="mt-1.5 space-y-0.5">
                    {item.errors.map((err, i) => (
                      <li key={i} className="text-[12px] text-danger text-pretty">
                        {err}
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>

          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setStep('paste')}>
              {cs.finance.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={apply.isPending || result.summary.created + result.summary.updated === 0}
              onClick={() => {
                const body = parseBody()
                if (body) apply.mutate(body)
              }}
            >
              <Sparkles size={14} aria-hidden />
              {cs.garden.previewApply}
            </Button>
          </div>
        </div>
      )}
    </ResponsiveModal>
  )
}
