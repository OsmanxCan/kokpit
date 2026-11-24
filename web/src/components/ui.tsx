// shadcn/ui tarzında temel bileşenler (dış bağımlılık olmadan).
import { createPortal } from 'react-dom'
import { forwardRef, useEffect, useRef, useState, type ReactNode, type ButtonHTMLAttributes, type InputHTMLAttributes, type TextareaHTMLAttributes, type SelectHTMLAttributes } from 'react'
import { LoaderCircle, X } from 'lucide-react'

export const cn = (...c: (string | false | null | undefined)[]) => c.filter(Boolean).join(' ')

// ---------- Kart ----------

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('rounded-xl border bg-card text-card-foreground shadow-sm', className)}>{children}</div>
}

export function CardHeader({ title, description, action, className }: { title: ReactNode; description?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex items-start justify-between gap-4 px-6 pt-6 pb-4', className)}>
      <div className="min-w-0 space-y-1">
        <h3 className="font-semibold leading-none tracking-tight">{title}</h3>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}

export function CardContent({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('px-6 pb-6', className)}>{children}</div>
}

export function CardFooter({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('flex items-center gap-2 border-t px-6 py-4', className)}>{children}</div>
}

// Eski sayfalarla uyumlu kısa yol
export function Panel({ title, action, children, className = '', pad = true, description }: {
  title?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; pad?: boolean; description?: ReactNode
}) {
  return (
    <Card className={className}>
      {(title || action) && <CardHeader title={title} description={description} action={action} />}
      <div className={pad ? 'px-6 pb-6' : ''}>{children}</div>
    </Card>
  )
}

export function PageHeader({ title, desc, action }: { title: string; desc?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3 mb-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {desc && <p className="text-muted-foreground max-w-[72ch]">{desc}</p>}
      </div>
      {action}
    </div>
  )
}

// ---------- Buton ----------

type BtnVariant = 'default' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link'
type BtnSize = 'default' | 'sm' | 'icon' | 'lg'

const btnVariants: Record<BtnVariant, string> = {
  default: 'bg-primary text-primary-foreground shadow-xs hover:bg-primary/90',
  secondary: 'bg-secondary text-secondary-foreground shadow-xs hover:bg-secondary/80',
  outline: 'border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground dark:bg-input/20 dark:border-input dark:hover:bg-input/40',
  ghost: 'hover:bg-accent hover:text-accent-foreground',
  destructive: 'bg-destructive text-white shadow-xs hover:bg-destructive/90',
  link: 'text-foreground underline-offset-4 hover:underline',
}
const btnSizes: Record<BtnSize, string> = {
  default: 'h-9 px-4',
  sm: 'h-8 px-3 text-[13px]',
  lg: 'h-10 px-6',
  icon: 'size-8',
}

export function Button({ variant = 'outline', size = 'default', loading, children, className, ...rest }:
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: BtnVariant | 'normal' | 'birincil' | 'tehlike' | 'hayalet'; size?: BtnSize; loading?: boolean }) {
  const v = ({ normal: 'outline', birincil: 'default', tehlike: 'destructive', hayalet: 'ghost' } as Record<string, BtnVariant>)[variant] ?? (variant as BtnVariant)
  return (
    <button
      {...rest}
      disabled={rest.disabled || loading}
      className={cn(
        'inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all cursor-pointer',
        'disabled:pointer-events-none disabled:opacity-50 [&_svg]:shrink-0',
        btnVariants[v], btnSizes[size], className,
      )}
    >
      {loading && <LoaderCircle size={14} className="animate-spin" />}
      {children}
    </button>
  )
}

// İki tıklamalı onay: ilk tıklamada "Emin misin?" olur, 3 sn içinde ikinci tıklama işlemi yapar.
export function ConfirmButton({ onConfirm, children, confirmText = 'Emin misin?', ...rest }:
  Omit<Parameters<typeof Button>[0], 'onClick'> & { onConfirm: () => void; confirmText?: string }) {
  const [armed, setArmed] = useState(false)
  const t = useRef<number>(0)
  useEffect(() => () => window.clearTimeout(t.current), [])
  return (
    <Button
      {...rest}
      variant={armed ? 'destructive' : rest.variant}
      onClick={() => {
        if (armed) {
          setArmed(false)
          onConfirm()
        } else {
          setArmed(true)
          t.current = window.setTimeout(() => setArmed(false), 3000)
        }
      }}
    >
      {armed ? confirmText : children}
    </Button>
  )
}

// ---------- Form ----------

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input({ className, ...rest }, ref) {
  return (
    <input
      ref={ref}
      {...rest}
      className={cn(
        'h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none',
        'placeholder:text-muted-foreground dark:bg-input/30 disabled:opacity-50',
        'focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40',
        'aria-invalid:border-destructive aria-invalid:ring-destructive/20',
        className,
      )}
    />
  )
})

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, ...rest }, ref) {
  return (
    <textarea
      ref={ref}
      {...rest}
      className={cn(
        'w-full min-h-24 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow]',
        'placeholder:text-muted-foreground dark:bg-input/30 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40',
        className,
      )}
    />
  )
})

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...rest}
      className={cn(
        'h-9 rounded-md border border-input bg-transparent px-2.5 text-sm shadow-xs outline-none dark:bg-input/30 cursor-pointer',
        'focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 [&>option]:bg-popover',
        className,
      )}
    >
      {children}
    </select>
  )
}

