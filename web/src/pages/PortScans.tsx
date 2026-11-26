import { useEffect, useState, type ReactNode } from 'react'
import { RefreshCw, Settings2, ShieldBan, ShieldCheck, ChevronDown, ChevronRight, Radar, Globe2 } from 'lucide-react'
import { api, type IPBan, type PortScanSettings, type Scan, type ScanResp } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago, dateTime } from '../lib/format'
import {
  Alert, Button, Card, CardContent, CardHeader, cn, ConfirmButton, Dialog, Empty, ErrorBox, Field, Input, Loading, PageHeader,
  Select, Switch, Tabs, Tag, Textarea, toast,
} from '../components/ui'
import { countryName, flag } from '../lib/geo'

const nf = new Intl.NumberFormat('tr-TR')

const typeInfo: Record<string, string> = {
  SYN: 'Standart tarama (nmap -sS)', FIN: 'FIN taraması: gizlenmeye çalışır', NULL: 'NULL taraması: gizlenmeye çalışır',
  XMAS: 'XMAS taraması: gizlenmeye çalışır', UDP: 'UDP taraması (VPN, DNS gibi servisleri arar)', OTHER: 'Alışılmadık bayraklar',
}

const banHourOptions = [
  { v: 1, l: '1 saat' }, { v: 6, l: '6 saat' }, { v: 24, l: '24 saat' }, { v: 72, l: '3 gün' }, { v: 168, l: '1 hafta' }, { v: 720, l: '30 gün' }, { v: 0, l: 'Kalıcı' },
]
const banLabel = (h: number) => banHourOptions.find((o) => o.v === h)?.l ?? `${h} saat`

function TypeTags({ types }: { types: string[] }) {
  return (
    <span className="flex flex-wrap gap-1">
      {types.map((t) => <span key={t} title={typeInfo[t]}><Tag tone={t === 'SYN' || t === 'UDP' ? 'soluk' : 'kirmizi'}>{t}</Tag></span>)}
    </span>
  )
}

