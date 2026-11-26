// Yeni site sihirbazı: alan adı → Nginx + tünel + DNS + uptime, tek seferde.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { CheckCircle2, XCircle, CircleMinus, Info, Undo2, TriangleAlert, ExternalLink, Rocket, ChevronDown, Check, X } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { Button, Dialog, ErrorBox, Field, Input, Loading, Switch, Tabs, cn } from './ui'

export type WizardInfo = {
  tunnel: { available: boolean; path: string; tunnelId?: string; rules?: { hostname: string; service: string }[]; error?: string }
  cloudflare: boolean; cert: boolean
  sites: { host: string; kind: string; port?: number; root?: string; tunnel: boolean; dnsMethod?: string; createdAt: number }[]
}
type Req = { host: string; kind: 'proxy' | 'static'; port: number; root: string; spa: boolean; bodyMb: number; tunnel: boolean; dns: boolean; uptime: boolean }
type Preview = { host: string; nginx: string; nginxAt: string; tunnel: string; tunnelId: string; dns: string; dnsMode: string; warnings: string[] }
export type Step = { name: string; status: 'ok' | 'skipped' | 'failed' | 'rolledback' | 'info'; detail: string }

export type Job = { id: string; op: string; host: string; done: boolean; error?: string; steps: Step[] }

// waitJob arka plan işinin bitmesini bekler. Panel Cloudflare Tünel arkasındaysa tünel yeniden
// başlarken istekler birkaç saniye başarısız olur; bu süre boyunca sormaya devam eder.
export async function waitJob(id: string, onWaiting: (offline: boolean) => void): Promise<Job> {
  const until = Date.now() + 3 * 60_000
  while (Date.now() < until) {
    await new Promise((r) => setTimeout(r, 1500))
    try {
      const j = await api.get<Job>(`/api/sitewizard/jobs/${id}`)
      onWaiting(false)
      if (j.done) return j
    } catch (e) {
      if (e instanceof ApiError && (e.status === 404 || e.status === 401)) throw e
      onWaiting(true) // bağlantı kopuk: tünel yeniden başlıyor olabilir
    }
  }
  throw new Error('İşlem 3 dakikada bitmedi. Siteler listesini ve İşlem kayıtlarını kontrol et.')
}

export function StepList({ steps }: { steps: Step[] }) {
  const icon = { ok: CheckCircle2, failed: XCircle, skipped: CircleMinus, info: Info, rolledback: Undo2 }
  const tone = { ok: 'text-success', failed: 'text-destructive', skipped: 'text-muted-foreground', info: 'text-info', rolledback: 'text-warning' }
  return (
    <ul className="divide-y rounded-lg border">
      {steps.map((s, i) => {
        const I = icon[s.status]
        return (
          <li key={i} className="flex items-start gap-3 px-4 py-2.5 text-sm">
            <I size={16} className={cn('mt-0.5 shrink-0', tone[s.status])} />
            <div className="min-w-0"><div className="font-medium">{s.name}</div><div className="break-words text-muted-foreground">{s.detail}</div></div>
          </li>
        )
      })}
    </ul>
  )
}

