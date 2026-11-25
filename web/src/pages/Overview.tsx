// Genel bakış: shadcn "dashboard-01" düzeni — bölüm kartları, etkileşimli grafik, alt paneller.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, CheckCircle2, ChevronRight, Activity, MemoryStick, HardDrive, Network } from 'lucide-react'
import type { MetricPoint, Overview as OV, Snapshot } from '../lib/api'
import { usePoll, useStream } from '../lib/hooks'
import { bytes, duration, pct, rate, ago, daysLeft } from '../lib/format'
import { Card, CardHeader, CardContent, Loading, ErrorBox, Dot, Tag, Tabs, cn } from '../components/ui'
import { ScoreGauge, AreaChart } from '../components/charts'

type Level = 'iyi' | 'orta' | 'yuksek'
const lvl = (v: number, warn: number, crit: number): Level => (v >= crit ? 'yuksek' : v >= warn ? 'orta' : 'iyi')
const lvlTag: Record<Level, { tone: 'yesil' | 'amber' | 'kirmizi'; text: string }> = {
  iyi: { tone: 'yesil', text: 'Normal' }, orta: { tone: 'amber', text: 'Yüksek' }, yuksek: { tone: 'kirmizi', text: 'Kritik' },
}

// shadcn SectionCards tarzı kart
function SectionCard({ label, value, badge, icon: Icon, line, sub }: {
  label: string; value: string; badge: { tone: 'yesil' | 'amber' | 'kirmizi' | 'mavi'; text: string }; icon: typeof Activity; line: string; sub: string
}) {
  return (
    <div className="@container/card flex flex-col gap-6 rounded-xl border bg-gradient-to-t from-primary/5 to-card py-6 shadow-xs dark:bg-card">
      <div className="grid grid-cols-[1fr_auto] items-start gap-1.5 px-6">
        <div className="text-sm text-muted-foreground">{label}</div>
        <Tag tone={badge.tone}>{badge.text}</Tag>
        <div className="col-span-2 text-2xl font-semibold tabular-nums tracking-tight @[250px]/card:text-3xl">{value}</div>
      </div>
      <div className="flex flex-col items-start gap-1.5 px-6 text-sm">
        <div className="flex items-center gap-2 font-medium line-clamp-1"><span className="truncate">{line}</span> <Icon size={16} className="shrink-0 text-muted-foreground" /></div>
        <div className="truncate text-muted-foreground max-w-full">{sub}</div>
      </div>
    </div>
  )
}

type Range = 'canli' | '24h' | '7d'

function UsageChart({ ring }: { ring: Snapshot[] }) {
  const [range, setRange] = useState<Range>('canli')
  const hist = usePoll<MetricPoint[]>(`/api/system/history?range=${range === 'canli' ? '1h' : range}`, range === 'canli' ? 0 : 60000)
  const pts: MetricPoint[] = range === 'canli'
    ? ring.map((x) => ({ ts: x.ts, cpu: x.cpu, mem: x.memPct, swap: x.swapPct, disk: x.rootDisk, load1: x.load1, rx: x.rx, tx: x.tx }))
    : hist.data ?? []
  return (
    <Card>
      <CardHeader
        title="Kaynak kullanımı"
        description={range === 'canli' ? 'Son dakikalar, 5 saniyede bir güncellenir' : range === '24h' ? 'Son 24 saat' : 'Son 7 gün'}
        action={<Tabs value={range} onChange={setRange} items={[{ id: 'canli', label: 'Canlı' }, { id: '24h', label: '24 saat' }, { id: '7d', label: '7 gün' }]} />}
      />
      <CardContent>
        <AreaChart
          height={240}
          times={pts.map((p) => p.ts)}
          max={100}
          format={(v) => pct(v, 1)}
          series={[
            { key: 'cpu', label: 'CPU', color: 'var(--chart-1)', values: pts.map((p) => p.cpu) },
            { key: 'mem', label: 'RAM', color: 'var(--chart-2)', values: pts.map((p) => p.mem) },
          ]}
        />
        <div className="mt-3 flex items-center gap-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5"><span className="size-2.5 rounded-sm" style={{ background: 'var(--chart-1)' }} /> CPU</span>
          <span className="flex items-center gap-1.5"><span className="size-2.5 rounded-sm" style={{ background: 'var(--chart-2)' }} /> RAM</span>
        </div>
      </CardContent>
    </Card>
  )
}

