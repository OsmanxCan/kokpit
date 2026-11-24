import { useMemo, useRef, useState } from 'react'

// Bağımlılıksız SVG grafikleri.

export function Sparkline({ values, color, max, height = 36 }: { values: number[]; color: string; max?: number; height?: number }) {
  const w = 160
  const path = useMemo(() => {
    if (values.length < 2) return ''
    const m = max ?? Math.max(1, ...values)
    return values
      .map((v, i) => `${i === 0 ? 'M' : 'L'}${((i / (values.length - 1)) * w).toFixed(1)},${(height - (Math.min(v, m) / m) * (height - 2) - 1).toFixed(1)}`)
      .join(' ')
  }, [values, max, height])
  if (!path) return <div style={{ height }} />
  return (
    <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" className="w-full block" style={{ height }} aria-hidden>
      <path d={`${path} L${w},${height} L0,${height} Z`} fill={color} opacity={0.12} />
      <path d={path} fill="none" stroke={color} strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

export interface Series { key: string; label: string; color: string; values: number[] }

export function AreaChart({ times, series, max, format, height = 180 }: {
  times: number[]; series: Series[]; max?: number; format: (v: number) => string; height?: number
}) {
  const ref = useRef<SVGSVGElement>(null)
  const [hover, setHover] = useState<number | null>(null)
  const w = 600
  const pad = { t: 8, b: 20, l: 0, r: 0 }
  const ih = height - pad.t - pad.b
  const m = max ?? Math.max(1, ...series.flatMap((s) => s.values)) * 1.1
  const n = times.length
  const x = (i: number) => (n <= 1 ? 0 : (i / (n - 1)) * w)
  const y = (v: number) => pad.t + ih - (Math.min(v, m) / m) * ih

  if (n < 2) return <div className="text-soluk text-sm flex items-center justify-center" style={{ height }}>Veri toplanıyor…</div>

  const labels = [0, Math.floor(n / 2), n - 1]
  const tfmt = (t: number) => {
    const span = times[n - 1] - times[0]
    const d = new Date(t * 1000)
    return span > 86400 * 1.5
      ? d.toLocaleDateString('tr-TR', { day: '2-digit', month: 'short' })
      : d.toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div className="relative">
      <svg
        ref={ref}
        viewBox={`0 0 ${w} ${height}`}
        preserveAspectRatio="none"
        className="w-full block"
        style={{ height }}
        onMouseMove={(e) => {
          const r = ref.current!.getBoundingClientRect()
          const i = Math.round(((e.clientX - r.left) / r.width) * (n - 1))
          setHover(Math.max(0, Math.min(n - 1, i)))
        }}
        onMouseLeave={() => setHover(null)}
        role="img"
        aria-label={series.map((s) => s.label).join(', ') + ' grafiği'}
      >
        {[0.25, 0.5, 0.75, 1].map((f) => (
          <line key={f} x1={0} x2={w} y1={y(m * f)} y2={y(m * f)} stroke="var(--color-cizgi)" strokeWidth={1} vectorEffect="non-scaling-stroke" strokeDasharray="2 4" />
        ))}
        {series.map((s) => {
          const d = s.values.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ')
          return (
            <g key={s.key}>
              <path d={`${d} L${w},${pad.t + ih} L0,${pad.t + ih} Z`} fill={s.color} opacity={0.1} />
              <path d={d} fill="none" stroke={s.color} strokeWidth={1.6} vectorEffect="non-scaling-stroke" />
            </g>
          )
        })}
        {hover !== null && <line x1={x(hover)} x2={x(hover)} y1={pad.t} y2={pad.t + ih} stroke="var(--color-soluk)" strokeWidth={1} vectorEffect="non-scaling-stroke" />}
      </svg>
      <div className="flex justify-between text-xs text-soluk -mt-4 pointer-events-none">
        {labels.map((i) => <span key={i}>{tfmt(times[i])}</span>)}
      </div>
      <div className="absolute top-0 right-0 text-xs text-soluk">{format(m)}</div>
      {hover !== null && (
        <div
          className="absolute top-2 pointer-events-none bg-gece/95 border border-cizgi rounded px-2 py-1 text-xs whitespace-nowrap"
          style={{ left: `${(hover / (n - 1)) * 100}%`, transform: `translateX(${hover > n / 2 ? '-105%' : '5%'})` }}
        >
          <div className="text-soluk">{new Date(times[hover] * 1000).toLocaleString('tr-TR')}</div>
          {series.map((s) => (
            <div key={s.key} className="flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: s.color }} />
              {s.label}: <b className="font-medium">{format(s.values[hover])}</b>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// Güvenlik skoru göstergesi: 20 segmentli halka. Panelin "imza" öğesi.
export function ScoreGauge({ score, size = 200 }: { score: number; size?: number }) {
  const seg = 20
  const lit = Math.round((score / 100) * seg)
  const color = score >= 85 ? 'var(--color-yesil)' : score >= 60 ? 'var(--color-amber)' : 'var(--color-kirmizi)'
  const word = score >= 85 ? 'Sağlam' : score >= 60 ? 'Geliştirilmeli' : 'Risk altında'
  const r = 80
  const c = 2 * Math.PI * r
  const gap = 3.2
  const segLen = c / seg - gap
  // 270° yay (alt kısım açık)
  const arcFrac = 0.75
  return (
    <div className="relative" style={{ width: size, height: size }}>
      <svg viewBox="0 0 200 200" width={size} height={size} role="img" aria-label={`Güvenlik skoru ${score}/100`}>
        <g transform="rotate(135 100 100)">
          {Array.from({ length: seg }).map((_, i) => {
            const len = segLen * arcFrac
            const off = -i * (c / seg) * arcFrac
            return (
              <circle
                key={i}
                cx={100} cy={100} r={r}
                fill="none"
                stroke={i < lit ? color : 'var(--color-cizgi)'}
                strokeWidth={14}
                strokeDasharray={`${len} ${c - len}`}
                strokeDashoffset={off}
                className={i < lit ? 'gosterge-dol' : ''}
                style={{ animationDelay: `${i * 45}ms` }}
              />
            )
          })}
        </g>
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <div className="text-5xl font-semibold leading-none" style={{ color }}>{score}</div>
        <div className="text-soluk text-sm mt-1">/ 100</div>
        <div className="text-sm font-medium mt-2" style={{ color }}>{word}</div>
      </div>
    </div>
  )
}
