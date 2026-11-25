import { useEffect, useState } from 'react'
import { Plus, RefreshCw, Play, Pause, Pencil, Trash2, EllipsisVertical, ExternalLink, Activity } from 'lucide-react'
import { api, type Monitor, type UpDetail, type UpSummary } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago, dateTime, daysLeft, duration } from '../lib/format'
import { AreaChart } from '../components/charts'
import {
  Button, Card, CardContent, CardHeader, cn, ConfirmButton, Dialog, Dot, ErrorBox, Field, Input, Loading,
  Menu, MenuItem, PageHeader, Select, Switch, Tag, toast,
} from '../components/ui'

const fmtPct = (v: number) => v < 0 ? '—' : `%${v >= 99.995 ? '100' : v.toFixed(2).replace('.', ',')}`

// "3 gündür", "5 saattir", "12 dakikadır"
function sinceText(since: number) {
  const s = Date.now() / 1000 - since
  if (s >= 86400) return `${Math.floor(s / 86400)} gündür`
  if (s >= 3600) return `${Math.floor(s / 3600)} saattir`
  if (s >= 60) return `${Math.floor(s / 60)} dakikadır`
  return 'az önce'
}

const statusText: Record<UpSummary['status'], string> = { up: 'Çalışıyor', down: 'Erişilemiyor', pending: 'Bekleniyor', paused: 'Duraklatıldı' }

function StatusDot({ s }: { s: UpSummary['status'] }) {
  if (s === 'paused' || s === 'pending') return <span className="inline-flex size-2 rounded-full bg-muted-foreground/40" aria-hidden />
  return (
    <span className="relative inline-flex size-2">
      {s === 'down' && <span className="absolute inset-0 animate-ping rounded-full bg-destructive opacity-60" />}
      <Dot ok={s === 'up'} />
    </span>
  )
}

// Son 24 saatin saatlik durum şeridi
function Strip({ bars }: { bars: UpSummary['bars'] }) {
  return (
    <div className="flex h-6 items-stretch gap-[2px]" role="img" aria-label="Son 24 saatin durumu">
      {bars.map((b) => {
        const c = b.uptime < 0 ? 'bg-muted' : b.uptime >= 99.9 ? 'bg-success' : b.uptime >= 95 ? 'bg-warning' : 'bg-destructive'
        const t = new Date(b.ts * 1000).toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })
        return <span key={b.ts} className={cn('flex-1 min-w-[3px] rounded-[2px]', c)}
          title={b.uptime < 0 ? `${t} · veri yok` : `${t} · ${fmtPct(b.uptime)}${b.avgMs ? ` · ort. ${b.avgMs} ms` : ''}`} />
      })}
    </div>
  )
}

function SslTag({ exp }: { exp: number }) {
  if (!exp) return null
  const d = daysLeft(exp)
  return <Tag tone={d < 7 ? 'kirmizi' : d <= 14 ? 'amber' : 'soluk'}>SSL {d < 0 ? 'süresi dolmuş' : `${d} gün`}</Tag>
}

type Form = Omit<Monitor, 'id' | 'createdAt'>
const blank: Form = { name: '', url: '', interval: 60, timeout: 10, expect: 0, keyword: '', enabled: true }

