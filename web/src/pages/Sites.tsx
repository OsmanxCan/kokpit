import { useState } from 'react'
import { RefreshCw, Activity, Plus, Wand2 } from 'lucide-react'
import { SiteWizard, StepList, type Step, type WizardInfo } from '../components/SiteWizard'
import { api, type Site } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { daysLeft, dateTime } from '../lib/format'
import { Panel, PageHeader, Loading, Button, ConfirmButton, Tag, Empty, ErrorBox, toast, Dialog } from '../components/ui'

type Probe = { host: string; expiry: number; issuer: string; ms: number; error?: string }

function CertTag({ expiry }: { expiry: number }) {
  if (!expiry) return <Tag>sertifika yok</Tag>
  const d = daysLeft(expiry)
  return <Tag tone={d < 7 ? 'kirmizi' : d < 21 ? 'amber' : 'yesil'}>{d < 0 ? 'süresi dolmuş' : `${d} gün kaldı`}</Tag>
}

export function Sites() {
  const { data, error, reload, loading } = usePoll<Site[]>('/api/nginx')
  const [probes, setProbes] = useState<Record<string, Probe | 'loading'>>({})
  const [out, setOut] = useState<{ ok: boolean; text: string } | null>(null)
  const wiz = usePoll<WizardInfo>('/api/sitewizard', 60000)
  const [wizOpen, setWizOpen] = useState(false)
  const [removed, setRemoved] = useState<{ host: string; steps: Step[] } | null>(null)

  async function removeSite(host: string) {
    try {
      const r = await api.del<{ steps: Step[] }>(`/api/sitewizard/${encodeURIComponent(host)}`)
      setRemoved({ host, steps: r.steps })
      reload(); wiz.reload()
    } catch (e) { toast.err((e as Error).message) }
  }

  async function probe(host: string) {
    setProbes((p) => ({ ...p, [host]: 'loading' }))
    try {
      const r = await api.get<Probe>(`/api/nginx/remote-cert?host=${encodeURIComponent(host)}`)
      setProbes((p) => ({ ...p, [host]: r }))
    } catch (e) {
      setProbes((p) => ({ ...p, [host]: { host, expiry: 0, issuer: '', ms: 0, error: (e as Error).message } }))
    }
  }

  async function nginx(action: 'test' | 'reload') {
    try {
      const r = await api.post<{ ok: boolean; output: string }>(`/api/nginx/${action}`)
      setOut({ ok: r.ok, text: r.output })
      if (action === 'reload') r.ok ? toast.ok('Nginx yeniden yüklendi') : toast.err('Nginx yeniden yüklenemedi')
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  const servers = data.filter((s) => !s.redirect || (s.proxyPass ?? []).length || s.root)

  return (
    <div className="space-y-5">
      <PageHeader
        title="Siteler"
        desc="Nginx'teki domainler, yönlendikleri yer ve SSL sertifikaları. Kontrol et butonu siteye internetten bağlanıp yanıt süresini ve dışarıya sunulan sertifikayı ölçer."
        action={
          <div className="flex flex-wrap gap-2">
            <Button onClick={reload} loading={loading}><RefreshCw size={14} /></Button>
            <Button variant="default" onClick={() => setWizOpen(true)}><Plus size={14} /> Yeni site</Button>
            <Button onClick={() => nginx('test')}>Ayarları test et</Button>
            <ConfirmButton onConfirm={() => nginx('reload')} variant="birincil" confirmText="Yeniden yükle?">Nginx'i yeniden yükle</ConfirmButton>
          </div>
        }
      />
      {out && (
        <pre className={`font-mono text-xs whitespace-pre-wrap rounded-md border px-3 py-2 ${out.ok ? 'border-success/40 text-success bg-success/5' : 'border-destructive/40 text-destructive bg-destructive/10'}`}>{out.text}</pre>
      )}
      <Panel pad={false}>
        {servers.length === 0 ? <Empty>/etc/nginx/sites-enabled içinde site bulunamadı.</Empty> : (
          <ul>
            {servers.map((s, i) => {
              const names = s.names ?? []
              const host = names.find((n) => n.includes('.')) ?? ''
              const p = host ? probes[host] : undefined
              const byWizard = (wiz.data?.sites ?? []).some((w) => w.host === host)
              return (
                <li key={s.file + i} className="px-4 py-3.5 border-b border-border/60 last:border-0 flex flex-wrap gap-x-6 gap-y-2 items-start">
                  <div className="min-w-0 flex-1 basis-72">
                    <div className="font-medium flex flex-wrap items-center gap-2">
                      {names.join(', ') || '(adsız)'}
                      {s.ssl ? <Tag tone="yesil">HTTPS</Tag> : <Tag>HTTP</Tag>}
                      {byWizard && <span title="Yeni site sihirbazı ile oluşturuldu"><Tag tone="mavi"><Wand2 size={11} /> sihirbaz</Tag></span>}
                    </div>
                    <div className="text-sm text-muted-foreground mt-0.5">
                      {(s.proxyPass ?? []).length > 0 ? <>→ <span className="font-mono text-xs text-foreground">{s.proxyPass!.join(', ')}</span></> :
                        s.root ? <>statik dosyalar: <span className="font-mono text-xs text-foreground">{s.root}</span></> : 'yönlendirme'}
                    </div>
                    <div className="text-xs text-muted-foreground mt-0.5">dosya: {s.file} · dinliyor: {(s.listen ?? []).join(', ')}</div>
                  </div>
                  <div className="text-sm min-w-52">
                    <div className="flex items-center gap-2"><span className="text-muted-foreground">Sunucudaki sertifika</span> <CertTag expiry={s.certExpiry} /></div>
                    {s.certExpiry > 0 && <div className="text-xs text-muted-foreground mt-0.5">{s.certIssuer} · {dateTime(s.certExpiry)}</div>}
                    {p && p !== 'loading' && (
                      <div className="mt-1.5 text-xs">
                        {p.error ? <span className="text-destructive">Ulaşılamadı: {p.error}</span> : (
                          <span>
                            <span className={p.ms > 1500 ? 'text-warning' : 'text-success'}>{p.ms} ms</span>
                            <span className="text-muted-foreground"> · dışarıdaki sertifika: {p.issuer} ({daysLeft(p.expiry)} gün)</span>
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                  {host && (
                    <Button variant="hayalet" onClick={() => probe(host)} loading={p === 'loading'}><Activity size={14} /> Kontrol et</Button>
                  )}
                  {byWizard && (
                    <ConfirmButton variant="hayalet" onConfirm={() => removeSite(host)} confirmText="Site kaldırılsın mı?">Kaldır</ConfirmButton>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </Panel>
      <SiteWizard open={wizOpen} info={wiz.data ?? null} onClose={() => setWizOpen(false)} onDone={() => { reload(); wiz.reload() }} />
      <Dialog open={!!removed} onClose={() => setRemoved(null)} title={`${removed?.host ?? ''} kaldırıldı`}
        description="Uygulamanın dosyalarına dokunulmadı; sadece yayın ayarları kaldırıldı."
        footer={<Button variant="default" onClick={() => setRemoved(null)}>Kapat</Button>}>
        {removed && <StepList steps={removed.steps} />}
      </Dialog>
    </div>
  )
}