function BanDialog({ ip, defHours, onClose, onDone }: { ip: string | null; defHours: number; onClose: () => void; onDone: () => void }) {
  const [hours, setHours] = useState(defHours)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (ip) setHours(defHours) }, [ip, defHours])
  if (!ip) return null
  async function go() {
    setBusy(true)
    try {
      await api.post('/api/security/bans', { ip, hours, reason: 'port taraması (elle)' })
      toast.ok(`${ip} engellendi`)
      onDone(); onClose()
    } catch (e) { toast.err((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Dialog open onClose={onClose} title={`${ip} engellensin mi?`} description="Bu IP'den gelen tüm bağlantılar firewall'un en üstündeki bir kuralla engellenir."
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="destructive" onClick={go} loading={busy}><ShieldBan size={14} /> Engelle</Button></>}>
      <Field label="Süre">
        <Select value={hours} onChange={(e) => setHours(Number(e.target.value))}>
          {banHourOptions.map((o) => <option key={o.v} value={o.v}>{o.l}</option>)}
        </Select>
      </Field>
    </Dialog>
  )
}

function SettingsDialog({ open, s, onClose, onSaved }: { open: boolean; s: PortScanSettings; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState(s)
  const [wl, setWl] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => { if (open) { setF(s); setWl(s.whitelist.join('\n')); setErr('') } }, [open, s])
  async function save() {
    setBusy(true); setErr('')
    try {
      await api.put('/api/settings', { portScan: { ...f, whitelist: wl.split(/[\s,]+/).filter(Boolean) } })
      toast.ok('Ayarlar kaydedildi')
      onSaved(); onClose()
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Dialog open={open} onClose={onClose} wide title="Port tarama ayarları"
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="default" onClick={save} loading={busy}>Kaydet</Button></>}>
      <div className="space-y-5">
        <label className="flex items-start justify-between gap-6 cursor-pointer">
          <div>
            <div className="text-sm font-medium">Taramaları tespit et</div>
            <p className="text-xs text-muted-foreground">Firewall'un engellediği bağlantılar izlenir. Kapatırsan Kokpit'in eklediği log kuralı da kaldırılır.</p>
          </div>
          <Switch checked={!f.off} onChange={(v) => setF({ ...f, off: !v })} label="Tespit" />
        </label>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Port eşiği" hint="Bu kadar farklı porta dokunan IP tarama sayılır">
            <Input type="number" min={5} max={1000} value={f.threshold} onChange={(e) => setF({ ...f, threshold: Number(e.target.value) })} />
          </Field>
          <Field label="Süre penceresi (saniye)" hint="Eşik bu süre içinde aşılmalı">
            <Input type="number" min={10} max={3600} value={f.window} onChange={(e) => setF({ ...f, window: Number(e.target.value) })} />
          </Field>
        </div>
        <div className="divide-y rounded-lg border">
          <label className="flex items-start justify-between gap-6 px-4 py-3 cursor-pointer">
            <div>
              <div className="text-sm font-medium">Hedefli taramaları otomatik engelle</div>
              <p className="text-xs text-muted-foreground">Seni hedefleyen bir tarama tespit edilince IP firewall'da engellenir.</p>
            </div>
            <Switch checked={f.autoBan} onChange={(v) => setF({ ...f, autoBan: v })} label="Otomatik engelle" />
          </label>
          <label className={cn('flex items-start justify-between gap-6 px-4 py-3 cursor-pointer', !f.autoBan && 'opacity-50')}>
            <div>
              <div className="text-sm font-medium">İnternet geneli tarayıcıları da engelle</div>
              <p className="text-xs text-muted-foreground">Shodan, Censys gibi kuruluşlar. Zararsızdır ama sunucunu herkese açık listelerde gösterirler.</p>
            </div>
            <Switch checked={f.banInternet} onChange={(v) => setF({ ...f, banInternet: v })} disabled={!f.autoBan} label="İnternet tarayıcılarını engelle" />
          </label>
          <div className={cn('flex items-center justify-between gap-6 px-4 py-3', !f.autoBan && 'opacity-50')}>
            <div className="text-sm font-medium">Engelleme süresi</div>
            <Select value={f.banHours} onChange={(e) => setF({ ...f, banHours: Number(e.target.value) })} className="w-40" disabled={!f.autoBan}>
              {banHourOptions.map((o) => <option key={o.v} value={o.v}>{o.l}</option>)}
            </Select>
          </div>
        </div>
        <Field label="Beyaz liste" hint="Asla engellenmeyecek IP'ler veya ağlar; her satıra bir tane (1.2.3.4 veya 10.0.0.0/8). Cloudflare, özel ağlar ve panele giriş yapılan IP'ler zaten korunuyor.">
          <Textarea rows={4} value={wl} onChange={(e) => setWl(e.target.value)} className="font-mono text-xs" placeholder="203.0.113.7" />
        </Field>
        {err && <ErrorBox>{err}</ErrorBox>}
      </div>
    </Dialog>
  )
}

function ScanRow({ s, onBan, onUnban, internet }: { s: Scan; onBan: (ip: string) => void; onUnban: (ip: string) => void; internet?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <li className="border-t">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-6 py-2.5 text-sm">
        <button onClick={() => setOpen(!open)} className="flex min-w-0 flex-1 basis-64 items-center gap-2 text-left cursor-pointer">
          {open ? <ChevronDown size={14} className="shrink-0 text-muted-foreground" /> : <ChevronRight size={14} className="shrink-0 text-muted-foreground" />}
          <span className="font-mono text-xs">{s.ip}</span>
          <span className="truncate text-muted-foreground">{flag(s.cc)} {countryName(s.cc)}</span>
          {s.open && <Tag tone="mavi">sürüyor</Tag>}
        </button>
        <span className="w-24 text-right tabular-nums">{nf.format(s.ports)} port</span>
        <span className="w-40"><TypeTags types={s.types} /></span>
        <span className="w-28 text-xs text-muted-foreground" title={dateTime(s.start)}>{ago(s.start)}</span>
        {!internet && (
          <span className="w-28 text-right">
            {s.banned
              ? <ConfirmButton size="sm" variant="ghost" onConfirm={() => onUnban(s.ip)} confirmText="Kaldırılsın mı?"><ShieldCheck size={14} className="text-success" /> Engelli</ConfirmButton>
              : <Button size="sm" variant="ghost" onClick={() => onBan(s.ip)}><ShieldBan size={14} /> Engelle</Button>}
          </span>
        )}
      </div>
      {open && (
        <div className="space-y-2 bg-muted/30 px-6 pb-3 pt-1 pl-12 text-sm">
          <div className="text-muted-foreground">
            {internet ? <>Ters DNS: <span className="font-mono text-foreground">{s.rdns}</span></> : <>Neden hedefli: <span className="text-foreground">{s.reason}</span>{s.ssh && ' · SSH’a da giriş denedi'}</>}
            {!internet && s.rdns && <> · Ters DNS: <span className="font-mono">{s.rdns}</span></>}
          </div>
          <div className="text-muted-foreground">{dateTime(s.start)} – {dateTime(s.end)} · {nf.format(s.packets)} paket</div>
          <div className="flex flex-wrap gap-1">
            {s.sample.map((p) => <span key={p} className="rounded bg-background px-1.5 py-0.5 font-mono text-[11px] ring-1 ring-border">{p}</span>)}
            {s.ports > s.sample.length && <span className="px-1.5 py-0.5 text-xs text-muted-foreground">+{nf.format(s.ports - s.sample.length)} port daha</span>}
          </div>
        </div>
      )}
    </li>
  )
}

function Bars({ a, b, range }: { a: number[]; b: number[]; range: string }) {
  const max = Math.max(1, ...a.map((x, i) => x + b[i]))
  return (
    <div>
      <div className="flex h-24 items-end gap-[3px]">
        {a.map((x, i) => (
          <div key={i} className="flex h-full flex-1 flex-col justify-end" title={`${x} hedefli, ${b[i]} internet geneli`}>
            <div className="w-full rounded-t-[3px] bg-muted-foreground/30" style={{ height: `${(b[i] / max) * 100}%` }} />
            <div className={cn('w-full bg-destructive/80', !b[i] && 'rounded-t-[3px]')} style={{ height: `${(x / max) * 100}%` }} />
          </div>
        ))}
      </div>
      <div className="mt-1.5 flex justify-between text-xs text-muted-foreground">
        <span>{range === '24h' ? '24 saat önce' : '7 gün önce'}</span>
        <span className="flex gap-3">
          <span className="flex items-center gap-1"><span className="size-2 rounded-sm bg-destructive/80" /> hedefli</span>
          <span className="flex items-center gap-1"><span className="size-2 rounded-sm bg-muted-foreground/30" /> internet geneli</span>
        </span>
        <span>şimdi</span>
      </div>
    </div>
  )
}

function BanList({ bans, onUnban }: { bans: IPBan[]; onUnban: (ip: string) => void }) {
  if (bans.length === 0) return null
  return (
    <Card>
      <CardHeader title="Engellenen IP'ler" description="Kokpit'in firewall'da engellediği adresler. Süresi dolanlar kendiliğinden kaldırılır." />
      <CardContent className="px-0 pb-0">
        <ul>
          {bans.map((b) => (
            <li key={b.ip} className="flex flex-wrap items-center gap-3 border-t px-6 py-2.5 text-sm">
              <span className="font-mono text-xs">{b.ip}</span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground">{b.reason} · {b.auto ? 'otomatik' : b.by}</span>
              <span className="text-xs text-muted-foreground">{b.until ? `${dateTime(b.until)} tarihine kadar` : 'kalıcı'}</span>
              <ConfirmButton size="sm" variant="ghost" onConfirm={() => onUnban(b.ip)} confirmText="Kaldırılsın mı?">Kaldır</ConfirmButton>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

export function PortScans({ switcher }: { switcher: ReactNode }) {
  const [range, setRange] = useState<'24h' | '7d'>('24h')
  const [list, setList] = useState<'targeted' | 'internet'>('targeted')
  const { data, error, loading, reload } = usePoll<ScanResp>(`/api/security/scans?range=${range}`, 20000)
  const [banIp, setBanIp] = useState<string | null>(null)
  const [settings, setSettings] = useState(false)
  const [limit, setLimit] = useState(30)

  async function unban(ip: string) {
    try { await api.del(`/api/security/bans/${encodeURIComponent(ip)}`); toast.ok(`${ip} engeli kaldırıldı`); reload() } catch (e) { toast.err((e as Error).message) }
  }

  const header = (
    <PageHeader
      title="Saldırılar"
      desc="Sunucunun portlarını tarayan IP'ler. Shodan gibi tüm interneti tarayan kuruluşlar ayrı, seni hedefleyenler ayrı gösterilir."
      action={
        <div className="flex flex-wrap gap-2">
          {switcher}
          <Tabs value={range} onChange={setRange} items={[{ id: '24h', label: '24 saat' }, { id: '7d', label: '7 gün' }]} />
          <Button onClick={reload} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>
          <Button onClick={() => setSettings(true)} aria-label="Ayarlar"><Settings2 size={14} /></Button>
        </div>
      }
    />
  )
  if (error && !data) return <div className="space-y-5">{header}<ErrorBox>{error}</ErrorBox></div>
  if (!data) return <div className="space-y-5">{header}<Loading /></div>

  const r = data.report
  const cfg = data.settings
  const scans = r.scans.filter((s) => (list === 'internet' ? s.kind === 'internet' : s.kind !== 'internet'))

  return (
    <div className="space-y-5">
      {header}

      {cfg.off ? (
        <Alert tone="amber" title="Port tarama tespiti kapalı">
          <div className="flex flex-wrap items-center justify-between gap-3"><span>Ayarlardan açabilirsin.</span><Button size="sm" onClick={() => setSettings(true)}>Ayarlar</Button></div>
        </Alert>
      ) : r.logRule && r.logRule !== 'aktif' ? (
        <Alert tone="amber" title="Tespit sınırlı çalışıyor">{r.logRule}</Alert>
      ) : null}
      {cfg.autoBan && !data.ufwActive && <Alert tone="kirmizi" title="Otomatik engelleme çalışamaz">UFW kapalı olduğu için IP'ler engellenemiyor. Firewall sayfasından aç.</Alert>}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {([
          ['Hedefli tarama', nf.format(r.targeted), r.targeted > 0],
          ['İnternet geneli', nf.format(r.internet), false],
          ['Farklı IP', nf.format(r.ips), false],
          ['Engellenen', cfg.autoBan ? nf.format(r.banned) : 'otomatik kapalı', false],
        ] as [string, string, boolean][]).map(([l, v, warn]) => (
          <Card key={l}><CardContent className="py-4">
            <div className="text-sm text-muted-foreground">{l}</div>
            <div className={cn('mt-1 truncate text-2xl font-semibold tabular-nums', warn && 'text-destructive', v === 'otomatik kapalı' && 'text-base text-muted-foreground font-normal pt-1.5')}>{v}</div>
          </CardContent></Card>
        ))}
      </div>

      <Card>
        <CardHeader title="Zaman içinde" description={range === '24h' ? 'Saatlik tarama sayısı' : 'Günlük tarama sayısı'} />
        <CardContent><Bars a={r.hourly} b={r.hourlyInternet} range={range} /></CardContent>
      </Card>

      <Card>
        <CardHeader
          title={list === 'targeted' ? 'Seni hedefleyen taramalar' : 'İnternet geneli tarayıcılar'}
          description={list === 'targeted'
            ? 'Bilinen bir araştırma kuruluşuna ait olmayan ya da gizlenmeye çalışan taramalar. Satıra tıklayınca taranan portlar görünür.'
            : 'Shodan, Censys gibi tüm interneti düzenli tarayan kuruluşlar. Ters DNS kayıtları ileri yönde doğrulandı; sahte kayıtla buraya düşülemez.'}
          action={<Tabs value={list} onChange={(v) => { setList(v); setLimit(30) }} items={[
            { id: 'targeted', label: <><Radar size={14} /> Hedefli ({r.targeted})</> },
            { id: 'internet', label: <><Globe2 size={14} /> İnternet geneli ({r.internet})</> },
          ]} />}
        />
        <CardContent className="px-0 pb-0">
          {scans.length === 0 ? (
            <Empty>{list === 'targeted' ? 'Bu süre içinde seni hedefleyen bir tarama yok.' : 'Bu süre içinde internet geneli tarayıcı görülmedi.'}</Empty>
          ) : (
            <ul>
              {scans.slice(0, limit).map((s) => <ScanRow key={s.id} s={s} internet={list === 'internet'} onBan={setBanIp} onUnban={unban} />)}
            </ul>
          )}
          {scans.length > limit && (
            <div className="border-t px-6 py-3"><Button size="sm" variant="ghost" onClick={() => setLimit(limit + 50)}>Daha fazla göster ({scans.length - limit})</Button></div>
          )}
        </CardContent>
      </Card>

      <BanList bans={data.bans} onUnban={unban} />

      <p className="text-xs text-muted-foreground">
        Tespit, firewall'un engellediği bağlantılara dayanır. Çok yavaş (saatte birkaç port) veya yüzlerce IP'ye bölünmüş taramalar fark edilmeyebilir.
        Otomatik engelleme: {cfg.autoBan ? `açık, ${banLabel(cfg.banHours)}` : 'kapalı'}.
      </p>

      <BanDialog ip={banIp} defHours={cfg.banHours || 24} onClose={() => setBanIp(null)} onDone={reload} />
      <SettingsDialog open={settings} s={cfg} onClose={() => setSettings(false)} onSaved={reload} />
    </div>
  )
}
