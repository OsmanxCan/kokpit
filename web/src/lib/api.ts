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
  put: <T>(url: string, body?: unknown) => req<T>('PUT', url, body ?? {}),
  del: <T>(url: string, body?: unknown) => req<T>('DELETE', url, body ?? {}),
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
  uptime?: { total: number; up: number; down: number }
}

export interface UpdateStatus {
  current: string; repo: string; enabled: boolean; available: boolean; checkedAt: number; error?: string
  arch: string; canApply: boolean
  latest: { tag: string; version: string; name: string; notes: string; url: string; publishedAt: string } | null
}

export interface Me {
  username: string; hostname: string; ip: string; telegram: boolean
  version: string; updateAvailable: boolean; latestVersion: string; repo: string
}

// ---- ayarlar ----

export interface EventSetting { enabled: boolean; telegram: boolean; cooldown: number; template: string }
export interface Thresholds { cpu: number; cpuMinutes: number; ram: number; ramMinutes: number; disk: number; sshBrute: number }
export interface SettingsT {
  telegram: { token: string; chatId: string; prefix: boolean; commands: boolean }
  thresholds: Thresholds
  events: Record<string, EventSetting>
  watchServices: string[]
  sessionIpBind: boolean
  updateCheck: boolean
  fileRoots: FileRoot[]
  terminalOff: boolean
  tokenSet: boolean
  tokenMasked: string
}
export interface VarDef { name: string; desc: string; sample: string }
export interface EventDef {
  id: string; group: string; name: string; desc: string; level: string
  defaultOn: boolean; defaultCooldown: number; defaultTemplate: string; vars: VarDef[]
}
export interface UserT { username: string; createdAt: number; updatedAt: number }

// ---- firewall ----

export interface FwRule { num: number; to: string; action: string; from: string; comment: string; v6: boolean; ssh: boolean }
export interface FwStatus {
  installed: boolean; active: boolean; incoming: string; outgoing: string; routed: string
  rules: FwRule[] | null; sshPort: number; sshAllowed: boolean; rollback: number; raw: string
}

// ---- uptime ----

export interface Monitor {
  id: string; name: string; url: string; interval: number; timeout: number; expect: number; keyword: string
  enabled: boolean; createdAt: number
}
export interface UpBar { ts: number; uptime: number; avgMs: number }
export interface UpSummary extends Monitor {
  status: 'up' | 'down' | 'pending' | 'paused'; since: number
  uptime24: number; uptime7: number; uptime30: number; avgMs: number; lastMs: number
  lastError: string; lastCheck: number; certExp: number; certIssuer: string; bars: UpBar[]
}
export interface UpCheck { ts: number; ok: boolean; ms: number; status: number; error?: string }
export interface UpDetail extends UpSummary {
  checks: UpCheck[]; hours: { ts: number; total: number; up: number; sumMs: number }[]
  incidents: { start: number; end: number; reason: string }[]
}

// ---- saldırı haritası ----

export interface GeoStatus { ready: boolean; loading: boolean; updated: number; ranges: number; source: string; error?: string }
export interface AttackReport {
  range: '24h' | '7d'; generated: number; total: number; uniqueIps: number; accepted: number
  countries: { cc: string; count: number; ips: number }[]
  topIps: { ip: string; cc: string; count: number; users: string; last: number; banned: boolean }[]
  hourly: number[]
  geo: GeoStatus
}

// ---- dosyalar ----

export interface FileRoot { name: string; path: string; write: boolean; delete: boolean }
export interface FileEntry { name: string; dir: boolean; size: number; mtime: number; mode: string; link: boolean; outside?: boolean; uid: number }
export interface FileListing { path: string; entries: FileEntry[]; write: boolean; delete: boolean; free: number; total: number }
export interface TextFileT { path: string; content: string; mtime: string; size: number; binary: boolean; tooBig: boolean }

// ---- deploy ----

export interface DeployStep { name: string; cmd: string }
export interface DeployProject {
  id: string; name: string; dir: string; branch: string; steps: DeployStep[]; pm2: string; healthUrl: string
  autoRollback: boolean; telegram: boolean; runAs: string; createdAt: number
}
export interface DeployRun {
  id: string; project: string; status: 'running' | 'ok' | 'failed' | 'rolled_back' | 'canceled'; trigger: string; user: string
  start: number; end: number; from: string; to: string; message: string; target: string; error?: string; step: string
}
export interface DeployItem extends DeployProject { busy: string; last: DeployRun | null; git: { hash?: string; subject?: string; time?: string; remote?: string } }
export interface TelegramStatus { running: boolean; bot: string; error?: string; lastSeen: number; ignored: number }
