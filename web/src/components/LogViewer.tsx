import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Pause, Play, Trash2, ArrowDownToLine } from 'lucide-react'
import { Button } from './ui'
import type { Kind } from '../lib/api'

const MAX_LINES = 3000

// Canlı log penceresi (SSE ile akar).
export function LogViewer({ kind, name, onClose }: { kind: Kind; name: string; onClose: () => void }) {
  const [lines, setLines] = useState<string[]>([])
  const [paused, setPaused] = useState(false)
  const [filter, setFilter] = useState('')
  const [state, setState] = useState<'baglaniyor' | 'canli' | 'bitti' | 'hata'>('baglaniyor')
  const [follow, setFollow] = useState(true)
  const box = useRef<HTMLDivElement>(null)
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const buffer = useRef<string[]>([])

  useEffect(() => {
    const es = new EventSource(`/api/services/${kind}/${encodeURIComponent(name)}/logs?lines=300`, { withCredentials: true })
    es.onopen = () => setState('canli')
    es.onmessage = (e) => {
      const batch: string[] = JSON.parse(e.data)
      if (pausedRef.current) {
        buffer.current.push(...batch)
        return
      }
      setLines((l) => [...l, ...batch].slice(-MAX_LINES))
    }
    es.addEventListener('end', () => { setState('bitti'); es.close() })
    es.onerror = () => { setState((s) => (s === 'bitti' ? s : 'hata')); es.close() }
    return () => es.close()
  }, [kind, name])

  useEffect(() => {
    if (!paused && buffer.current.length) {
      const b = buffer.current
      buffer.current = []
      setLines((l) => [...l, ...b].slice(-MAX_LINES))
    }
  }, [paused])

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [lines, follow])

  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [onClose])

  const shown = filter ? lines.filter((l) => l.toLowerCase().includes(filter.toLowerCase())) : lines
  const stateText = { baglaniyor: 'Bağlanıyor…', canli: paused ? 'Duraklatıldı' : 'Canlı', bitti: 'Akış bitti', hata: 'Bağlantı kesildi' }[state]

  return createPortal(
    <div className="fixed inset-0 z-40 bg-background/80 backdrop-blur-sm flex items-stretch md:items-center justify-center md:p-6" role="dialog" aria-modal aria-label={`${name} logları`}>
      <div className="bg-card border border-border md:rounded-lg w-full max-w-6xl flex flex-col md:h-[85vh]">
        <header className="flex flex-wrap items-center gap-2 px-4 py-3 border-b border-border">
          <div className="mr-auto min-w-0">
            <div className="font-semibold truncate">{name}</div>
            <div className="text-xs text-muted-foreground flex items-center gap-1.5">
              <span className={`size-1.5 rounded-full ${state === 'canli' && !paused ? 'bg-success nabiz' : state === 'hata' ? 'bg-destructive' : 'bg-muted-foreground'}`} />
              {stateText} · {kind} · {lines.length} satır
            </div>
          </div>
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Loglarda ara"
            className="h-8 w-44 bg-background border border-border rounded-md px-2 text-sm placeholder:text-muted-foreground"
          />
          <Button onClick={() => setPaused((p) => !p)} title={paused ? 'Devam et' : 'Duraklat'}>
            {paused ? <Play size={14} /> : <Pause size={14} />}
          </Button>
          <Button onClick={() => setFollow((f) => !f)} variant={follow ? 'normal' : 'hayalet'} title="Sona kaydır">
            <ArrowDownToLine size={14} />
          </Button>
          <Button onClick={() => setLines([])} title="Ekranı temizle"><Trash2 size={14} /></Button>
          <Button onClick={onClose} variant="hayalet" title="Kapat (Esc)"><X size={16} /></Button>
        </header>
        <div
          ref={box}
          onScroll={(e) => {
            const el = e.currentTarget
            setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
          }}
          className="flex-1 overflow-auto font-mono text-[12.5px] leading-relaxed px-4 py-3 bg-background/60"
        >
          {shown.length === 0 && <div className="text-muted-foreground">{state === 'baglaniyor' ? 'Loglar yükleniyor…' : 'Gösterilecek satır yok.'}</div>}
          {shown.map((l, i) => (
            <div key={i} className={`whitespace-pre-wrap break-all ${/error|err!|fatal|panic|exception|failed/i.test(l) ? 'text-destructive' : /warn/i.test(l) ? 'text-warning' : ''}`}>
              {l}
            </div>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  )
}