function MonitorForm({ open, initial, onClose, onSaved }: { open: boolean; initial: Monitor | null; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<Form>(blank)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [sugg, setSugg] = useState<{ name: string; url: string }[]>([])

  useEffect(() => {
    if (!open) return
    setErr('')
    setF(initial ? { ...initial } : blank)
    if (!initial) api.get<{ name: string; url: string }[]>('/api/uptime/suggest').then(setSugg).catch(() => setSugg([]))
  }, [open, initial])

  async function save() {
    setBusy(true)
    setErr('')
    try {
      if (initial) await api.put(`/api/uptime/${initial.id}`, f)
      else await api.post('/api/uptime', f)
      toast.ok(initial ? 'İzleyici güncellendi' : 'İzleyici eklendi, ilk kontrol yapılıyor')
      onSaved()
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} title={initial ? 'İzleyiciyi düzenle' : 'Site izle'}
      description="Kokpit bu adrese düzenli olarak istek gönderir. Art arda 2 kontrol başarısız olursa site çöktü sayılır ve bildirim gelir."
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="default" onClick={save} loading={busy}>{initial ? 'Kaydet' : 'İzlemeye başla'}</Button></>}>
      <div className="space-y-4">
        {!initial && sugg.length > 0 && (
          <div>
            <div className="mb-1.5 text-xs text-muted-foreground">Nginx'teki siteler</div>
            <div className="flex flex-wrap gap-1.5">
              {sugg.slice(0, 8).map((s) => (
                <Button key={s.url} size="sm" variant={f.url === s.url ? 'default' : 'secondary'} onClick={() => setF({ ...f, url: s.url, name: f.name || s.name })}>{s.name}</Button>
              ))}
            </div>
          </div>
        )}
        <Field label="Adres">
          <Input value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} placeholder="https://ornek.com/saglik" autoFocus={!initial} />
        </Field>
        <Field label="Ad" hint="Boş bırakırsan alan adı kullanılır">
          <Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} maxLength={60} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Kontrol aralığı">
            <Select value={f.interval} onChange={(e) => setF({ ...f, interval: Number(e.target.value) })}>
              {[30, 60, 120, 300, 600, 1800].map((s) => <option key={s} value={s}>{duration(s)}</option>)}
            </Select>
          </Field>
          <Field label="Zaman aşımı">
            <Select value={f.timeout} onChange={(e) => setF({ ...f, timeout: Number(e.target.value) })}>
              {[5, 10, 20, 30].map((s) => <option key={s} value={s}>{s} sn</option>)}
            </Select>
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Beklenen HTTP kodu" hint="Boş: 200–399 arası">
            <Input value={f.expect || ''} onChange={(e) => setF({ ...f, expect: Number(e.target.value.replace(/\D/g, '')) || 0 })} placeholder="200–399" inputMode="numeric" />
          </Field>
          <Field label="Sayfada aranacak metin" hint="İsteğe bağlı">
            <Input value={f.keyword} onChange={(e) => setF({ ...f, keyword: e.target.value })} maxLength={200} />
          </Field>
        </div>
        <Switch checked={f.enabled} onChange={(v) => setF({ ...f, enabled: v })} label="İzleme açık" />
        {err && <ErrorBox>{err}</ErrorBox>}
      </div>
    </Dialog>
  )
}