// Hangi projeler hangi türle yayınlanır: [ad, nasıl]
const SUPPORT: Record<'proxy' | 'static', { title: string; items: [string, string][] }> = {
  proxy: {
    title: 'Sunucuda bir portu dinleyen her uygulama',
    items: [
      ['Next.js, Nuxt, SvelteKit, Remix, Astro (SSR)', 'next start, node .output/server/index.mjs gibi; PM2 ile çalıştır'],
      ['NestJS, Express, Fastify, Hono', 'node dist/main.js; PM2 ile çalıştır'],
      ['Laravel', 'Octane (php artisan octane:start) ile; php artisan serve sadece deneme içindir'],
      ['Django, FastAPI, Flask', 'gunicorn veya uvicorn ile 127.0.0.1:<port>'],
      ['Go, Rust, .NET, Spring Boot, Bun, Deno', 'derlenmiş programı systemd veya PM2 ile çalıştır'],
      ['Docker konteynerleri', 'portu 127.0.0.1:<port>:<port> olarak yayınla (yoksa firewall atlanır)'],
      ['Hazır uygulamalar', 'Strapi, Ghost, n8n, Grafana, Uptime Kuma, Gitea…'],
    ],
  },
  static: {
    title: 'Derleme çıktısı HTML/CSS/JS olan siteler',
    items: [
      ['React, Vue, Svelte, Solid (Vite)', 'klasör: dist; tek sayfa uygulaması seçeneğini aç'],
      ['Angular', 'klasör: dist/<proje>/browser; tek sayfa uygulaması seçeneğini aç'],
      ['Next.js (output: export)', 'klasör: out'],
      ['Astro, Nuxt generate, SvelteKit adapter-static', 'klasör: dist, .output/public veya build'],
      ['Hugo, Jekyll, Gatsby, Eleventy, VitePress', 'klasör: public, _site, _site, .vitepress/dist'],
      ['Düz HTML siteleri', 'index.html\'in bulunduğu klasör'],
    ],
  },
}
const UNSUPPORTED: [string, string][] = [
  ['PHP-FPM ile çalışan siteler', 'WordPress, klasik Laravel/PHP kurulumları (şimdilik elle Nginx ayarı gerekir)'],
  ['Tünelsiz HTTPS', 'tünel kullanmıyorsan sertifikayı certbot ile ayrıca almalısın'],
]

function SupportInfo({ kind }: { kind: 'proxy' | 'static' }) {
  const d = SUPPORT[kind]
  return (
    <details className="group rounded-lg border">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-2.5 text-sm font-medium [&::-webkit-details-marker]:hidden">
        <span className="flex items-center gap-2"><Info size={15} className="text-muted-foreground" /> Hangi projeler destekleniyor?</span>
        <ChevronDown size={16} className="text-muted-foreground transition-transform group-open:rotate-180" />
      </summary>
      <div className="space-y-3 border-t px-4 py-3 text-sm">
        <div className="text-muted-foreground">{d.title}:</div>
        <ul className="space-y-1.5">
          {d.items.map(([a, b]) => (
            <li key={a} className="flex gap-2"><Check size={15} className="mt-0.5 shrink-0 text-success" /><span><span className="font-medium">{a}</span> <span className="text-muted-foreground">· {b}</span></span></li>
          ))}
        </ul>
        <div className="text-muted-foreground">Desteklenmeyenler:</div>
        <ul className="space-y-1.5">
          {UNSUPPORTED.map(([a, b]) => (
            <li key={a} className="flex gap-2"><X size={15} className="mt-0.5 shrink-0 text-destructive" /><span><span className="font-medium">{a}</span> <span className="text-muted-foreground">· {b}</span></span></li>
          ))}
        </ul>
      </div>
    </details>
  )
}

export function WorkingBox({ offline, tunnel }: { offline: boolean; tunnel: boolean }) {
  return (
    <div className="flex flex-col items-center gap-3 py-8 text-center">
      <Loading />
      <div className="max-w-md text-sm text-muted-foreground">
        {offline
          ? 'Panel bağlantısı geçici olarak koptu. Tünel yeniden başlıyor olabilir; bağlantı gelince sonuç burada görünecek.'
          : tunnel ? 'Nginx ayarlanıyor, tünel yeniden başlatılıyor. Panel tünelin arkasındaysa bağlantı birkaç saniye kopabilir; bu normal.' : 'Nginx ayarlanıyor…'}
      </div>
    </div>
  )
}

