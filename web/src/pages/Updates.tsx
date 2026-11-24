import { useState } from 'react'
import { RefreshCw, Download, ExternalLink, CheckCircle2 } from 'lucide-react'
import { api, type UpdateStatus } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago } from '../lib/format'
import { Panel, PageHeader, Loading, Button, ConfirmButton, ErrorBox, Tag, toast } from '../components/ui'

export function Updates() {
  const { data, error, setData } = usePoll<UpdateStatus>('/api/update')
  const [checking, setChecking] = useState(false)
  const [applying, setApplying] = useState(false)

  async function check() {
    setChecking(true)
    try {
      setData(await api.post<UpdateStatus>('/api/update/check'))
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setChecking(false)
    }
  }

  async function apply() {
    setApplying(true)
    try {
      const r = await api.post<{ version: string }>('/api/update/apply')
      toast.ok(`${r.version} kuruldu. Panel yeniden başlıyor…`)
      // Panel yeniden başlayınca sayfayı tazele
      const started = Date.now()
      const wait = async () => {
        try {
          const res = await fetch('/api/auth/me', { credentials: 'same-origin' })
          if (res.status !== 502 && res.status !== 503 && Date.now() - started > 3000) return window.location.reload()
        } catch { /* henüz açılmadı */ }
        window.setTimeout(wait, 1500)
      }
      window.setTimeout(wait, 2500)
    } catch (e) {
      toast.err((e as Error).message)
      setApplying(false)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const l = data.latest

  return (
    <div className="space-y-5 max-w-3xl">
      <PageHeader
        title="Güncellemeler"
        desc="Panel, GitHub'daki yayınlanmış sürümleri 6 saatte bir kontrol eder. Güncelleme indirilir, SHA-256 özeti doğrulanır ve panel kendini yeniden başlatır."
        action={<Button onClick={check} loading={checking}><RefreshCw size={14} /> Şimdi kontrol et</Button>}
      />

      <Panel>
        <div className="grid sm:grid-cols-2 gap-4 pt-3">
          <div>
            <div className="text-sm text-soluk">Kurulu sürüm</div>
            <div className="text-2xl font-semibold mt-0.5">{data.current}</div>
            <div className="text-xs text-soluk">{data.arch}</div>
          </div>
          <div>
            <div className="text-sm text-soluk">GitHub'daki son sürüm</div>
            <div className={`text-2xl font-semibold mt-0.5 ${data.available ? 'text-amber' : ''}`}>{l?.version ?? '—'}</div>
            <div className="text-xs text-soluk">
              {data.repo ? <a href={`https://github.com/${data.repo}`} target="_blank" rel="noreferrer" className="hover:text-amber">{data.repo}</a> : 'depo ayarlı değil'}
              {data.checkedAt ? ` · kontrol: ${ago(data.checkedAt)}` : ''}
            </div>
          </div>
        </div>

        {data.error && <div className="mt-4"><ErrorBox>{data.error}</ErrorBox></div>}
        {!data.enabled && <div className="mt-4 text-sm text-soluk">Otomatik kontrol kapalı (PANEL_UPDATE_CHECK=false). "Şimdi kontrol et" ile elle bakabilirsin.</div>}

        <div className="mt-5 pt-4 border-t border-cizgi">
          {data.available && l ? (
            <div className="flex flex-wrap items-center gap-3">
              <ConfirmButton variant="birincil" onConfirm={apply} loading={applying} confirmText={`${l.version} kurulsun mu?`} disabled={!data.canApply}>
                <Download size={14} /> {l.version} sürümünü kur
              </ConfirmButton>
              {!data.canApply && <span className="text-sm text-kirmizi">Panelin bulunduğu klasöre yazma izni yok. Sunucuda: kokpit guncelle</span>}
              <a href={l.url} target="_blank" rel="noreferrer" className="text-sm text-soluk hover:text-amber flex items-center gap-1">GitHub'da gör <ExternalLink size={13} /></a>
            </div>
          ) : l ? (
            <div className="flex items-center gap-2 text-yesil"><CheckCircle2 size={18} /> En güncel sürümü kullanıyorsun.</div>
          ) : (
            <div className="text-sm text-soluk">{data.error ? 'Sürüm bilgisi alınamadı.' : 'Henüz kontrol edilmedi.'}</div>
          )}
        </div>
      </Panel>

      {l && (
        <Panel title={<span className="flex items-center gap-2">{l.name || l.tag} {data.available && <Tag tone="amber">yeni</Tag>}</span>}>
          <pre className="whitespace-pre-wrap font-sans text-sm text-metin/90 leading-relaxed">{l.notes || 'Sürüm notu yok.'}</pre>
        </Panel>
      )}

      <Panel title="Terminalden güncelleme">
        <pre className="font-mono text-xs bg-gece border border-cizgi rounded-md p-3 overflow-x-auto">sudo kokpit guncelle</pre>
        <p className="text-sm text-soluk mt-2">Panelden kurulum başarısız olursa eski sürüm <span className="font-mono text-xs">/usr/local/bin/kokpit.eski</span> olarak saklanır.</p>
      </Panel>
    </div>
  )
}