function DetailDialog({ id, onClose }: { id: string | null; onClose: () => void }) {
  const [d, setD] = useState<UpDetail | null>(null)
  useEffect(() => {
    setD(null)
    if (id) api.get<UpDetail>(`/api/uptime/${id}`).then(setD).catch((e) => toast.err(e.message))
  }, [id])
  const checks = d?.checks ?? []
  return (
    <Dialog open={!!id} onClose={onClose} wide title={d?.name ?? 'Yükleniyor'} description={d?.url}>
      {!d ? <Loading /> : (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {[['Son 24 saat', fmtPct(d.uptime24)], ['Son 7 gün', fmtPct(d.uptime7)], ['Son 30 gün', fmtPct(d.uptime30)], ['Ort. yanıt', d.avgMs ? `${d.avgMs} ms` : '—']].map(([l, v]) => (
              <div key={l} className="rounded-lg border px-3 py-2.5">
                <div className="text-xs text-muted-foreground">{l}</div>
                <div className="mt-0.5 text-lg font-semibold tabular-nums">{v}</div>
              </div>
            ))}
          </div>
          <div>
            <div className="mb-2 text-sm font-medium">Yanıt süresi, son 24 saat</div>
            <AreaChart height={160} times={checks.map((c) => c.ts)} format={(v) => `${Math.round(v)} ms`}
              series={[{ key: 'ms', label: 'Yanıt', color: 'var(--brand)', values: checks.map((c) => (c.ok ? c.ms : 0)) }]} />
          </div>
          {d.certExp > 0 && (
            <div className="text-sm text-muted-foreground">
              SSL sertifikası: <span className="text-foreground">{d.certIssuer || 'bilinmiyor'}</span> tarafından verilmiş, {dateTime(d.certExp)} tarihinde bitiyor ({daysLeft(d.certExp)} gün).
            </div>
          )}
          <div>
            <div className="mb-2 text-sm font-medium">Kesintiler</div>
            {d.incidents.length === 0 ? <div className="text-sm text-muted-foreground">Kayıtlı kesinti yok.</div> : (
              <ul className="divide-y rounded-lg border text-sm">
                {d.incidents.map((i) => (
                  <li key={i.start} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                    <span className="flex items-center gap-2">
                      <Dot ok={i.end > 0} warn={false} />
                      {dateTime(i.start)}
                      <span className="text-muted-foreground">{i.reason}</span>
                    </span>
                    <span className="text-muted-foreground tabular-nums">{i.end ? duration(i.end - i.start) : 'devam ediyor'}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </Dialog>
  )
}

export function Uptime() {
  const { data, error, reload, loading } = usePoll<UpSummary[]>('/api/uptime', 20000)
  const [form, setForm] = useState<{ open: boolean; m: Monitor | null }>({ open: false, m: null })
  const [detail, setDetail] = useState<string | null>(null)
  const [checking, setChecking] = useState<string | null>(null)

  async function checkNow(m: UpSummary) {
    setChecking(m.id)
    try {
      const c = await api.post<{ ok: boolean; ms: number; error?: string }>(`/api/uptime/${m.id}/check`)
      c.ok ? toast.ok(`${m.name}: ${c.ms} ms`) : toast.err(`${m.name}: ${c.error}`)
      reload()
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setChecking(null)
    }
  }

  async function toggle(m: UpSummary) {
    try {
      await api.put(`/api/uptime/${m.id}`, { ...m, enabled: !m.enabled })
      reload()
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  async function remove(m: UpSummary) {
    try {
      await api.del(`/api/uptime/${m.id}`)
      toast.ok('İzleyici silindi')
      reload()
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  const down = data.filter((m) => m.status === 'down')
  const active = data.filter((m) => m.enabled && m.uptime24 >= 0)
  const avg24 = active.length ? active.reduce((a, m) => a + m.uptime24, 0) / active.length : -1

  return (
    <div className="space-y-5">
      <PageHeader
        title="Uptime"
        desc="Sitelerine düzenli olarak dışarıdan istek gönderir; çöktüğünde, düzeldiğinde ve SSL sertifikası bitmeye yaklaştığında haber verir."
        action={
          <div className="flex gap-2">
            <Button onClick={reload} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>
            <Button variant="default" onClick={() => setForm({ open: true, m: null })}><Plus size={14} /> Site izle</Button>
          </div>
        }
      />

      {data.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <Activity className="text-muted-foreground" />
            <div className="font-medium">Henüz izlenen site yok</div>
            <p className="max-w-sm text-sm text-muted-foreground">Bir adres ekle; Kokpit her dakika kontrol etsin ve sorun olduğunda Telegram'dan haber versin.</p>
            <Button variant="default" onClick={() => setForm({ open: true, m: null })}><Plus size={14} /> Site izle</Button>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <Card><CardContent className="py-4">
              <div className="text-sm text-muted-foreground">İzlenen site</div>
              <div className="mt-1 text-2xl font-semibold tabular-nums">{data.length}</div>
            </CardContent></Card>
            <Card><CardContent className="py-4">
              <div className="text-sm text-muted-foreground">Şu an erişilemeyen</div>
              <div className={cn('mt-1 text-2xl font-semibold tabular-nums', down.length > 0 && 'text-destructive')}>{down.length}</div>
            </CardContent></Card>
            <Card><CardContent className="py-4">
              <div className="text-sm text-muted-foreground">Ortalama erişilebilirlik (24 saat)</div>
              <div className="mt-1 text-2xl font-semibold tabular-nums">{fmtPct(avg24)}</div>
            </CardContent></Card>
          </div>

          <Card>
            <CardHeader title="İzleyiciler" description="Şerit son 24 saati saat saat gösterir; üzerine gelince ayrıntısı görünür." />
            <CardContent className="px-0 pb-0">
              <ul>
                {data.map((m) => (
                  <li key={m.id} className="flex flex-wrap items-center gap-x-6 gap-y-3 border-t px-6 py-4">
                    <button onClick={() => setDetail(m.id)} className="min-w-0 flex-1 basis-60 text-left cursor-pointer group">
                      <div className="flex items-center gap-2 font-medium">
                        <StatusDot s={m.status} />
                        <span className="truncate group-hover:underline underline-offset-4">{m.name}</span>
                        <SslTag exp={m.certExp} />
                      </div>
                      <div className="mt-0.5 truncate text-xs text-muted-foreground">
                        {statusText[m.status]}
                        {m.status === 'down' && m.lastError && <> · <span className="text-destructive">{m.lastError}</span></>}
                        {m.since > 0 && (m.status === 'up' || m.status === 'down') && <> · {sinceText(m.since)}</>}
                        {' · '}{m.url}
                      </div>
                    </button>
                    <div className="w-full basis-full sm:basis-56 sm:w-56"><Strip bars={m.bars} /></div>
                    <div className="flex items-center gap-6 text-sm tabular-nums">
                      <div className="w-16 text-right"><div className="text-xs text-muted-foreground">24 saat</div>{fmtPct(m.uptime24)}</div>
                      <div className="w-16 text-right"><div className="text-xs text-muted-foreground">Yanıt</div>{m.status === 'up' && m.lastCheck ? (m.lastMs < 1 ? '<1 ms' : `${m.lastMs} ms`) : '—'}</div>
                    </div>
                    <div className="flex items-center gap-1">
                      <Button size="icon" variant="ghost" onClick={() => checkNow(m)} loading={checking === m.id} title={`Şimdi kontrol et (son: ${ago(m.lastCheck)})`} aria-label="Şimdi kontrol et">
                        {checking !== m.id && <RefreshCw size={14} />}
                      </Button>
                      <Menu trigger={(o) => <Button size="icon" variant="ghost" onClick={o} aria-label="Diğer işlemler"><EllipsisVertical size={14} /></Button>}>
                        {(close) => (
                          <>
                            <MenuItem onClick={() => { close(); setForm({ open: true, m }) }}><Pencil size={14} /> Düzenle</MenuItem>
                            <MenuItem onClick={() => { close(); toggle(m) }}>{m.enabled ? <><Pause size={14} /> Duraklat</> : <><Play size={14} /> Devam ettir</>}</MenuItem>
                            <MenuItem onClick={() => { close(); window.open(m.url, '_blank', 'noopener') }}><ExternalLink size={14} /> Siteyi aç</MenuItem>
                            <div className="px-1 pt-1">
                              <ConfirmButton size="sm" variant="ghost" className="w-full justify-start text-destructive" onConfirm={() => { close(); remove(m) }} confirmText="Silinsin mi?">
                                <Trash2 size={14} /> Sil
                              </ConfirmButton>
                            </div>
                          </>
                        )}
                      </Menu>
                    </div>
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
        </>
      )}

      <MonitorForm open={form.open} initial={form.m} onClose={() => setForm({ open: false, m: null })} onSaved={reload} />
      <DetailDialog id={detail} onClose={() => setDetail(null)} />
    </div>
  )
}
