import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PortScans } from './PortScans'
import { WebAttacks } from './WebAttacks'
import { countryName, flag } from '../lib/geo'
import { RefreshCw, Globe2, LoaderCircle } from 'lucide-react'
import { type AttackReport } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago } from '../lib/format'
import {
  Alert, Button, Card, CardContent, CardHeader, Empty, ErrorBox, Loading, PageHeader, Table, Tabs, Tag, Td, Th, THead, Tr,
} from '../components/ui'

type MapData = { viewBox: string; locations: { id: string; d: string }[] }

const nf = new Intl.NumberFormat('tr-TR')

// Renk: log ölçeği, 5 kademe. Saldırı yoğunluğunu kırmızının tonlarıyla gösterir.
function level(count: number, max: number) {
  if (!count) return 0
  return Math.max(1, Math.ceil((Math.log(count + 1) / Math.log(max + 1)) * 5))
}
const fills = ['var(--muted)', 'color-mix(in oklch, var(--destructive) 18%, var(--muted))', 'color-mix(in oklch, var(--destructive) 36%, var(--muted))',
  'color-mix(in oklch, var(--destructive) 56%, var(--muted))', 'color-mix(in oklch, var(--destructive) 78%, var(--muted))', 'var(--destructive)']

function WorldMap({ counts, max }: { counts: Map<string, { count: number; ips: number }>; max: number }) {
  const [map, setMap] = useState<MapData | null>(null)
  const [hover, setHover] = useState<{ cc: string; x: number; y: number } | null>(null)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let alive = true
    import('../lib/world-map.json').then((m) => { if (alive) setMap(m.default as MapData) })
    return () => { alive = false }
  }, [])

  if (!map) return <div className="flex aspect-[1010/560] items-center justify-center text-muted-foreground"><LoaderCircle size={16} className="animate-spin" /></div>

  const h = hover ? counts.get(hover.cc.toUpperCase()) : undefined
  return (
    <div ref={box} className="relative" onMouseLeave={() => setHover(null)}>
      {/* Antarktika'yı göstermemek için alt kısım kırpılır */}
      <svg viewBox="0 30 1010 560" className="block h-auto w-full" role="img" aria-label="Saldırıların ülkelere göre dağılımı">
        {map.locations.map((l) => {
          if (l.id === 'aq') return null
          const c = counts.get(l.id.toUpperCase())
          const lv = level(c?.count ?? 0, max)
          return (
            <path key={l.id} d={l.d} fill={fills[lv]}
              className="stroke-background"
              strokeWidth={0.4} fillOpacity={hover && hover.cc === l.id ? 0.75 : 1}
              onMouseMove={(e) => {
                const r = box.current!.getBoundingClientRect()
                setHover({ cc: l.id, x: e.clientX - r.left, y: e.clientY - r.top })
              }} />
          )
        })}
      </svg>
      {hover && (
        <div className="pointer-events-none absolute z-10 rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-md"
          style={{ left: Math.min(hover.x + 12, (box.current?.clientWidth ?? 0) - 170), top: hover.y + 12 }}>
          <div className="font-medium">{flag(hover.cc)} {countryName(hover.cc.toUpperCase())}</div>
          <div className="text-muted-foreground">{h ? `${nf.format(h.count)} deneme · ${nf.format(h.ips)} IP` : 'deneme yok'}</div>
        </div>
      )}
      <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
        az
        <div className="flex">{fills.slice(1).map((f) => <span key={f} className="h-2 w-6 first:rounded-l-sm last:rounded-r-sm" style={{ background: f }} />)}</div>
        çok
      </div>
    </div>
  )
}

function Timeline({ values, range }: { values: number[]; range: '24h' | '7d' }) {
  const max = Math.max(1, ...values)
  const now = new Date()
  return (
    <div>
      <div className="flex h-28 items-end gap-[3px]">
        {values.map((v, i) => {
          const back = values.length - 1 - i
          const t = range === '24h' ? new Date(now.getTime() - back * 3600_000) : new Date(now.getTime() - back * 86400_000)
          const label = range === '24h'
            ? `${String(t.getHours()).padStart(2, '0')}:00`
            : t.toLocaleDateString('tr-TR', { day: 'numeric', month: 'short' })
          return (
            <div key={i} className="group relative flex h-full flex-1 items-end" title={`${label} · ${nf.format(v)} deneme`}>
              <div className="w-full rounded-t-[3px] bg-destructive/70 transition-colors group-hover:bg-destructive" style={{ height: `${Math.max(v ? 3 : 0, (v / max) * 100)}%` }} />
            </div>
          )
        })}
      </div>
      <div className="mt-1.5 flex justify-between text-xs text-muted-foreground">
        <span>{range === '24h' ? '24 saat önce' : '7 gün önce'}</span>
        <span>şimdi</span>
      </div>
    </div>
  )
}

// Saldırılar sayfası: SSH denemeleri ve port taramaları iki sekmede.
export function Attacks() {
  const [params, setParams] = useSearchParams()
  const s = params.get('sekme')
  const tab = s === 'tarama' || s === 'web' ? s : 'ssh'
  const switcher = (
    <Tabs value={tab} onChange={(v) => setParams(v === 'ssh' ? {} : { sekme: v })}
      items={[{ id: 'ssh', label: 'SSH' }, { id: 'web', label: 'Web' }, { id: 'tarama', label: 'Port taramaları' }]} />
  )
  if (tab === 'tarama') return <PortScans switcher={switcher} />
  if (tab === 'web') return <WebAttacks switcher={switcher} />
  return <SSHAttacks switcher={switcher} />
}

