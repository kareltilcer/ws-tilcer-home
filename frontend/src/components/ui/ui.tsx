import { forwardRef, type ButtonHTMLAttributes, type InputHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
type ButtonSize = 'sm' | 'md'

const buttonVariants: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-accent-fg hover:opacity-90 border border-accent',
  secondary: 'bg-s2 text-fg border border-border hover:bg-s3',
  ghost: 'bg-transparent text-muted hover:bg-s2 hover:text-fg border border-transparent',
  danger: 'bg-transparent text-danger border border-border hover:bg-danger/10',
}

const buttonSizes: Record<ButtonSize, string> = {
  sm: 'h-8 px-2.5 text-[13px] gap-1.5',
  md: 'h-10 px-4 text-sm gap-2',
}

const buttonBase =
  'inline-flex items-center justify-center rounded-md font-semibold transition-colors disabled:opacity-50 disabled:pointer-events-none'

/** buttonClass exposes the button styling for the cases that must NOT be a
 *  <button>: a download link, for instance, has to be an <a href> so the browser
 *  handles the Content-Disposition response itself. */
export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'md', className?: string): string {
  return cn(buttonBase, buttonVariants[variant], buttonSizes[size], className)
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading, className, children, disabled, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      disabled={disabled || loading}
      className={buttonClass(variant, size, className)}
      {...props}
    >
      {loading && <Loader2 className="animate-spin" size={size === 'sm' ? 14 : 16} aria-hidden />}
      {children}
    </button>
  )
})

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input(
  { className, ...props },
  ref,
) {
  return (
    <input
      ref={ref}
      className={cn(
        'h-10 w-full rounded-md border border-border bg-s1 px-3 text-sm text-fg placeholder:text-subtle',
        'focus-visible:outline-2 focus-visible:outline-focus',
        className,
      )}
      {...props}
    />
  )
})

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea(
  { className, ...props },
  ref,
) {
  return (
    <textarea
      ref={ref}
      className={cn(
        'min-h-[120px] w-full rounded-md border border-border bg-s1 p-3 text-sm leading-relaxed text-fg placeholder:text-subtle',
        'focus-visible:outline-2 focus-visible:outline-focus',
        className,
      )}
      {...props}
    />
  )
})

/** Field is a labelled form row: the label text above its control, wrapped in a
 *  <label> so tapping the text focuses the control.
 *
 *  ⚠ IT LIVES HERE BECAUSE IT EXISTED FIVE TIMES, byte-identical, once in each
 *  electricity dialog — and a sixth copy was the obvious thing to write for the
 *  next dialog.
 *
 *  ⚠ THE OTHER LABELS THAT LOOK LIKE THIS ARE NOT THIS ONE. Twelve sites, in
 *  three groups, and each group breaks a different way:
 *
 *    - `EventForm` and `ColumnMenu` declare a `Field` of their OWN that renders
 *      a <div> at a different type scale.
 *    - `AdministracePage` (×5), `Composer` (×2) and `ScheduleBuilder`'s time
 *      field space the label at `mb-1`, so adopting this moves it by 2px.
 *    - `ConditionsBuilder`, `AudiencePicker` and `ScheduleBuilder`'s day picker
 *      are bare <div>s, and garden's `PlanTab` season picker is an inline-flex
 *      <label> whose span has neither `block` nor a margin. Wrapping a builder
 *      full of <select>s in this <label> changes what a tap focuses.
 *
 *  Every one of those is a visual change wearing a refactor's clothes. Worth
 *  doing one day, but as a design decision. */
export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[12.5px] font-semibold text-muted">{label}</span>
      {children}
    </label>
  )
}

/** FormError is the inline refusal a dialog shows under its fields — the one
 *  the server wrote, not a toast. `role="alert"` is the load-bearing part: the
 *  message appears while focus is still in the form, so a screen reader has to
 *  announce it without being sent there.
 *
 *  ⚠ IT LIVES HERE BECAUSE IT EXISTED FIVE TIMES, byte-identical, once in each
 *  electricity dialog.
 *
 *  ⚠ `LoginScreen`'s two alerts are NOT this component. They are <div>s at a
 *  different radius, carry `mb-4`, and one of them is `warn`-coloured with a
 *  link inside rather than a sentence. Folding them in here would change how
 *  the login screen looks, which is not what sharing a box is for. */
export function FormError({ children }: { children: React.ReactNode }) {
  return (
    <p role="alert" className="rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-[13px] text-pretty">
      {children}
    </p>
  )
}

export function Badge({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <span className={cn('inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-semibold', className)}>
      {children}
    </span>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn('animate-spin text-muted', className)} aria-label="Načítání" />
}
