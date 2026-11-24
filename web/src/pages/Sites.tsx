import { useState } from 'react'
import { RefreshCw, Activity } from 'lucide-react'
import { api, type Site } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { daysLeft, dateTime } from '../lib/format'
import { Panel, PageHeader, Loading, Button, ConfirmButton, Tag, Empty, ErrorBox, toast } from '../components/ui'

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
          <div className="flex gap-2">
            <Button onClick={reload} loading={loading}><RefreshCw size={14} /></Button>
            <Button onClick={() => nginx('test')}>Ayarları test et</Button>
            <ConfirmButton onConfirm={() => nginx('reload')} variant="birincil" confirmText="Yeniden yükle?">Nginx'i yeniden yükle</ConfirmButton>
          </div>
        }
      />
      {out && (
        <pre className={`font-mono text-xs whitespace-pre-wrap rounded-md border px-3 py-2 ${out.ok ? 'border-yesil/40 text-yesil bg-yesil/5' : 'border-kirmizi/40 text-kirmizi bg-kirmizi/10'}`}>{out.text}</pre>
      )}
      <Panel pad={false}>
        {servers.length === 0 ? <Empty>/etc/nginx/sites-enabled içinde site bulunamadı.</Empty> : (
          <ul>
            {servers.map((s, i) => {
              const names = s.names ?? []
              const host = names.find((n) => n.includes('.')) ?? ''
              const p = host ? probes[host] : undefined
              return (
                <li key={s.file + i} className="px-4 py-3.5 border-b border-cizgi/60 last:border-0 flex flex-wrap gap-x-6 gap-y-2 items-start">
                  <div className="min-w-0 flex-1 basis-72">
                    <div className="font-medium flex flex-wrap items-center gap-2">
                      {names.join(', ') || '(adsız)'}
                      {s.ssl ? <Tag tone="yesil">HTTPS</Tag> : <Tag>HTTP</Tag>}
                    </div>
                    <div className="text-sm text-soluk mt-0.5">
                      {(s.proxyPass ?? []).length > 0 ? <>→ <span className="font-mono text-xs text-metin">{s.proxyPass!.join(', ')}</span></> :
                        s.root ? <>statik dosyalar: <span className="font-mono text-xs text-metin">{s.root}</span></> : 'yönlendirme'}
                    </div>
                    <div className="text-xs text-soluk mt-0.5">dosya: {s.file} · dinliyor: {(s.listen ?? []).join(', ')}</div>
                  </div>
                  <div className="text-sm min-w-52">
                    <div className="flex items-center gap-2"><span className="text-soluk">Sunucudaki sertifika</span> <CertTag expiry={s.certExpiry} /></div>
                    {s.certExpiry > 0 && <div className="text-xs text-soluk mt-0.5">{s.certIssuer} · {dateTime(s.certExpiry)}</div>}
                    {p && p !== 'loading' && (
                      <div className="mt-1.5 text-xs">
                        {p.error ? <span className="text-kirmizi">Ulaşılamadı: {p.error}</span> : (
                          <span>
                            <span className={p.ms > 1500 ? 'text-amber' : 'text-yesil'}>{p.ms} ms</span>
                            <span className="text-soluk"> · dışarıdaki sertifika: {p.issuer} ({daysLeft(p.expiry)} gün)</span>
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                  {host && (
                    <Button variant="hayalet" onClick={() => probe(host)} loading={p === 'loading'}><Activity size={14} /> Kontrol et</Button>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </Panel>
    </div>
  )
}
