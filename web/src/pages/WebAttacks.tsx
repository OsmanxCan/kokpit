import { useEffect, useState, type ReactNode } from 'react'
import { RefreshCw, ShieldBan, ShieldCheck, ChevronDown, ChevronRight, TriangleAlert, Wrench } from 'lucide-react'
import { api, type WebIP, type WebResp } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago, dateTime } from '../lib/format'
import { countryName, flag } from '../lib/geo'
import {
  Alert, Button, Card, CardContent, CardHeader, cn, Dialog, Empty, ErrorBox, Field, Loading, PageHeader, Select, Tabs, Tag, toast,
} from '../components/ui'

const nf = new Intl.NumberFormat('tr-TR')

const catTone: Record<string, 'kirmizi' | 'amber' | 'soluk'> = {
  rce: 'kirmizi', sqli: 'kirmizi', traversal: 'kirmizi', xss: 'kirmizi', sensitive: 'amber', tool: 'amber',
  wordpress: 'soluk', admin: 'soluk', notfound: 'soluk', invalid: 'soluk',
}

const banHours = [{ v: 1, l: '1 saat' }, { v: 24, l: '24 saat' }, { v: 168, l: '1 hafta' }, { v: 720, l: '30 gün' }, { v: 0, l: 'Kalıcı' }]

