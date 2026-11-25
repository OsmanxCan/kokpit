import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { Maximize2, Minimize2, Plug, RotateCw, Settings2 } from 'lucide-react'
import { usePoll } from '../lib/hooks'
import type { SettingsT } from '../lib/api'
import { Button, Card, cn, Loading, PageHeader } from '../components/ui'

type State = 'baglaniyor' | 'acik' | 'kapali'

const closeText: Record<number, string> = {
  4000: '15 dakika işlem yapılmadığı için kapatıldı',
  4001: 'Panel oturumu sona erdi',
  4003: 'Terminal Ayarlar\'dan kapatıldı',
}

// xterm teması: panelin renklerine uyumlu
function theme() {
  const dark = document.documentElement.classList.contains('dark')
  return dark
    ? { background: '#0a0a0a', foreground: '#e5e5e5', cursor: '#e5e5e5', selectionBackground: '#404040', black: '#171717', brightBlack: '#525252' }
    : { background: '#fafafa', foreground: '#171717', cursor: '#171717', selectionBackground: '#d4d4d4', black: '#171717', brightBlack: '#737373', white: '#a3a3a3', brightWhite: '#525252' }
}

export function Terminal() {
  const { data: settings } = usePoll<SettingsT>('/api/settings')
  const box = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<State>('baglaniyor')
  const [reason, setReason] = useState('')
  const [session, setSession] = useState(0)
  const [full, setFull] = useState(false)
  const fitRef = useRef<FitAddon | null>(null)

  useEffect(() => {
    if (!settings || settings.terminalOff || !box.current) return
    const term = new XTerm({
      fontFamily: '"Geist Mono Variable", ui-monospace, monospace', fontSize: 13, lineHeight: 1.2,
      cursorBlink: true, theme: theme(), scrollback: 5000, allowProposedApi: false,
    })
    const fit = new FitAddon()
    fitRef.current = fit
    term.loadAddon(fit)
    term.open(box.current)
    fit.fit()
    setState('baglaniyor')
    setReason('')

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/api/terminal`)
    ws.binaryType = 'arraybuffer'
    const send = (m: object) => ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify(m))
    const resize = () => { fit.fit(); send({ t: 'r', c: term.cols, r: term.rows }) }

    ws.onopen = () => { setState('acik'); resize(); term.focus() }
    ws.onmessage = (e) => {
      if (typeof e.data === 'string') {
        try { const d = JSON.parse(e.data); if (d.error) term.write(`\r\n\x1b[31m${d.error}\x1b[0m\r\n`) } catch { /* yoksay */ }
      } else term.write(new Uint8Array(e.data))
    }
    ws.onclose = (e) => {
      setState('kapali')
      setReason(closeText[e.code] ?? (e.code === 1006 ? 'Bağlantı koptu' : 'Oturum kapandı'))
    }
    const d1 = term.onData((d) => send({ t: 'i', d }))
    const ro = new ResizeObserver(() => resize())
    ro.observe(box.current)
    return () => { d1.dispose(); ro.disconnect(); ws.close(); term.dispose() }
  }, [settings, session])

  useEffect(() => { requestAnimationFrame(() => fitRef.current?.fit()) }, [full])

  if (!settings) return <Loading />
  if (settings.terminalOff) {
    return (
      <div className="space-y-5">
        <PageHeader title="Terminal" />
        <Card>
          <div className="flex flex-col items-center gap-3 py-14 text-center">
            <Plug className="text-muted-foreground" />
            <div className="font-medium">Web terminali kapalı</div>
            <p className="max-w-sm text-sm text-muted-foreground">Ayarlar → Dosyalar ve terminal bölümünden açabilirsin.</p>
            <Link to="/ayarlar?sekme=dosyalar"><Button variant="default"><Settings2 size={14} /> Ayarlara git</Button></Link>
          </div>
        </Card>
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col gap-4', full ? 'fixed inset-0 z-50 bg-background p-3' : 'h-[calc(100dvh-7.5rem)] min-h-96')}>
      {!full && (
        <PageHeader
          title="Terminal"
          desc="Sunucuda root olarak çalışan bir kabuk. Her açılış kaydedilir ve Telegram'a bildirilir; 15 dakika işlem yapılmazsa kapanır."
        />
      )}
      <Card className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center gap-2 border-b px-3 py-2 text-sm">
          <span className={cn('size-2 rounded-full', state === 'acik' ? 'bg-success' : state === 'baglaniyor' ? 'bg-warning animate-pulse' : 'bg-muted-foreground/50')} />
          <span className="text-muted-foreground">{state === 'acik' ? 'Bağlı' : state === 'baglaniyor' ? 'Bağlanıyor…' : reason}</span>
          <div className="ml-auto flex gap-1">
            {state === 'kapali' && <Button size="sm" variant="default" onClick={() => setSession((n) => n + 1)}><RotateCw size={14} /> Yeniden bağlan</Button>}
            <Button size="icon" variant="ghost" onClick={() => setFull(!full)} aria-label={full ? 'Küçült' : 'Tam ekran'}>{full ? <Minimize2 size={14} /> : <Maximize2 size={14} />}</Button>
          </div>
        </div>
        <div className="min-h-0 flex-1 p-2" style={{ background: theme().background }}>
          <div ref={box} className="h-full w-full" />
        </div>
      </Card>
    </div>
  )
}