export function Overview() {
  const { data, error } = usePoll<OV>('/api/overview', 30000)
  const sys = usePoll<{ latest: Snapshot; ring: Snapshot[] }>('/api/system')
  const [live, setLive] = useState<Snapshot | null>(null)
  const [ring, setRing] = useState<Snapshot[]>([])
  useStream<Snapshot>('/api/system/stream', (s) => {
    setLive(s)
    setRing((r) => [...r, s].slice(-720))
  })

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const s = live ?? data.system
  const sec = data.security
  const up = data.uptime
  const failed = sec.failed ?? []
  const problems = data.problems ?? []
  const soonCerts = (data.sites ?? []).filter((x) => x.certExpiry > 0 && daysLeft(x.certExpiry) < 21)
  const base = sys.data?.ring ?? []
  const fullRing = [...base, ...ring.filter((x) => x.ts > (base[base.length - 1]?.ts ?? 0))].slice(-720)
  const root = (s.disks ?? []).find((d) => d.mount === '/')
  const cpuL = lvl(s.cpu, 70, 90), memL = lvl(s.memPct, 75, 90), diskL = lvl(s.rootDisk, 75, 90)

  return (
    <div className="flex flex-col gap-4 md:gap-6">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <SectionCard label="İşlemci" value={pct(s.cpu)} badge={lvlTag[cpuL]} icon={Activity}
          line={`Yük ${s.load1.toFixed(2)} · ${s.load5.toFixed(2)} · ${s.load15.toFixed(2)}`} sub={`${s.cores} çekirdek · ${s.procCount} süreç`} />
        <SectionCard label="Bellek" value={pct(s.memPct)} badge={lvlTag[memL]} icon={MemoryStick}
          line={`${bytes(s.memUsed)} / ${bytes(s.memTotal)}`} sub={`Swap ${pct(s.swapPct)} · ${bytes(s.swapUsed)} kullanılıyor`} />
        <SectionCard label="Disk (/)" value={pct(s.rootDisk)} badge={lvlTag[diskL]} icon={HardDrive}
          line={root ? `${bytes(root.total - root.used)} boş` : '—'} sub={root ? `${bytes(root.used)} / ${bytes(root.total)} dolu` : ''} />
        <SectionCard label="Ağ" value={rate(s.rx + s.tx)} badge={{ tone: 'mavi', text: 'Canlı' }} icon={Network}
          line={`↓ ${rate(s.rx)}  ↑ ${rate(s.tx)}`} sub={`Açık kalma süresi ${duration(s.uptime)}`} />
      </div>

      <UsageChart ring={fullRing} />

      <div className="grid gap-4 md:gap-6 lg:grid-cols-3">
        <Card>
          <CardHeader title="Güvenlik skoru" description="Sunucunun sertleştirme durumu"
            action={<Link to="/guvenlik" className="flex items-center text-sm text-muted-foreground hover:text-foreground">Ayrıntılar <ChevronRight size={14} /></Link>} />
          <CardContent>
            <div className="flex justify-center py-1"><ScoreGauge score={sec.score} size={190} /></div>
            <div className="mt-4 grid grid-cols-3 divide-x rounded-lg border text-center text-sm">
              <div className="py-2.5"><div className="text-lg font-semibold tabular-nums">{sec.sshFail24h}</div><div className="text-xs text-muted-foreground">SSH denemesi</div></div>
              <div className="py-2.5"><div className="text-lg font-semibold tabular-nums">{sec.banned}</div><div className="text-xs text-muted-foreground">engelli IP</div></div>
              <div className="py-2.5"><div className={cn('text-lg font-semibold', sec.ufw === 'active' ? 'text-success' : 'text-destructive')}>{sec.ufw === 'active' ? 'Açık' : 'Kapalı'}</div><div className="text-xs text-muted-foreground">firewall</div></div>
            </div>
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader title="Dikkat gerektirenler" description={failed.length + problems.length + soonCerts.length + (up?.down ?? 0) ? 'Çözülmesi önerilen konular' : undefined} />
          <CardContent>
            {failed.length === 0 && problems.length === 0 && soonCerts.length === 0 && !up?.down ? (
              <div className="flex items-center gap-2 py-3 text-success"><CheckCircle2 size={18} /> Her şey yolunda görünüyor.</div>
            ) : (
              <ul className="divide-y rounded-lg border">
                {up && up.down > 0 && (
                  <li className="flex items-center gap-3 px-4 py-2.5">
                    <Dot ok={false} />
                    <div className="min-w-0 flex-1 truncate text-sm font-medium">{up.down} site erişilemiyor</div>
                    <Link to="/uptime" className="text-sm text-muted-foreground hover:text-foreground">Aç</Link>
                  </li>
                )}
                {problems.map((p) => (
                  <li key={p.kind + p.name} className="flex items-center gap-3 px-4 py-2.5">
                    <Dot ok={false} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{p.name} çalışmıyor</div>
                      <div className="text-xs text-muted-foreground">{p.kind} · {p.status}</div>
                    </div>
                    <Link to="/projeler" className="text-sm text-muted-foreground hover:text-foreground">Aç</Link>
                  </li>
                ))}
                {soonCerts.map((c) => (
                  <li key={c.file + c.certFile} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                    <Dot ok={false} warn />
                    <span className="min-w-0 flex-1 truncate">SSL sertifikası {daysLeft(c.certExpiry)} gün içinde bitiyor: {(c.names ?? []).join(', ')}</span>
                  </li>
                ))}
                {failed.map((c) => (
                  <li key={c.id} className="flex items-start gap-3 px-4 py-2.5 text-sm">
                    <AlertTriangle size={16} className={cn('mt-0.5 shrink-0', c.level === 'critical' ? 'text-destructive' : c.level === 'warning' ? 'text-warning' : 'text-info')} />
                    <div className="min-w-0"><span className="font-medium">{c.title}</span><span className="text-muted-foreground">: {c.detail}</span></div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:gap-6 lg:grid-cols-3">
        <Card>
          <CardHeader title="Projeler" action={<Link to="/projeler" className="flex items-center text-sm text-muted-foreground hover:text-foreground">Tümü <ChevronRight size={14} /></Link>} />
          <CardContent className="grid gap-3">
            {(['pm2', 'docker', 'systemd'] as const).map((k) => {
              const c = data.counts[k]
              const all = c.total > 0 && c.healthy === c.total
              return (
                <div key={k} className="flex items-center justify-between rounded-lg border px-4 py-3">
                  <div>
                    <div className="text-sm font-medium">{{ pm2: 'PM2 süreçleri', docker: 'Docker', systemd: 'Servisler' }[k]}</div>
                    <div className="text-xs text-muted-foreground">{c.total === 0 ? 'yok' : all ? 'hepsi çalışıyor' : `${c.total - c.healthy} tanesi durmuş`}</div>
                  </div>
                  <div className="text-xl font-semibold tabular-nums">
                    <span className={all ? '' : c.total ? 'text-warning' : 'text-muted-foreground'}>{c.healthy}</span>
                    <span className="text-base text-muted-foreground">/{c.total}</span>
                  </div>
                </div>
              )
            })}
            {up && up.total > 0 && (
              <Link to="/uptime" className="flex items-center justify-between rounded-lg border px-4 py-3 hover:bg-muted/50">
                <div>
                  <div className="text-sm font-medium">İzlenen siteler</div>
                  <div className="text-xs text-muted-foreground">{up.down ? `${up.down} tanesi erişilemiyor` : 'hepsi erişilebilir'}</div>
                </div>
                <div className="text-xl font-semibold tabular-nums">
                  <span className={up.down ? 'text-destructive' : ''}>{up.up}</span>
                  <span className="text-base text-muted-foreground">/{up.total}</span>
                </div>
              </Link>
            )}
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader title="Son bildirimler" action={<Link to="/bildirimler" className="flex items-center text-sm text-muted-foreground hover:text-foreground">Tümü <ChevronRight size={14} /></Link>} />
          <CardContent>
            {data.alerts.length === 0 ? <div className="py-3 text-sm text-muted-foreground">Henüz bildirim yok.</div> : (
              <ul className="divide-y rounded-lg border">
                {data.alerts.slice(0, 6).map((a) => (
                  <li key={a.id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                    <Tag tone={a.level === 'critical' ? 'kirmizi' : a.level === 'warning' ? 'amber' : a.level === 'ok' ? 'yesil' : 'mavi'}>{a.kind}</Tag>
                    <span className="min-w-0 flex-1 truncate">{plainText(a.message)}</span>
                    <span className="whitespace-nowrap text-xs text-muted-foreground">{ago(a.ts)}</span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
      <p className="text-xs text-muted-foreground">{s.hostname} · {s.os} · çekirdek {s.kernel}</p>
    </div>
  )
}

// Bildirim metinlerindeki Telegram HTML etiketlerini temizle; React metni zaten kaçışlar.
export function plainText(s: string) {
  return s.replace(/<[^>]*>/g, '').replace(/\n/g, ' ')
}
