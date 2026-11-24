import { useState } from 'react'
import { Play, Square, RotateCw, ScrollText, RefreshCw } from 'lucide-react'
import { api, type Item, type Kind, type ServicesRes } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { bytes, duration, pct } from '../lib/format'
import { Panel, PageHeader, Loading, Button, ConfirmButton, Dot, Tag, Empty, ErrorBox, toast } from '../components/ui'
import { LogViewer } from '../components/LogViewer'

const kindTitle: Record<Kind, string> = { pm2: 'PM2 süreçleri', docker: 'Docker container\'ları', systemd: 'Systemd servisleri' }
const actionDone: Record<string, string> = { start: 'başlatıldı', stop: 'durduruldu', restart: 'yeniden başlatıldı' }

function Row({ it, onLogs, onChange }: { it: Item; onLogs: () => void; onChange: () => void }) {
  const [busy, setBusy] = useState<string | null>(null)
  async function act(action: string) {
    setBusy(action)
    try {
      await api.post(`/api/services/${it.kind}/${encodeURIComponent(it.name)}/${action}`)
      toast.ok(`${it.name} ${actionDone[action]}`)
      onChange()
    } catch (e) {
      toast.err(`${it.name}: ${(e as Error).message}`)
    } finally {
      setBusy(null)
    }
  }
  const up = it.kind === 'pm2' && it.uptime && it.healthy ? duration((Date.now() - it.uptime) / 1000) : ''
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 border-b border-border/60 last:border-0">
      <div className="flex items-center gap-2.5 min-w-0 flex-1 basis-64">
        <Dot ok={it.healthy} />
        <div className="min-w-0">
          <div className="font-medium truncate flex items-center gap-2">
            {it.name}
            {it.user === 'root' && it.kind === 'pm2' && <Tag tone="amber">root</Tag>}
            {it.user === 'izleniyor' && <Tag tone="mavi">izleniyor</Tag>}
          </div>
          <div className="text-xs text-muted-foreground truncate" title={it.info}>{it.info}{it.ports ? ` · ${it.ports}` : ''}</div>
        </div>
      </div>
      <div className="flex items-center gap-4 text-sm">
        <span className={it.healthy ? 'text-success' : 'text-destructive'}>{it.status}</span>
        {(it.kind !== 'systemd') && (
          <>
            <span className="text-muted-foreground w-16 text-right">{pct(it.cpu, 1)}</span>
            <span className="text-muted-foreground w-20 text-right">{bytes(it.mem)}</span>
          </>
        )}
        {it.kind === 'pm2' && <span className="text-muted-foreground w-28 text-right hidden sm:inline">{up}{it.restarts ? ` · ↺${it.restarts}` : ''}</span>}
      </div>
      <div className="flex items-center gap-1.5">
        <Button variant="hayalet" onClick={onLogs} title="Canlı loglar"><ScrollText size={15} /> Log</Button>
        {it.healthy ? (
          <>
            <ConfirmButton onConfirm={() => act('restart')} loading={busy === 'restart'} title="Yeniden başlat"><RotateCw size={14} /></ConfirmButton>
            <ConfirmButton onConfirm={() => act('stop')} loading={busy === 'stop'} title="Durdur" confirmText="Durdur?"><Square size={13} /></ConfirmButton>
          </>
        ) : (
          <Button onClick={() => act('start')} loading={busy === 'start'} title="Başlat"><Play size={14} /> Başlat</Button>
        )}
      </div>
    </li>
  )
}

export function Projects() {
  const [all, setAll] = useState(false)
  const { data, error, reload, loading } = usePoll<ServicesRes>(`/api/services${all ? '?all=1' : ''}`, 15000)
  const [logs, setLogs] = useState<{ kind: Kind; name: string } | null>(null)
  const [q, setQ] = useState('')

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  const match = (it: Item) => !q || it.name.toLowerCase().includes(q.toLowerCase()) || it.info.toLowerCase().includes(q.toLowerCase())

  return (
    <div className="space-y-5">
      <PageHeader
        title="Projeler"
        desc="PM2 süreçleri, Docker container'ları ve sistem servisleri. Yeniden başlatma ve durdurma için butona iki kez bas."
        action={
          <div className="flex items-center gap-2">
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Ara" className="h-8 w-40 bg-background border border-border rounded-md px-2 text-sm placeholder:text-muted-foreground" />
            <Button onClick={reload} loading={loading} title="Yenile"><RefreshCw size={14} /></Button>
          </div>
        }
      />
      {(['pm2', 'docker', 'systemd'] as Kind[]).map((k) => {
        const items = (data[k] ?? []).filter(match)
        return (
          <Panel
            key={k}
            pad={false}
            title={<span>{kindTitle[k]} <span className="text-muted-foreground font-normal">{items.filter((i) => i.healthy).length}/{items.length}</span></span>}
            action={k === 'systemd' && (
              <label className="text-sm text-muted-foreground flex items-center gap-2 cursor-pointer">
                <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} className="accent-primary" /> Durmuş servisleri de göster
              </label>
            )}
          >
            {data.errors?.[k] && <div className="px-4 pb-3"><ErrorBox>{data.errors[k]}</ErrorBox></div>}
            {items.length === 0 ? <Empty>{q ? 'Aramayla eşleşen yok.' : 'Burada çalışan bir şey yok.'}</Empty> : (
              <ul className="border-t border-border">
                {items.map((it) => <Row key={it.kind + it.name} it={it} onLogs={() => setLogs({ kind: k, name: it.name })} onChange={reload} />)}
              </ul>
            )}
          </Panel>
        )
      })}
      {logs && <LogViewer kind={logs.kind} name={logs.name} onClose={() => setLogs(null)} />}
    </div>
  )
}