export function Label({ children, htmlFor, className }: { children: ReactNode; htmlFor?: string; className?: string }) {
  return <label htmlFor={htmlFor} className={cn('text-sm font-medium leading-none select-none', className)}>{children}</label>
}

export function Field({ label, hint, children, htmlFor }: { label: ReactNode; hint?: ReactNode; children: ReactNode; htmlFor?: string }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <p className="text-[13px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function Switch({ checked, onChange, disabled, id, label }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean; id?: string; label?: string }) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-transparent shadow-xs transition-colors cursor-pointer disabled:opacity-50',
        checked ? 'bg-primary' : 'bg-input dark:bg-input/80',
      )}
    >
      <span className={cn('pointer-events-none block size-4 rounded-full bg-background shadow-sm transition-transform dark:bg-foreground',
        checked ? 'translate-x-[calc(100%-2px)] dark:bg-primary-foreground' : 'translate-x-0.5')} />
    </button>
  )
}

export function Separator({ className }: { className?: string }) {
  return <div className={cn('h-px w-full bg-border', className)} />
}

// ---------- Durum ----------

export function Dot({ ok, warn }: { ok: boolean; warn?: boolean }) {
  const c = ok ? 'bg-success' : warn ? 'bg-warning' : 'bg-destructive'
  return <span className={`relative inline-flex size-2 rounded-full shrink-0 ${c}`} aria-hidden />
}

type Tone = 'soluk' | 'yesil' | 'kirmizi' | 'amber' | 'mavi' | 'default'

export function Tag({ children, tone = 'soluk' }: { children: ReactNode; tone?: Tone }) {
  const tones: Record<Tone, string> = {
    soluk: 'text-muted-foreground border-border',
    default: 'bg-primary text-primary-foreground border-transparent',
    yesil: 'text-success border-success/30 bg-success/10',
    kirmizi: 'text-destructive border-destructive/30 bg-destructive/10',
    amber: 'text-warning border-warning/30 bg-warning/10',
    mavi: 'text-info border-info/30 bg-info/10',
  }
  return <span className={cn('inline-flex items-center gap-1 h-5 px-2 rounded-md border text-xs font-medium whitespace-nowrap', tones[tone])}>{children}</span>
}
export const Badge = Tag

export function ErrorBox({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-destructive/30 bg-destructive/10 text-destructive px-4 py-3 text-sm whitespace-pre-wrap">{children}</div>
}

export function Alert({ children, tone = 'mavi', title }: { children: ReactNode; tone?: 'mavi' | 'amber' | 'kirmizi' | 'yesil'; title?: ReactNode }) {
  const t = { mavi: 'border-info/30 bg-info/5 [&_b]:text-info', amber: 'border-warning/30 bg-warning/5 [&_b]:text-warning', kirmizi: 'border-destructive/30 bg-destructive/5 [&_b]:text-destructive', yesil: 'border-success/30 bg-success/5 [&_b]:text-success' }[tone]
  return (
    <div className={cn('rounded-lg border px-4 py-3 text-sm', t)}>
      {title && <b className="font-medium block mb-0.5">{title}</b>}
      <div className="text-muted-foreground">{children}</div>
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground text-sm py-8 text-center">{children}</div>
}

export function Loading() {
  return (
    <div className="flex items-center gap-2 text-muted-foreground py-16 justify-center">
      <LoaderCircle size={16} className="animate-spin" /> Yükleniyor
    </div>
  )
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} />
}

export function Bar({ value, color }: { value: number; color: string }) {
  return (
    <div className="h-1.5 bg-muted rounded-full overflow-hidden">
      <div className="h-full rounded-full transition-[width] duration-700" style={{ width: `${Math.min(100, Math.max(0, value))}%`, background: color }} />
    </div>
  )
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { id: T; label: ReactNode }[] }) {
  return (
    <div className="inline-flex h-9 items-center rounded-lg bg-muted p-[3px] text-muted-foreground" role="tablist">
      {items.map((it) => (
        <button
          key={it.id}
          role="tab"
          aria-selected={value === it.id}
          onClick={() => onChange(it.id)}
          className={cn('inline-flex h-full items-center gap-1.5 rounded-md px-3 text-sm font-medium transition-all cursor-pointer whitespace-nowrap',
            value === it.id ? 'bg-background text-foreground shadow-sm dark:bg-input/40' : 'hover:text-foreground')}
        >
          {it.label}
        </button>
      ))}
    </div>
  )
}

// ---------- Tablo ----------

