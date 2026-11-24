// API yardımcıları. Değiştiren isteklerde X-Panel başlığı CSRF koruması için zorunlu.

export class ApiError extends Error {
  status: number
  constructor(status: number, msg: string) {
    super(msg)
    this.status = status
  }
}

let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Panel': '1' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401 && !url.endsWith('/login')) onUnauthorized()
    throw new ApiError(res.status, data.error || `HTTP ${res.status}`)
  }
  return data as T
}

export const api = {
  get: <T>(url: string) => req<T>('GET', url),
  post: <T>(url: string, body?: unknown) => req<T>('POST', url, body ?? {}),
}

// ---- tipler ----

export interface Disk { mount: string; device: string; fstype: string; total: number; used: number; percent: number }
export interface Proc { pid: number; name: string; user: string; cpu: number; mem: number; memPct: number; cmd: string }
export interface Snapshot {
  ts: number; cpu: number; cores: number; cpuModel: string
  memTotal: number; memUsed: number; memPct: number
  swapTotal: number; swapUsed: number; swapPct: number
  load1: number; load5: number; load15: number; uptime: number
  hostname: string; os: string; kernel: string
  rx: number; tx: number; rxTotal: number; txTotal: number
  disks: Disk[] | null; rootDisk: number
  temps: { name: string; c: number }[] | null
  procs: Proc[] | null; procCount: number
}
export interface MetricPoint { ts: number; cpu: number; mem: number; swap: number; disk: number; load1: number; rx: number; tx: number }

export type Kind = 'pm2' | 'docker' | 'systemd'
export interface Item {
  kind: Kind; name: string; id: string; status: string; healthy: boolean
  cpu: number; mem: number; uptime: number; restarts: number; user: string; info: string; ports: string
}
export interface ServicesRes { pm2: Item[]; docker: Item[]; systemd: Item[]; errors: Record<string, string> }

export interface Site {
  file: string; names: string[] | null; listen: string[] | null; proxyPass: string[] | null; root: string
  ssl: boolean; certFile: string; certExpiry: number; certIssuer: string; redirect: boolean
}

export interface Check { id: string; title: string; ok: boolean; level: 'critical' | 'warning' | 'info'; detail: string; fix: string; weight: number }
export interface Port { proto: string; addr: string; port: number; process: string; public: boolean }
export interface SSHEvent { ts: string; type: string; user: string; ip: string; method: string }
export interface SecReport {
  ts: number; score: number; checks: Check[]; ports: Port[] | null
  sshAccepted: SSHEvent[]; sshFailTop: { ip: string; count: number; users: string }[]; sshFail24h: number
  sessions: { user: string; tty: string; from: string; when: string }[]
  banned: string[] | null; fail2ban: boolean; ufw: string; ufwRules: string
  updates: number; secUpdates: number; reboot: boolean
}

export interface Alert { id: number; ts: number; level: string; kind: string; message: string; sent: boolean }
export interface AuditEntry { id: number; ts: number; username: string; ip: string; action: string; target: string; ok: boolean; detail: string }

export interface Overview {
  system: Snapshot
  counts: Record<Kind, { total: number; healthy: number }>
  problems: Item[] | null
  security: { score: number; failed: Check[] | null; sshFail24h: number; banned: number; ufw: string }
  alerts: Alert[]
  sites: Site[] | null
}

export interface UpdateStatus {
  current: string; repo: string; enabled: boolean; available: boolean; checkedAt: number; error?: string
  arch: string; canApply: boolean
  latest: { tag: string; version: string; name: string; notes: string; url: string; publishedAt: string } | null
}

export interface Me {
  username: string; hostname: string; ip: string; telegram: boolean
  version: string; updateAvailable: boolean; latestVersion: string
}
