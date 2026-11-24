import { useState } from 'react'
import type { MetricPoint, Snapshot } from '../lib/api'
import { usePoll, useStream } from '../lib/hooks'
import { bytes, duration, pct, rate, levelColor } from '../lib/format'
import { Panel, PageHeader, Loading, Tabs, Bar, Empty } from '../components/ui'
import { AreaChart } from '../components/charts'

type Range = 'canli' | '6h' | '24h' | '7d'

export function System() {
  const [range, setRange] = useState<Range>('canli')
  const [live, setLive] = useState<Snapshot | null>(null)
  const ring = usePoll<{ latest: Snapshot; ring: Snapshot[] }>('/api/system')
  const [liveRing, setLiveRing] = useState<Snapshot[]>([])
  const hist = usePoll<MetricPoint[]>(`/api/system/history?range=${range === 'canli' ? '1h' : range}`, range === 'canli' ? 0 : 60000)

  useStream<Snapshot>('/api/system/stream', (s) => {
    setLive(s)
    setLiveRing((r) => [...r, s].slice(-720))
  })

  const s = live ?? ring.data?.latest
  if (!s) return <Loading />

  const base = ring.data?.ring ?? []
  const merged = [...base, ...liveRing.filter((x) => x.ts > (base[base.length - 1]?.ts ?? 0))].slice(-720)
  const pts: MetricPoint[] =
    range === 'canli'
      ? merged.map((x) => ({ ts: x.ts, cpu: x.cpu, mem: x.memPct, swap: x.swapPct, disk: x.rootDisk, load1: x.load1, rx: x.rx, tx: x.tx }))
      : hist.data ?? []
  const times = pts.map((p) => p.ts)

  return (
    <div className="space-y-5">
      <PageHeader
        title="Sistem"
        desc={`${s.cpuModel || 'İşlemci'} · ${s.cores} çekirdek · ${s.kernel} · açık kalma ${duration(s.uptime)}`}
        action={<Tabs value={range} onChange={setRange} items={[
          { id: 'canli', label: 'Son 1 saat' }, { id: '6h', label: '6 saat' }, { id: '24h', label: '24 saat' }, { id: '7d', label: '7 gün' },
        ]} />}
      />

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title={<span>İşlemci <span className="text-soluk font-normal">{pct(s.cpu, 1)} · yük {s.load1.toFixed(2)} / {s.load5.toFixed(2)} / {s.load15.toFixed(2)}</span></span>}>
          <AreaChart times={times} max={100} format={(v) => pct(v, 1)}
            series={[{ key: 'cpu', label: 'CPU', color: 'var(--color-amber)', values: pts.map((p) => p.cpu) }]} />
        </Panel>
        <Panel title={<span>Bellek <span className="text-soluk font-normal">{bytes(s.memUsed)} / {bytes(s.memTotal)} · swap {bytes(s.swapUsed)}</span></span>}>
          <AreaChart times={times} max={100} format={(v) => pct(v, 1)}
            series={[
              { key: 'mem', label: 'RAM', color: 'var(--color-yesil)', values: pts.map((p) => p.mem) },
              { key: 'swap', label: 'Swap', color: 'var(--color-mavi)', values: pts.map((p) => p.swap) },
            ]} />
        </Panel>
        <Panel title={<span>Ağ <span className="text-soluk font-normal">↓ {rate(s.rx)} · ↑ {rate(s.tx)} · toplam ↓ {bytes(s.rxTotal)} ↑ {bytes(s.txTotal)}</span></span>}>
          <AreaChart times={times} format={rate}
            series={[
              { key: 'rx', label: 'Gelen', color: 'var(--color-mavi)', values: pts.map((p) => p.rx) },
              { key: 'tx', label: 'Giden', color: 'var(--color-amber)', values: pts.map((p) => p.tx) },
            ]} />
        </Panel>
        <Panel title="Diskler ve sıcaklık">
          <ul className="space-y-3">
            {(s.disks ?? []).map((d) => (
              <li key={d.mount}>
                <div className="flex justify-between text-sm mb-1">
                  <span>{d.mount} <span className="text-soluk">{d.device} · {d.fstype}</span></span>
                  <span style={{ color: levelColor(d.percent, 75, 90) }}>{pct(d.percent)} <span className="text-soluk">({bytes(d.used)} / {bytes(d.total)})</span></span>
                </div>
                <Bar value={d.percent} color={levelColor(d.percent, 75, 90)} />
              </li>
            ))}
          </ul>
          <div className="mt-4 pt-3 border-t border-cizgi">
            {(s.temps ?? []).length === 0 ? (
              <div className="text-sm text-soluk">Sıcaklık sensörü yok (sanal sunucularda normaldir).</div>
            ) : (
              <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
                {s.temps!.map((t, i) => (
                  <span key={i}>{t.name}: <b style={{ color: levelColor(t.c, 70, 85) }} className="font-medium">{t.c.toFixed(0)}°C</b></span>
                ))}
              </div>
            )}
          </div>
        </Panel>
      </div>

      <Panel title={`En çok kaynak kullanan süreçler (${s.procCount} süreç)`} pad={false}>
        {(s.procs ?? []).length === 0 ? <Empty>Veri toplanıyor…</Empty> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-soluk text-left">
                <tr className="border-b border-cizgi">
                  <th className="font-normal px-4 py-2">Süreç</th>
                  <th className="font-normal px-2 py-2">PID</th>
                  <th className="font-normal px-2 py-2">Kullanıcı</th>
                  <th className="font-normal px-2 py-2 text-right">CPU</th>
                  <th className="font-normal px-2 py-2 text-right">RAM</th>
                  <th className="font-normal px-4 py-2">Komut</th>
                </tr>
              </thead>
              <tbody>
                {s.procs!.map((p) => (
                  <tr key={p.pid} className="border-b border-cizgi/50 last:border-0 hover:bg-panel2/50">
                    <td className="px-4 py-1.5 font-medium">{p.name}</td>
                    <td className="px-2 py-1.5 text-soluk">{p.pid}</td>
                    <td className={`px-2 py-1.5 ${p.user === 'root' ? 'text-amber' : 'text-soluk'}`}>{p.user}</td>
                    <td className="px-2 py-1.5 text-right" style={{ color: levelColor(p.cpu / Math.max(1, s.cores), 50, 85) }}>{pct(p.cpu, 1)}</td>
                    <td className="px-2 py-1.5 text-right whitespace-nowrap">{bytes(p.mem)}</td>
                    <td className="px-4 py-1.5 text-soluk font-mono text-xs max-w-[480px] truncate" title={p.cmd}>{p.cmd}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  )
}
