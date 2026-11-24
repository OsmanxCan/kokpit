export function bytes(n: number, digits = 1): string {
  if (!n || n < 0) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : digits).replace('.', ',')} ${u[i]}`
}

export const rate = (n: number) => `${bytes(n)}/sn`

export const pct = (n: number, d = 0) => `%${(n || 0).toFixed(d).replace('.', ',')}`

export function duration(sec: number): string {
  sec = Math.max(0, Math.floor(sec))
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d} gün ${h} sa`
  if (h > 0) return `${h} sa ${m} dk`
  if (m > 0) return `${m} dk`
  return `${sec} sn`
}

export function ago(unix: number): string {
  if (!unix) return '—'
  const s = Date.now() / 1000 - unix
  if (s < 60) return 'az önce'
  return `${duration(s).split(' ').slice(0, 2).join(' ')} önce`
}

export function dateTime(unix: number): string {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString('tr-TR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })
}

export function daysLeft(unix: number): number {
  return Math.floor((unix * 1000 - Date.now()) / 86400000)
}

// Durum rengi: yeşil < sarı eşiği < amber < kırmızı eşiği
export function levelColor(v: number, warn = 70, crit = 90): string {
  if (v >= crit) return 'var(--destructive)'
  if (v >= warn) return 'var(--warning)'
  return 'var(--success)'
}