function BanDialog({ ip, cf, onClose, onDone }: { ip: string | null; cf: boolean; onClose: () => void; onDone: () => void }) {
  const [hours, setHours] = useState(24)
  const [target, setTarget] = useState('auto')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (ip) { setHours(24); setTarget('auto') } }, [ip])
  if (!ip) return null
  async function go() {
    setBusy(true)
    try {
      const r = await api.post<{ backend: string }>('/api/web/ban', { ip, hours, target, reason: 'web saldırısı' })
      toast.ok(`${ip}: ${r.backend} ile engellendi`)
      onDone(); onClose()
    } catch (e) { toast.err((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Dialog open onClose={onClose} title={`${ip} engellensin mi?`}
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="destructive" onClick={go} loading={busy}><ShieldBan size={14} /> Engelle</Button></>}>
      <div className="space-y-4">
        <Field label="Nerede engellensin?" hint={cf
          ? 'Cloudflare Tünel veya proxy arkasındaki sitelere gelen saldırılar sadece Cloudflare\'de engellenebilir. "Otomatik" ikisini birlikte kullanır.'
          : 'Cloudflare ayarlanmadığı için sadece sunucu firewall\'u kullanılabilir. Tünel arkasındaki sitelere gelen istekleri bu engel durdurmaz; Ayarlar → Cloudflare bölümünden token ekleyebilirsin.'}>
          <Select value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="auto">{cf ? 'Otomatik (Cloudflare + firewall)' : 'Otomatik (firewall)'}</option>
            {cf && <option value="cloudflare">Sadece Cloudflare</option>}
            <option value="firewall">Sadece sunucu firewall'u</option>
          </Select>
        </Field>
        <Field label="Süre">
          <Select value={hours} onChange={(e) => setHours(Number(e.target.value))}>
            {banHours.map((o) => <option key={o.v} value={o.v}>{o.l}</option>)}
          </Select>
        </Field>
      </div>
    </Dialog>
  )
}

function RealIPCard({ d, onChange }: { d: WebResp; onChange: () => void }) {
  const ri = d.realip
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  if (!ri.nginx) return null
  const share = Math.max(ri.localShare, d.report.localShare)
  async function apply() {
    setBusy(true)
    try { await api.post('/api/web/realip'); toast.ok('Ayar uygulandı, Nginx yeniden yüklendi'); setOpen(false); onChange() } catch (e) { toast.err((e as Error).message) } finally { setBusy(false) }
  }
  async function remove() {
    if (!window.confirm('Kokpit\'in eklediği gerçek IP ayarı kaldırılsın mı?')) return
    try { await api.del('/api/web/realip'); toast.ok('Ayar kaldırıldı'); onChange() } catch (e) { toast.err((e as Error).message) }
  }
  if (ri.configured) {
    return ri.kokpit ? (
      <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <ShieldCheck size={12} className="text-success" /> Nginx ziyaretçilerin gerçek IP'sini kaydediyor (Kokpit ayarı).
        <button className="underline underline-offset-4 cursor-pointer" onClick={remove}>Kaldır</button>
      </p>
    ) : null
  }
  return (
    <>
      <Alert tone="amber" title="Nginx ziyaretçilerin gerçek IP'sini kaydetmiyor">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>
            {share > 1 ? `İsteklerin %${share.toFixed(0)} kadarı 127.0.0.1'den geliyormuş gibi görünüyor. ` : ''}
            Cloudflare Tünel veya proxy arkasında saldırganın IP'si görünmez; bu yüzden analiz eksik kalır ve engelleme yapılamaz.
          </span>
          <Button size="sm" variant="default" onClick={() => setOpen(true)}><Wrench size={14} /> Düzelt</Button>
        </div>
      </Alert>
      <Dialog open={open} onClose={() => setOpen(false)} wide title="Gerçek IP ayarı"
        description="Nginx'e ziyaretçinin IP'sini Cloudflare'in CF-Connecting-IP başlığından almasını söyleyen bir ayar dosyası eklenir."
        footer={<><Button onClick={() => setOpen(false)}>Vazgeç</Button><Button variant="default" onClick={apply} loading={busy}>Uygula</Button></>}>
        <div className="space-y-3 text-sm text-muted-foreground">
          <p>Dosya: <span className="font-mono text-foreground">/etc/nginx/conf.d/kokpit-realip.conf</span></p>
          <p>Başlığa sadece tünelden (127.0.0.1) ve Cloudflare'in sunucularından gelen isteklerde güvenilir; doğrudan gelen biri başlığı taklit ederek IP'sini gizleyemez.</p>
          <p>Uygulamadan önce <span className="font-mono">nginx -t</span> ile denenir; hata olursa hiçbir şey değişmez. Sonra Nginx kesintisiz yeniden yüklenir.</p>
          {!ri.confd && <ErrorBox>nginx.conf, conf.d klasörünü içermiyor; bu ayar otomatik uygulanamaz.</ErrorBox>}
          {ri.problems?.map((p) => <ErrorBox key={p}>{p}</ErrorBox>)}
        </div>
      </Dialog>
    </>
  )
}

function IPRow({ x, names, onBan }: { x: WebIP; names: Record<string, string>; onBan: (ip: string) => void }) {
  const [open, setOpen] = useState(false)
  const cats = Object.entries(x.cats).sort((a, b) => b[1] - a[1])
  return (
    <li className="border-t">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-6 py-2.5 text-sm">
        <button onClick={() => setOpen(!open)} className="flex min-w-0 flex-1 basis-56 items-center gap-2 text-left cursor-pointer">
          {open ? <ChevronDown size={14} className="shrink-0 text-muted-foreground" /> : <ChevronRight size={14} className="shrink-0 text-muted-foreground" />}
          <span className="font-mono text-xs">{x.ip}</span>
          <span className="truncate text-muted-foreground">{flag(x.cc)} {countryName(x.cc)}</span>
        </button>
        <span className="flex w-72 flex-wrap gap-1">
          {cats.slice(0, 3).map(([c, n]) => <Tag key={c} tone={catTone[c] ?? 'soluk'}>{names[c] ?? c} · {nf.format(n)}</Tag>)}
        </span>
        <span className="w-24 text-right tabular-nums text-muted-foreground">{nf.format(x.requests)} istek</span>
        <span className="w-20 text-xs text-muted-foreground">{ago(x.last)}</span>
        <span className="w-32 text-right">
          {x.blocked
            ? <Tag tone="yesil"><ShieldCheck size={12} /> {x.blocked === 'cloudflare' ? 'Cloudflare' : 'Firewall'}</Tag>
            : <Button size="sm" variant="ghost" onClick={() => onBan(x.ip)}><ShieldBan size={14} /> Engelle</Button>}
        </span>
      </div>
      {open && (
        <div className="space-y-2 bg-muted/30 px-6 pb-3 pt-1 pl-12 text-sm">
          <div className="text-muted-foreground">
            {dateTime(x.first)} – {dateTime(x.last)} · {nf.format(x.attacks)} saldırı isteği · {nf.format(x.notFound)} adet 404
            {x.sites?.length ? <> · {x.sites.join(', ')}</> : null}
          </div>
          {x.ua && <div className="truncate text-xs text-muted-foreground">Tarayıcı kimliği: <span className="font-mono">{x.ua}</span></div>}
          <ul className="space-y-0.5">
            {(x.paths ?? []).map((p) => <li key={p} className="truncate font-mono text-xs">{p}</li>)}
          </ul>
        </div>
      )}
    </li>
  )
}

export function WebAttacks({ switcher }: { switcher: ReactNode }) {
  const [range, setRange] = useState<'24h' | '7d'>('24h')
  const [fresh, setFresh] = useState(0)
  const { data, error, loading, reload } = usePoll<WebResp>(`/api/web/report?range=${range}${fresh ? `&fresh=1&n=${fresh}` : ''}`, 120000)
  const [ban, setBan] = useState<string | null>(null)
  const [limit, setLimit] = useState(20)

  const header = (
    <PageHeader
      title="Saldırılar"
      desc="Sitelerine gelen saldırı istekleri: hassas dosya arama, SQL injection, dizin dolaşma, saldırı araçları. Nginx loglarından okunur."
      action={
        <div className="flex flex-wrap gap-2">
          {switcher}
          <Tabs value={range} onChange={setRange} items={[{ id: '24h', label: '24 saat' }, { id: '7d', label: '7 gün' }]} />
          <Button onClick={() => { setFresh(Date.now()); reload() }} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>
        </div>
      }
    />
  )
  if (error && !data) return <div className="space-y-5">{header}<ErrorBox>{error}</ErrorBox></div>
  if (!data) return <div className="space-y-5">{header}<Loading /></div>
  const r = data.report
  if (!data.realip.nginx && r.requests === 0) {
    return <div className="space-y-5">{header}<Alert title="Nginx bulunamadı">Web saldırı analizi Nginx access loglarını kullanır.</Alert></div>
  }
  const confirmed = r.findings.filter((f) => f.confirmed)
  const suspect = r.findings.filter((f) => !f.confirmed)
  const cats = Object.entries(r.cats).sort((a, b) => b[1] - a[1])
  const catMax = Math.max(1, ...cats.map(([, n]) => n))
  const hmax = Math.max(1, ...r.hourly)

  return (
    <div className="space-y-5">
      {header}
      <RealIPCard d={data} onChange={() => { setFresh(Date.now()); reload() }} />

      {confirmed.map((f) => (
        <Alert key={f.url} tone="kirmizi" title="Hassas dosya dışarıdan okunabiliyor">
          <span className="font-mono">{f.url}</span>: bu dosyayı hemen erişime kapat ve içindeki şifreleri, anahtarları değiştir.
          Son 24 saatte {f.count} kez indirildi.
        </Alert>
      ))}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[
          ['Toplam istek', nf.format(r.requests)],
          ['Saldırı isteği', nf.format(r.attacks)],
          ['Saldıran IP', nf.format(r.attackIps)],
          ['En yaygın', cats[0] ? r.catNames[cats[0][0]] : '—'],
        ].map(([l, v]) => (
          <Card key={l}><CardContent className="py-4">
            <div className="text-sm text-muted-foreground">{l}</div>
            <div className="mt-1 truncate text-2xl font-semibold tabular-nums">{v}</div>
          </CardContent></Card>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-3 lg:items-start">
        <Card className="lg:col-span-2">
          <CardHeader title="Zaman içinde" description={range === '24h' ? 'Saatlik saldırı isteği' : 'Günlük saldırı isteği'} />
          <CardContent>
            <div className="flex h-28 items-end gap-[3px]">
              {r.hourly.map((v, i) => (
                <div key={i} className="flex h-full flex-1 items-end" title={`${nf.format(v)} istek`}>
                  <div className="w-full rounded-t-[3px] bg-destructive/70" style={{ height: `${Math.max(v ? 3 : 0, (v / hmax) * 100)}%` }} />
                </div>
              ))}
            </div>
            <div className="mt-1.5 flex justify-between text-xs text-muted-foreground"><span>{range === '24h' ? '24 saat önce' : '7 gün önce'}</span><span>şimdi</span></div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader title="Saldırı türleri" />
          <CardContent className="space-y-2.5">
            {cats.length === 0 ? <div className="text-sm text-muted-foreground">Saldırı yok.</div> : cats.map(([c, n]) => (
              <div key={c} className="text-sm">
                <div className="flex justify-between"><span>{r.catNames[c] ?? c}</span><span className="tabular-nums text-muted-foreground">{nf.format(n)}</span></div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className={cn('h-full', catTone[c] === 'kirmizi' ? 'bg-destructive' : catTone[c] === 'amber' ? 'bg-warning' : 'bg-muted-foreground/50')} style={{ width: `${(n / catMax) * 100}%` }} />
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader title="Saldıran IP'ler" description="Saldırı isteği gönderen veya çok sayıda 404 üreten adresler. Satıra tıklayınca denedikleri yollar görünür." />
        <CardContent className="px-0 pb-0">
          {r.topIps.length === 0 ? <Empty>Bu süre içinde saldırı yok.</Empty> : (
            <ul>{r.topIps.slice(0, limit).map((x) => <IPRow key={x.ip} x={x} names={r.catNames} onBan={setBan} />)}</ul>
          )}
          {r.topIps.length > limit && <div className="border-t px-6 py-3"><Button size="sm" variant="ghost" onClick={() => setLimit(limit + 40)}>Daha fazla göster ({r.topIps.length - limit})</Button></div>}
        </CardContent>
      </Card>

      {r.recent.length > 0 && (
        <Card>
          <CardHeader title="Son saldırı istekleri" />
          <CardContent className="px-0 pb-0">
            <ul className="max-h-96 overflow-y-auto">
              {r.recent.map((h, i) => (
                <li key={i} className="flex items-center gap-3 border-t px-6 py-1.5 text-xs">
                  <span className="w-12 shrink-0 text-muted-foreground tabular-nums">{new Date(h.ts * 1000).toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })}</span>
                  <span className="w-32 shrink-0 truncate font-mono">{h.ip}</span>
                  <span className={cn('w-10 shrink-0 tabular-nums', h.status === 200 ? 'text-warning' : 'text-muted-foreground')}>{h.status}</span>
                  <span className="min-w-0 flex-1 truncate font-mono" title={h.path}>{h.method} {h.path}</span>
                  <span className="hidden shrink-0 gap-1 sm:flex">{h.cats.map((c) => <Tag key={c} tone={catTone[c] ?? 'soluk'}>{r.catNames[c] ?? c}</Tag>)}</span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      {suspect.length > 0 && (
        <Card>
          <CardHeader title="Kontrol edilen şüpheli yanıtlar" description="Hassas bir dosya isteğine 200 dönmüş ama içerik gerçek dosyaya benzemiyor (çoğunlukla sitenin kendi sayfası)." />
          <CardContent className="space-y-1 text-sm">
            {suspect.map((f) => (
              <div key={f.site + f.path} className="flex items-start gap-2">
                <TriangleAlert size={14} className="mt-0.5 shrink-0 text-muted-foreground" />
                <span><span className="font-mono text-xs">{f.site ? f.site : ''}{f.path}</span> <span className="text-muted-foreground">· {f.note}</span></span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <p className="text-xs text-muted-foreground">
        Okunan loglar: {r.files?.length ? r.files.join(', ') : 'bulunamadı'} · {ago(r.generated)} hesaplandı.
      </p>
      <BanDialog ip={ban} cf={data.cloudflare} onClose={() => setBan(null)} onDone={reload} />
    </div>
  )
}
