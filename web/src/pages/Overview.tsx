import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, CheckCircle2, ChevronRight } from 'lucide-react'
import type { Overview as OV, Snapshot } from '../lib/api'
import { usePoll, useStream } from '../lib/hooks'
import { bytes, duration, pct, rate, levelColor, ago, daysLeft } from '../lib/format'
import { Panel, Loading, ErrorBox, Dot, Tag, Bar } from '../components/ui'
import { ScoreGauge, Sparkline } from '../components/charts'

function Vital({ label, value, sub, bar, color, spark, sparkMax }: {
  label: string; value: string; sub: string; bar?: number; color: string; spark: number[]; sparkMax?: number
}) {
  return (
    <div className="bg-panel border border-cizgi rounded-lg p-4 flex flex-col">
      <div className="text-sm text-soluk">{label}</div>
      <div className="text-3xl font-semibold mt-0.5" style={{ color }}>{value}</div>
      <div className="text-xs text-soluk mt-0.5 mb-3 truncate" title={sub}>{sub}</div>
      {bar !== undefined && <div className="mb-3"><Bar value={bar} color={color} /></div>}
      <div className="mt-auto"><Sparkline values={spark} color={color} max={sparkMax} height={44} /></div>
    </div>
  )
}

export function Overview() {
  const { data, error } = usePoll<OV>('/api/overview', 30000)
  const [live, setLive] = useState<Snapshot | null>(null)
  const [hist, setHist] = useState<Snapshot[]>([])
  useStream<Snapshot>('/api/system/stream', (s) => {
    setLive(s)
    setHist((h) => [...h, s].slice(-60))
  })

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const s = live ?? data.system
  const sec = data.security
  const failed = sec.failed ?? []
  const problems = data.problems ?? []
  const certs = (data.sites ?? []).filter((x) => x.certExpiry > 0)
  const soonCerts = certs.filter((c) => daysLeft(c.certExpiry) < 21)

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">{s.hostname}</h1>
        <div className="text-soluk text-sm">{s.os} · çalışma süresi {duration(s.uptime)} · {s.procCount} süreç</div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,330px)_1fr]">
        <Panel title="Güvenlik" action={<Link to="/guvenlik" className="text-sm text-soluk hover:text-amber flex items-center">Ayrıntılar <ChevronRight size={14} /></Link>}>
          <div className="flex justify-center py-1"><ScoreGauge score={sec.score} /></div>
          <div className="grid grid-cols-3 text-center text-sm border-t border-cizgi pt-3 mt-1">
            <div><div className="font-semibold text-lg">{sec.sshFail24h}</div><div className="text-soluk text-xs">SSH denemesi / 24 sa</div></div>
            <div><div className="font-semibold text-lg">{sec.banned}</div><div className="text-soluk text-xs">engelli IP</div></div>
            <div><div className={`font-semibold text-lg ${sec.ufw === 'active' ? 'text-yesil' : 'text-kirmizi'}`}>{sec.ufw === 'active' ? 'Açık' : 'Kapalı'}</div><div className="text-soluk text-xs">firewall</div></div>
          </div>
        </Panel>

        <div className="grid gap-4 grid-cols-2">
          <Vital label="İşlemci" value={pct(s.cpu)} sub={`${s.cores} çekirdek · yük ${s.load1.toFixed(2)}`} bar={s.cpu}
            color={levelColor(s.cpu)} spark={hist.map((h) => h.cpu)} sparkMax={100} />
          <Vital label="Bellek" value={pct(s.memPct)} sub={`${bytes(s.memUsed)} / ${bytes(s.memTotal)} · swap ${pct(s.swapPct)}`} bar={s.memPct}
            color={levelColor(s.memPct, 75, 90)} spark={hist.map((h) => h.memPct)} sparkMax={100} />
          <Vital label="Disk (/)" value={pct(s.rootDisk)} sub={(s.disks ?? []).filter((d) => d.mount === '/').map((d) => `${bytes(d.used)} / ${bytes(d.total)}`)[0] ?? ''}
            bar={s.rootDisk} color={levelColor(s.rootDisk, 75, 90)} spark={hist.map((h) => h.rootDisk)} sparkMax={100} />
          <Vital label="Ağ" value={rate(s.rx + s.tx)} sub={`↓ ${rate(s.rx)} · ↑ ${rate(s.tx)}`} color="var(--color-mavi)"
            spark={hist.map((h) => h.rx + h.tx)} />
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title="Dikkat gerektirenler">
          {failed.length === 0 && problems.length === 0 && soonCerts.length === 0 ? (
            <div className="flex items-center gap-2 text-yesil py-3"><CheckCircle2 size={18} /> Her şey yolunda görünüyor.</div>
          ) : (
            <ul className="divide-y divide-cizgi">
              {problems.map((p) => (
                <li key={p.kind + p.name} className="py-2.5 flex items-center gap-3">
                  <Dot ok={false} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{p.name} çalışmıyor</div>
                    <div className="text-xs text-soluk">{p.kind} · {p.status}</div>
                  </div>
                  <Link to="/projeler" className="text-sm text-amber hover:underline">Aç</Link>
                </li>
              ))}
              {soonCerts.map((c) => (
                <li key={c.file + c.certFile} className="py-2.5 flex items-center gap-3">
                  <Dot ok={false} warn />
                  <div className="flex-1 min-w-0 truncate">SSL sertifikası {daysLeft(c.certExpiry)} gün içinde bitiyor: {(c.names ?? []).join(', ')}</div>
                </li>
              ))}
              {failed.map((c) => (
                <li key={c.id} className="py-2.5 flex items-start gap-3">
                  <AlertTriangle size={16} className={`mt-0.5 shrink-0 ${c.level === 'critical' ? 'text-kirmizi' : c.level === 'warning' ? 'text-amber' : 'text-mavi'}`} />
                  <div className="min-w-0">
                    <div>{c.title}: <span className="text-soluk">{c.detail}</span></div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Panel>

        <Panel title="Projeler" action={<Link to="/projeler" className="text-sm text-soluk hover:text-amber flex items-center">Tümü <ChevronRight size={14} /></Link>}>
          <div className="grid grid-cols-3 gap-3">
            {(['pm2', 'docker', 'systemd'] as const).map((k) => {
              const c = data.counts[k]
              const all = c.total > 0 && c.healthy === c.total
              return (
                <div key={k} className="bg-gece/60 border border-cizgi rounded-md p-3">
                  <div className="text-sm text-soluk">{{ pm2: 'PM2', docker: 'Docker', systemd: 'Servisler' }[k]}</div>
                  <div className="text-2xl font-semibold mt-0.5">
                    <span className={all ? 'text-yesil' : c.total ? 'text-amber' : ''}>{c.healthy}</span>
                    <span className="text-soluk text-lg">/{c.total}</span>
                  </div>
                  <div className="text-xs text-soluk">çalışıyor</div>
                </div>
              )
            })}
          </div>
          <div className="mt-4">
            <div className="text-sm text-soluk mb-1.5">Son bildirimler</div>
            {data.alerts.length === 0 ? <div className="text-sm text-soluk">Henüz bildirim yok.</div> : (
              <ul className="space-y-1.5">
                {data.alerts.slice(0, 5).map((a) => (
                  <li key={a.id} className="flex items-center gap-2 text-sm">
                    <Tag tone={a.level === 'critical' ? 'kirmizi' : a.level === 'warning' ? 'amber' : a.level === 'ok' ? 'yesil' : 'mavi'}>{a.kind}</Tag>
                    <span className="truncate flex-1">{plainText(a.message)}</span>
                    <span className="text-xs text-soluk whitespace-nowrap">{ago(a.ts)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Panel>
      </div>
    </div>
  )
}

// Bildirim metinlerindeki Telegram HTML etiketlerini temizle; React metni zaten kaçışlar.
export function plainText(s: string) {
  return s.replace(/<[^>]*>/g, '').replace(/\n/g, ' ')
}