export function SiteWizard({ open, info, onClose, onDone }: { open: boolean; info: WizardInfo | null; onClose: () => void; onDone: () => void }) {
  const nav = useNavigate()
  const blank: Req = { host: '', kind: 'proxy', port: 3000, root: '/var/www/', spa: true, bodyMb: 50, tunnel: true, dns: true, uptime: true }
  const [f, setF] = useState<Req>(blank)
  const [stage, setStageRaw] = useState<'form' | 'preview' | 'working' | 'result'>('form')
  const stageRef = useRef(stage)
  const setStage = (v: typeof stage) => { stageRef.current = v; setStageRaw(v) }
  const [preview, setPreview] = useState<Preview | null>(null)
  const [steps, setSteps] = useState<Step[]>([])
  const [failed, setFailed] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [offline, setOffline] = useState(false)

  useEffect(() => {
    if (open) {
      setF({ ...blank, tunnel: !!info?.tunnel.available })
      setStage('form'); setPreview(null); setSteps([]); setErr(''); setFailed('')
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  const set = <K extends keyof Req>(k: K, v: Req[K]) => setF((x) => ({ ...x, [k]: v }))

  async function next() {
    setBusy(true); setErr('')
    try { setPreview(await api.post<Preview>('/api/sitewizard/preview', f)); setStage('preview') } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  async function create() {
    setBusy(true); setErr('')
    try {
      const { job } = await api.post<{ job: string }>('/api/sitewizard', f)
      setStage('working')
      const j = await waitJob(job, setOffline)
      setSteps(j.steps)
      setFailed(j.error ?? '')
      setStage('result')
      onDone()
    } catch (e) {
      setErr((e as Error).message)
      if (stageRef.current === 'working') setStage('preview')
    } finally { setBusy(false); setOffline(false) }
  }

  const host = preview?.host ?? f.host
  const footer = stage === 'form' ? <>
    <Button onClick={onClose}>Vazgeç</Button>
    <Button variant="default" onClick={next} loading={busy} disabled={!f.host.trim()}>Devam</Button>
  </> : stage === 'preview' ? <>
    <Button onClick={() => setStage('form')}>Geri</Button>
    <Button variant="default" onClick={create} loading={busy}>Siteyi oluştur</Button>
  </> : stage === 'working' ? <>
    <Button disabled>Kapat</Button>
  </> : <>
    {!failed && <Button className="mr-auto" onClick={() => { onClose(); nav(`/deploy?yeni=1&ad=${encodeURIComponent(host)}${f.kind === 'static' ? `&klasor=${encodeURIComponent(f.root)}` : ''}`) }}><Rocket size={14} /> Deploy projesi ekle</Button>}
    {!failed && <a href={`https://${host}`} target="_blank" rel="noopener noreferrer"><Button><ExternalLink size={14} /> Siteyi aç</Button></a>}
    <Button variant="default" onClick={onClose}>Kapat</Button>
  </>

  return (
    <Dialog open={open} onClose={stage === 'working' ? () => {} : onClose} wide footer={footer}
      title={stage === 'form' ? 'Yeni site' : stage === 'preview' ? 'Yapılacaklar' : stage === 'working' ? 'Site oluşturuluyor' : failed ? 'Site oluşturulamadı' : 'Site hazır'}
      description={stage === 'form' ? 'Alan adını ve sitenin nereden sunulacağını seç. Sonraki adımda yapılacak her değişikliği görüp onaylayacaksın.'
        : stage === 'preview' ? 'Bir adım başarısız olursa o ana kadar yapılanlar geri alınır.' : stage === 'working' ? 'Bu pencereyi kapatma.' : failed ? failed : 'DNS kaydının yayılması birkaç dakika sürebilir.'}>
      {!info ? <Loading /> : stage === 'form' ? (
        <div className="space-y-4">
          <Field label="Alan adı"><Input value={f.host} onChange={(e) => set('host', e.target.value)} placeholder="uygulama.example.com" autoFocus /></Field>
          <Tabs value={f.kind} onChange={(v) => set('kind', v)} items={[{ id: 'proxy', label: 'Çalışan uygulama (port)' }, { id: 'static', label: 'Statik dosyalar (klasör)' }]} />
          <SupportInfo kind={f.kind} />
          {f.kind === 'proxy' ? (
            <Field label="Uygulamanın portu" hint="Sunucunda çalışan uygulamanın dinlediği port (örn. Next.js için 3000)">
              <Input type="number" value={f.port} onChange={(e) => set('port', Number(e.target.value))} className="w-40" />
            </Field>
          ) : (
            <div className="space-y-3">
              <Field label="Site klasörü" hint="index.html'in bulunduğu klasör (örn. derleme çıktısı)"><Input value={f.root} onChange={(e) => set('root', e.target.value)} className="font-mono" /></Field>
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" checked={f.spa} onChange={(e) => set('spa', e.target.checked)} className="accent-[var(--brand)]" />
                Tek sayfa uygulaması (React, Vue): bulunamayan yollar index.html'e gitsin
              </label>
            </div>
          )}
          <div className="divide-y rounded-lg border">
            <label className={cn('flex items-start justify-between gap-6 px-4 py-3 cursor-pointer', !info.tunnel.available && 'opacity-50')}>
              <div>
                <div className="text-sm font-medium">Cloudflare Tünel'e ekle</div>
                <p className="text-xs text-muted-foreground">{info.tunnel.available ? `${info.tunnel.path} dosyasına yönlendirme kuralı eklenir.` : 'Sunucuda yerel olarak yönetilen bir tünel bulunamadı.'}</p>
              </div>
              <Switch checked={f.tunnel} onChange={(v) => set('tunnel', v)} disabled={!info.tunnel.available} label="Tünel" />
            </label>
            <label className="flex items-start justify-between gap-6 px-4 py-3 cursor-pointer">
              <div>
                <div className="text-sm font-medium">DNS kaydı oluştur</div>
                <p className="text-xs text-muted-foreground">{info.cloudflare ? 'Cloudflare API ile.' : info.cert ? 'cloudflared ile.' : 'Cloudflare token yok; eklenecek kayıt sana gösterilir.'}</p>
              </div>
              <Switch checked={f.dns} onChange={(v) => set('dns', v)} label="DNS" />
            </label>
            <label className="flex items-start justify-between gap-6 px-4 py-3 cursor-pointer">
              <div>
                <div className="text-sm font-medium">Uptime izleyicisi ekle</div>
                <p className="text-xs text-muted-foreground">Site çökerse haber verir.</p>
              </div>
              <Switch checked={f.uptime} onChange={(v) => set('uptime', v)} label="Uptime" />
            </label>
          </div>
          {err && <ErrorBox>{err}</ErrorBox>}
        </div>
      ) : stage === 'preview' && preview ? (
        <div className="space-y-4">
          {preview.warnings.map((w) => (
            <div key={w} className="flex gap-2 rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-sm text-muted-foreground"><TriangleAlert size={16} className="mt-0.5 shrink-0 text-warning" />{w}</div>
          ))}
          <div>
            <div className="mb-1.5 text-sm font-medium">1. Nginx: <span className="font-mono text-xs font-normal text-muted-foreground">{preview.nginxAt}</span></div>
            <pre className="max-h-64 overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-xs">{preview.nginx}</pre>
          </div>
          <div>
            <div className="mb-1.5 text-sm font-medium">2. Tünel</div>
            {preview.tunnel ? <pre className="overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-xs">{preview.tunnel}</pre> : <div className="text-sm text-muted-foreground">Kullanılmayacak.</div>}
          </div>
          <div>
            <div className="mb-1.5 text-sm font-medium">3. DNS</div>
            <div className="text-sm text-muted-foreground">{preview.dns || 'Yapılmayacak.'}</div>
          </div>
          {f.uptime && <div><div className="mb-1.5 text-sm font-medium">4. Uptime</div><div className="text-sm text-muted-foreground">https://{preview.host} izlenecek.</div></div>}
          {err && <ErrorBox>{err}</ErrorBox>}
        </div>
      ) : stage === 'working' ? (
        <WorkingBox offline={offline} tunnel={f.tunnel} />
      ) : (
        <StepList steps={steps} />
      )}
    </Dialog>
  )
}