function SSHAttacks({ switcher }: { switcher: ReactNode }) {
  const [range, setRange] = useState<'24h' | '7d'>('24h')
  const [fresh, setFresh] = useState(0)
  const [allIps, setAllIps] = useState(false)
  const { data, error, loading, reload } = usePoll<AttackReport>(`/api/security/attacks?range=${range}${fresh ? `&fresh=1&n=${fresh}` : ''}`, 60000)

  const counts = useMemo(() => new Map((data?.countries ?? []).map((c) => [c.cc, c])), [data])
  const max = data?.countries.reduce((m, c) => (c.cc === '??' ? m : Math.max(m, c.count)), 0) ?? 0

  const header = (
    <PageHeader
      title="Saldırılar"
      desc="SSH'a yapılan başarısız giriş denemelerinin hangi ülkelerden geldiği. Ülke bilgisi sunucudaki yerel veritabanından bulunur; IP adresleri hiçbir dış servise gönderilmez."
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

  const known = data.countries.filter((c) => c.cc !== '??')
  const top = known[0]

  return (
    <div className="space-y-5">
      {header}

      {!data.geo.ready && (
        <Alert tone={data.geo.error ? 'amber' : 'mavi'} title={data.geo.loading ? 'Ülke veritabanı indiriliyor' : 'Ülke veritabanı hazır değil'}>
          {data.geo.error
            ? <>İndirilemedi: {data.geo.error}. Kokpit günde bir tekrar dener.</>
            : <>İlk açılışta yaklaşık 10 MB'lık ücretsiz DB-IP veritabanı indirilir; bu bir iki dakika sürebilir. O zamana kadar ülkeler “Bilinmiyor” görünür.</>}
        </Alert>
      )}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[
          ['Başarısız deneme', nf.format(data.total)],
          ['Farklı IP', nf.format(data.uniqueIps)],
          ['Ülke', nf.format(known.length)],
          ['En çok deneyen', top ? `${flag(top.cc)} ${countryName(top.cc)}` : '—'],
        ].map(([l, v]) => (
          <Card key={l}><CardContent className="py-4">
            <div className="text-sm text-muted-foreground">{l}</div>
            <div className="mt-1 truncate text-2xl font-semibold tabular-nums">{v}</div>
          </CardContent></Card>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader title="Dünya haritası" description={`Başarılı girişler: ${data.accepted} · veriler ${ago(data.generated)} hesaplandı`} />
          <CardContent>
            {data.total === 0 ? <Empty>Bu süre içinde başarısız SSH denemesi yok.</Empty> : <WorldMap counts={counts} max={max} />}
          </CardContent>
        </Card>
        <Card>
          <CardHeader title="Ülkeler" description="Deneme sayısına göre" />
          <CardContent className="px-0 pb-2">
            {data.countries.length === 0 ? <Empty>Veri yok.</Empty> : (
              <ul className="max-h-[420px] overflow-y-auto">
                {data.countries.slice(0, 40).map((c) => (
                  <li key={c.cc} className="flex items-center gap-3 px-6 py-1.5 text-sm">
                    <span className="w-5 text-center" aria-hidden>{flag(c.cc)}</span>
                    <span className="min-w-0 flex-1 truncate">{countryName(c.cc)}</span>
                    <span className="text-xs text-muted-foreground tabular-nums">{nf.format(c.ips)} IP</span>
                    <span className="w-14 text-right font-medium tabular-nums">{nf.format(c.count)}</span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader title="Zaman içinde" description={range === '24h' ? 'Saatlik deneme sayısı' : 'Günlük deneme sayısı'} />
        <CardContent><Timeline values={data.hourly} range={range} /></CardContent>
      </Card>

      <Card>
        <CardHeader title="En çok deneyen IP adresleri" description="Engellendi: fail2ban bu IP'yi şu an banlamış durumda" />
        <CardContent className="px-0 pb-0">
          {data.topIps.length === 0 ? <Empty>Veri yok.</Empty> : (
            <Table>
              <THead><Tr><Th>IP</Th><Th>Ülke</Th><Th className="text-right">Deneme</Th><Th>Denenen kullanıcılar</Th><Th>Son deneme</Th><Th>Durum</Th></Tr></THead>
              <tbody>
                {(allIps ? data.topIps : data.topIps.slice(0, 15)).map((ip) => (
                  <Tr key={ip.ip}>
                    <Td className="font-mono text-xs">{ip.ip}</Td>
                    <Td className="whitespace-nowrap">{flag(ip.cc)} {countryName(ip.cc)}</Td>
                    <Td className="text-right tabular-nums">{nf.format(ip.count)}</Td>
                    <Td className="max-w-56 truncate text-muted-foreground" title={ip.users}>{ip.users || '—'}</Td>
                    <Td className="whitespace-nowrap text-muted-foreground">{ip.last ? new Date(ip.last * 1000).toLocaleString('tr-TR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }) : '—'}</Td>
                    <Td>{ip.banned ? <Tag tone="yesil">Engellendi</Tag> : <Tag>Serbest</Tag>}</Td>
                  </Tr>
                ))}
              </tbody>
            </Table>
          )}
          {data.topIps.length > 15 && (
            <div className="border-t px-6 py-3">
              <Button size="sm" variant="ghost" onClick={() => setAllIps(!allIps)}>{allIps ? 'Daha az göster' : `Tümünü göster (${data.topIps.length})`}</Button>
            </div>
          )}
        </CardContent>
      </Card>

      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Globe2 size={12} />
        <a href="https://db-ip.com" target="_blank" rel="noopener noreferrer" className="underline-offset-4 hover:underline">IP Geolocation by DB-IP</a>
        (CC BY 4.0) · Harita: svg-maps (CC BY 4.0)
      </p>
    </div>
  )
}