export function Table({ children }: { children: ReactNode }) {
  return <div className="relative w-full overflow-x-auto"><table className="w-full caption-bottom text-sm">{children}</table></div>
}
export const Th = ({ children, className }: { children?: ReactNode; className?: string }) =>
  <th className={cn('h-10 px-3 first:pl-6 last:pr-6 text-left align-middle font-medium text-muted-foreground whitespace-nowrap', className)}>{children}</th>
export const Td = ({ children, className, title }: { children?: ReactNode; className?: string; title?: string }) =>
  <td title={title} className={cn('px-3 first:pl-6 last:pr-6 py-2.5 align-middle', className)}>{children}</td>
export const Tr = ({ children, className }: { children: ReactNode; className?: string }) =>
  <tr className={cn('border-b last:border-0 transition-colors hover:bg-muted/50', className)}>{children}</tr>
export const THead = ({ children }: { children: ReactNode }) => <thead className="[&_tr]:border-b [&_tr]:hover:bg-transparent">{children}</thead>

// ---------- Diyalog ----------

export function Dialog({ open, onClose, title, description, children, footer, wide, bare, className }: {
  open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; children: ReactNode; footer?: ReactNode
  wide?: boolean; bare?: boolean; className?: string
}) {
  useEffect(() => {
    if (!open) return
    const k = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', k)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { window.removeEventListener('keydown', k); document.body.style.overflow = prev }
  }, [open, onClose])
  if (!open) return null
  // Portal: pencere her zaman sayfanın en üstünde, ata öğelerin stillerinden etkilenmeden açılır
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start sm:items-center justify-center overflow-y-auto p-4 sm:p-6 bg-black/50" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal
        className={cn('sayfa-gir relative flex w-full flex-col overflow-hidden rounded-xl border bg-popover text-foreground shadow-lg',
          'max-h-[calc(100dvh-2rem)] sm:max-h-[calc(100dvh-3rem)]', wide ? 'max-w-2xl' : 'max-w-md', className)}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {bare ? children : (
          <>
            <div className="flex items-start justify-between gap-4 p-6 pb-2">
              <div className="space-y-1.5">
                <h2 className="text-lg font-semibold leading-none">{title}</h2>
                {description && <p className="text-sm text-muted-foreground">{description}</p>}
              </div>
              <button onClick={onClose} className="rounded-sm opacity-70 hover:opacity-100 cursor-pointer" aria-label="Kapat"><X size={16} /></button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">{children}</div>
            {footer && <div className="flex justify-end gap-2 px-6 pb-6">{footer}</div>}
          </>
        )}
      </div>
    </div>,
    document.body,
  )
}

// ---------- Açılır menü ----------

export function Menu({ trigger, children, align = 'end', side = 'bottom' }: { trigger: (open: () => void) => ReactNode; children: (close: () => void) => ReactNode; align?: 'start' | 'end'; side?: 'top' | 'bottom' }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const out = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false) }
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', out)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', out); window.removeEventListener('keydown', esc) }
  }, [open])
  return (
    <div ref={ref} className="relative">
      {trigger(() => setOpen((o) => !o))}
      {open && (
        <div role="menu" className={cn('sayfa-gir absolute z-50 min-w-56 rounded-lg border bg-popover p-1 shadow-md',
          side === 'top' ? 'bottom-full mb-2' : 'top-full mt-2', align === 'end' ? 'right-0' : 'left-0')}>
          {children(() => setOpen(false))}
        </div>
      )}
    </div>
  )
}

export function MenuItem({ children, onClick, danger }: { children: ReactNode; onClick: () => void; danger?: boolean }) {
  return (
    <button role="menuitem" onClick={onClick}
      className={cn('flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-none cursor-pointer [&_svg]:text-muted-foreground',
        danger ? 'text-destructive hover:bg-destructive/10 [&_svg]:text-destructive' : 'hover:bg-accent')}>
      {children}
    </button>
  )
}

// ---------- Bildirim (toast) ----------

type ToastT = { id: number; text: string; ok: boolean }
let pushToast: (t: Omit<ToastT, 'id'>) => void = () => {}
export const toast = {
  ok: (text: string) => pushToast({ text, ok: true }),
  err: (text: string) => pushToast({ text, ok: false }),
}

export function Toaster() {
  const [list, setList] = useState<ToastT[]>([])
  useEffect(() => {
    pushToast = (t) => {
      const id = Date.now() + Math.random()
      setList((l) => [...l, { ...t, id }])
      window.setTimeout(() => setList((l) => l.filter((x) => x.id !== id)), t.ok ? 3500 : 7000)
    }
  }, [])
  return (
    <div className="fixed bottom-4 right-4 z-[60] flex flex-col gap-2 w-[min(380px,calc(100vw-2rem))]" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className="sayfa-gir flex items-start gap-3 rounded-lg border bg-popover px-4 py-3 text-sm shadow-lg">
          <span className={cn('mt-1.5 size-2 rounded-full shrink-0', t.ok ? 'bg-success' : 'bg-destructive')} />
          <span className="whitespace-pre-wrap">{t.text}</span>
        </div>
      ))}
    </div>
  )
}
