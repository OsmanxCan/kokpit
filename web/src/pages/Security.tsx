import { useState } from 'react'
import { CheckCircle2, XCircle, Info, RefreshCw, Ban, Unlock } from 'lucide-react'
import { api, type SecReport } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { dateTime } from '../lib/format'
import { Panel, PageHeader, Loading, Button, ConfirmButton, Tag, Empty, ErrorBox, toast } from '../components/ui'
import { ScoreGauge } from '../components/charts'

export function Security() {
  const [fresh, setFresh] = useState(0)
  const { data, error, reload, loading } = usePoll<SecReport>(`/api/security${fresh ? '?fresh=1&n=' + fresh : ''}`, 60000)
  const [banIP, setBanIP] = useState('')
  const [showRules, setShowRules] = useState(false)

  async function ban(ip: string, b: boolean) {
    try {
      await api.post('/api/security/ban', { ip, ban: b })
      toast.ok(b ? `${ip} engellendi` : `${ip} engeli kaldırıldı`)
      setBanIP('')
      reload()
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const checks = [...data.checks].sort((a, b) => Number(a.ok) - Number(b.ok) || b.weight - a.weight)
  const ports = data.ports ?? []
  const banned = data.banned ?? []

  return (
    <div className="space-y-5">
      <PageHeader
        title="Güvenlik"
        desc={`Son tarama ${dateTime(data.ts)}. Rapor 5 dakikada bir kendiliğinden yenilenir.`}
        action={<Button onClick={() => setFresh(Date.now())} loading={loading}><RefreshCw size={14} /> Şimdi tara</Button>}
      />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,300px)_1fr] items-start">
        <Panel className="lg:sticky lg:top-6">
          <div className="flex flex-col items-center pt-4">
            <ScoreGauge score={data.score} size={220} />
            <p className="text-sm text-soluk text-center mt-2 max-w-[30ch]">
              {checks.filter((c) => c.ok).length} / {checks.length} kontrol geçti. Kritik maddeler skoru en çok etkiler.
            </p>
          </div>
        </Panel>
        <Panel title="Kontrol listesi" pad={false}>
          <ul className="border-t border-cizgi">
            {checks.map((c) => {
              const Icon = c.ok ? CheckCircle2 : c.level === 'info' ? Info : XCircle
              const color = c.ok ? 'text-yesil' : c.level === 'critical' ? 'text-kirmizi' : c.level === 'warning' ? 'text-amber' : 'text-mavi'
              return (
                <li key={c.id} className="flex gap-3 px-4 py-2.5 border-b border-cizgi/60 last:border-0">
                  <Icon size={17} className={`${color} mt-0.5 shrink-0`} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={c.ok ? '' : 'font-medium'}>{c.title}</span>
                      <span className={`text-sm ${c.ok ? 'text-yesil' : 'text-soluk'}`}>{c.detail}</span>
                      {!c.ok && <Tag tone={c.level === 'critical' ? 'kirmizi' : c.level === 'warning' ? 'amber' : 'mavi'}>{{ critical: 'kritik', warning: 'uyarı', info: 'öneri' }[c.level]}</Tag>}
                    </div>
                    {!c.ok && <div className="text-xs mt-1 text-metin/80">Çözüm: {c.fix}</div>}
                  </div>
                </li>
              )
            })}
          </ul>
        </Panel>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title={<span>Başarısız SSH denemeleri <span className="text-soluk font-normal">son 24 saat · {data.sshFail24h}</span></span>} pad={false}>
          {data.sshFailTop.length === 0 ? <Empty>Son 24 saatte başarısız deneme yok.</Empty> : (
            <table className="w-full text-sm">
              <thead><tr className="text-soluk text-left border-y border-cizgi">
                <th className="font-normal px-4 py-2">IP</th><th className="font-normal px-2 py-2 text-right">Deneme</th>
                <th className="font-normal px-2 py-2">Denenen kullanıcılar</th><th className="px-4" />
              </tr></thead>
              <tbody>
                {data.sshFailTop.map((f) => (
                  <tr key={f.ip} className="border-b border-cizgi/50 last:border-0">
                    <td className="px-4 py-1.5 font-mono text-xs">{f.ip}</td>
                    <td className="px-2 py-1.5 text-right">{f.count}</td>
                    <td className="px-2 py-1.5 text-soluk truncate max-w-[200px]">{f.users}</td>
                    <td className="px-4 py-1.5 text-right">
                      {data.fail2ban && !banned.includes(f.ip) && (
                        <ConfirmButton variant="hayalet" onConfirm={() => ban(f.ip, true)} confirmText="Engelle?"><Ban size={13} /></ConfirmButton>
                      )}
                      {banned.includes(f.ip) && <Tag tone="kirmizi">engelli</Tag>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>

        <Panel title="Başarılı SSH girişleri" pad={false}>
          {data.sshAccepted.length === 0 ? <Empty>Son 24 saatte SSH girişi yok.</Empty> : (
            <ul className="border-t border-cizgi">
              {data.sshAccepted.map((e, i) => (
                <li key={i} className="px-4 py-2 border-b border-cizgi/50 last:border-0 flex items-center gap-3 text-sm">
                  <span className="font-medium">{e.user}</span>
                  <span className="font-mono text-xs">{e.ip}</span>
                  <Tag tone={e.method === 'password' ? 'amber' : 'yesil'}>{e.method === 'password' ? 'şifre' : e.method === 'publickey' ? 'anahtar' : e.method}</Tag>
                  <span className="ml-auto text-xs text-soluk">{e.ts.replace('T', ' ').slice(0, 19)}</span>
                </li>
              ))}
            </ul>
          )}
          {data.sessions.length > 0 && (
            <div className="px-4 py-3 border-t border-cizgi text-sm">
              <div className="text-soluk mb-1">Şu an bağlı</div>
              {data.sessions.map((s, i) => (
                <div key={i}>{s.user} <span className="text-soluk">{s.tty} · {s.from || 'yerel'} · {s.when}</span></div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title={<span>Dinleyen portlar <span className="text-soluk font-normal">{ports.filter((p) => p.public).length} dışarıya açık</span></span>} pad={false}>
          <table className="w-full text-sm">
            <thead><tr className="text-soluk text-left border-y border-cizgi">
              <th className="font-normal px-4 py-2">Port</th><th className="font-normal px-2 py-2">Adres</th>
              <th className="font-normal px-2 py-2">Süreç</th><th className="font-normal px-4 py-2">Erişim</th>
            </tr></thead>
            <tbody>
              {ports.map((p, i) => (
                <tr key={i} className="border-b border-cizgi/50 last:border-0">
                  <td className="px-4 py-1.5">{p.port}<span className="text-soluk">/{p.proto}</span></td>
                  <td className="px-2 py-1.5 font-mono text-xs">{p.addr}</td>
                  <td className="px-2 py-1.5">{p.process || <span className="text-soluk">çekirdek</span>}</td>
                  <td className="px-4 py-1.5">{p.public ? <Tag tone="amber">dışarıya açık</Tag> : <Tag>yerel</Tag>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>

        <Panel title="Firewall ve engellenen IP'ler">
          <div className="flex flex-wrap items-center gap-2 text-sm mb-3">
            <span>UFW:</span>
            <Tag tone={data.ufw === 'active' ? 'yesil' : 'kirmizi'}>{data.ufw === 'active' ? 'açık' : data.ufw}</Tag>
            <span className="ml-3">Fail2ban:</span>
            <Tag tone={data.fail2ban ? 'yesil' : 'kirmizi'}>{data.fail2ban ? 'çalışıyor' : 'çalışmıyor'}</Tag>
            {data.ufwRules && <Button variant="hayalet" className="ml-auto" onClick={() => setShowRules((x) => !x)}>{showRules ? 'Kuralları gizle' : 'Kuralları göster'}</Button>}
          </div>
          {showRules && <pre className="font-mono text-xs bg-gece border border-cizgi rounded-md p-3 overflow-x-auto mb-3">{data.ufwRules}</pre>}
          {data.fail2ban ? (
            <>
              <form className="flex gap-2 mb-3" onSubmit={(e) => { e.preventDefault(); if (banIP) ban(banIP.trim(), true) }}>
                <input value={banIP} onChange={(e) => setBanIP(e.target.value)} placeholder="IP adresi engelle" className="h-8 flex-1 bg-gece border border-cizgi rounded-md px-2 text-sm placeholder:text-soluk" />
                <Button type="submit" variant="tehlike"><Ban size={14} /> Engelle</Button>
              </form>
              {banned.length === 0 ? <div className="text-sm text-soluk">Şu an engellenmiş IP yok.</div> : (
                <ul className="flex flex-wrap gap-2">
                  {banned.map((ip) => (
                    <li key={ip} className="flex items-center gap-1 bg-gece border border-cizgi rounded-md pl-2 text-xs font-mono">
                      {ip}
                      <ConfirmButton variant="hayalet" className="h-6 px-1.5" onConfirm={() => ban(ip, false)} confirmText="Kaldır?" title="Engeli kaldır"><Unlock size={12} /></ConfirmButton>
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : <div className="text-sm text-soluk">IP engellemek için fail2ban kurulu ve çalışıyor olmalı.</div>}
          <div className="mt-4 pt-3 border-t border-cizgi text-sm flex flex-wrap gap-x-5 gap-y-1">
            <span>Güncellemeler: <b className={data.secUpdates ? 'text-amber font-medium' : 'font-medium'}>{data.updates}</b> <span className="text-soluk">({data.secUpdates} güvenlik)</span></span>
            <span>Yeniden başlatma: <b className="font-medium">{data.reboot ? 'gerekli' : 'gerekmiyor'}</b></span>
          </div>
        </Panel>
      </div>
    </div>
  )
}
