import { useEffect, useRef, useState, type ReactNode, type ButtonHTMLAttributes } from 'react'
import { LoaderCircle } from 'lucide-react'

export function Panel({ title, action, children, className = '', pad = true }: {
  title?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; pad?: boolean
}) {
  return (
    <section className={`bg-panel border border-cizgi rounded-lg ${className}`}>
      {(title || action) && (
        <header className="flex items-center justify-between gap-3 px-4 pt-3.5 pb-2">
          <h2 className="text-[15px] font-semibold text-metin">{title}</h2>
          {action}
        </header>
      )}
      <div className={pad ? 'px-4 pb-4' : ''}>{children}</div>
    </section>
  )
}

export function PageHeader({ title, desc, action }: { title: string; desc?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3 mb-5">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {desc && <p className="text-soluk mt-0.5 max-w-[70ch]">{desc}</p>}
      </div>
      {action}
    </div>
  )
}

type BtnVariant = 'normal' | 'birincil' | 'tehlike' | 'hayalet'

const variants: Record<BtnVariant, string> = {
  normal: 'bg-panel2 border-cizgi hover:border-soluk/60 text-metin',
  birincil: 'bg-amber border-amber text-gece font-semibold hover:brightness-110',
  tehlike: 'bg-kirmizi/10 border-kirmizi/50 text-kirmizi hover:bg-kirmizi/20',
  hayalet: 'bg-transparent border-transparent text-soluk hover:text-metin hover:bg-panel2',
}

export function Button({ variant = 'normal', loading, children, className = '', ...rest }:
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: BtnVariant; loading?: boolean }) {
  return (
    <button
      {...rest}
      disabled={rest.disabled || loading}
      className={`inline-flex items-center justify-center gap-1.5 h-8 px-3 rounded-md border text-sm transition-colors
        disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer ${variants[variant]} ${className}`}
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
      variant={armed ? 'tehlike' : rest.variant}
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

export function Dot({ ok, warn }: { ok: boolean; warn?: boolean }) {
  const c = ok ? 'bg-yesil' : warn ? 'bg-amber' : 'bg-kirmizi'
  return <span className={`inline-block size-2 rounded-full shrink-0 ${c}`} aria-hidden />
}

export function Tag({ children, tone = 'soluk' }: { children: ReactNode; tone?: 'soluk' | 'yesil' | 'kirmizi' | 'amber' | 'mavi' }) {
  const tones = {
    soluk: 'text-soluk border-cizgi',
    yesil: 'text-yesil border-yesil/40',
    kirmizi: 'text-kirmizi border-kirmizi/40',
    amber: 'text-amber border-amber/40',
    mavi: 'text-mavi border-mavi/40',
  }
  return <span className={`inline-flex items-center h-5 px-1.5 rounded border text-xs whitespace-nowrap ${tones[tone]}`}>{children}</span>
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return <div className="border border-kirmizi/40 bg-kirmizi/10 text-kirmizi rounded-md px-3 py-2 text-sm whitespace-pre-wrap">{children}</div>
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="text-soluk text-sm py-6 text-center">{children}</div>
}

export function Loading() {
  return (
    <div className="flex items-center gap-2 text-soluk py-10 justify-center">
      <LoaderCircle size={16} className="animate-spin" /> Yükleniyor
    </div>
  )
}

export function Bar({ value, color }: { value: number; color: string }) {
  return (
    <div className="h-1.5 bg-gece rounded-full overflow-hidden">
      <div className="h-full rounded-full transition-[width] duration-700" style={{ width: `${Math.min(100, Math.max(0, value))}%`, background: color }} />
    </div>
  )
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { id: T; label: ReactNode }[] }) {
  return (
    <div className="inline-flex p-0.5 bg-gece border border-cizgi rounded-md" role="tablist">
      {items.map((it) => (
        <button
          key={it.id}
          role="tab"
          aria-selected={value === it.id}
          onClick={() => onChange(it.id)}
          className={`h-7 px-3 rounded text-sm cursor-pointer transition-colors ${value === it.id ? 'bg-panel2 text-metin' : 'text-soluk hover:text-metin'}`}
        >
          {it.label}
        </button>
      ))}
    </div>
  )
}

// Basit bildirim (toast) sistemi
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
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className={`rounded-md border px-3 py-2 text-sm shadow-lg bg-panel2 whitespace-pre-wrap
          ${t.ok ? 'border-yesil/50' : 'border-kirmizi/60 text-kirmizi'}`}>
          {t.text}
        </div>
      ))}
    </div>
  )
}
