import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Rocket, Plus, Pencil, Trash2, History, EllipsisVertical, GitBranch, FolderGit2, Square, ArrowUp, ArrowDown, X,
  RotateCcw, Send, CheckCircle2, XCircle, Undo2, LoaderCircle, Ban, ScrollText,
} from 'lucide-react'
import { api, type DeployItem, type DeployProject, type DeployRun, type DeployStep } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago, dateTime, duration } from '../lib/format'
import {
  Button, Card, CardContent, CardHeader, cn, ConfirmButton, Dialog, Empty, ErrorBox, Field, Input, Loading, Menu, MenuItem,
  PageHeader, Select, Switch, Tag, toast,
} from '../components/ui'

type ListResp = { projects: DeployItem[]; pm2: string[] | null; runs: DeployRun[] }

const statusInfo: Record<DeployRun['status'], { label: string; tone: 'yesil' | 'kirmizi' | 'amber' | 'mavi' | 'soluk'; icon: typeof CheckCircle2 }> = {
  running: { label: 'Sürüyor', tone: 'mavi', icon: LoaderCircle },
  ok: { label: 'Başarılı', tone: 'yesil', icon: CheckCircle2 },
  failed: { label: 'Başarısız', tone: 'kirmizi', icon: XCircle },
  rolled_back: { label: 'Geri alındı', tone: 'amber', icon: Undo2 },
  canceled: { label: 'Durduruldu', tone: 'soluk', icon: Ban },
}

function RunTag({ r }: { r: DeployRun }) {
  const s = statusInfo[r.status]
  return <Tag tone={s.tone}><s.icon size={12} className={r.status === 'running' ? 'animate-spin' : ''} /> {s.label}</Tag>
}

const short = (h?: string) => (h ? h.slice(0, 7) : '')
const who = (r: DeployRun) => (r.trigger === 'telegram' ? 'Telegram' : r.user || 'panel')

// Hazır adım şablonları
const presets: { label: string; steps: DeployStep[] }[] = [
  { label: 'Node.js', steps: [{ name: 'Bağımlılıklar', cmd: 'npm ci' }, { name: 'Derleme', cmd: 'npm run build' }] },
  { label: 'Node.js (pnpm)', steps: [{ name: 'Bağımlılıklar', cmd: 'pnpm install --frozen-lockfile' }, { name: 'Derleme', cmd: 'pnpm build' }] },
  { label: 'Laravel', steps: [
    { name: 'Composer', cmd: 'composer install --no-dev --optimize-autoloader --no-interaction' },
    { name: 'Veritabanı', cmd: 'php artisan migrate --force' },
    { name: 'Önbellek', cmd: 'php artisan optimize' },
  ] },
  { label: 'Go', steps: [{ name: 'Derleme', cmd: 'go build -o app .' }] },
]

// ---------------------------------------------------------------- canlı log

function LogDialog({ runId, onClose, onDone }: { runId: string | null; onClose: () => void; onDone: () => void }) {
  const [text, setText] = useState('')
  const [run, setRun] = useState<DeployRun | null>(null)
  const pre = useRef<HTMLPreElement>(null)
  const stick = useRef(true)

  useEffect(() => {
    setText(''); setRun(null)
    if (!runId) return
    stick.current = true
    const es = new EventSource(`/api/deploy/runs/${runId}/log`)
    es.onmessage = (e) => setText((t) => t + JSON.parse(e.data))
    es.addEventListener('done', (e) => { setRun(JSON.parse((e as MessageEvent).data)); es.close(); onDone() })
    es.onerror = () => es.close()
    return () => es.close()
  }, [runId]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (stick.current && pre.current) pre.current.scrollTop = pre.current.scrollHeight
  }, [text])

  async function cancel() {
    try { await api.post(`/api/deploy/runs/${runId}/cancel`) } catch (e) { toast.err((e as Error).message) }
  }

  const running = !run
  return (
    <Dialog open={!!runId} onClose={onClose} title="Deploy çıktısı" bare className="max-w-4xl! h-[calc(100dvh-4rem)]">
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <ScrollText size={16} className="text-muted-foreground" />
        <div className="flex-1 text-sm font-medium">Deploy çıktısı</div>
        {run ? <RunTag r={run} /> : <Tag tone="mavi"><LoaderCircle size={12} className="animate-spin" /> Sürüyor</Tag>}
        {running && <ConfirmButton size="sm" variant="outline" onConfirm={cancel} confirmText="Durdurulsun mu?"><Square size={12} /> Durdur</ConfirmButton>}
        <Button size="icon" variant="ghost" onClick={onClose} aria-label="Kapat"><X size={16} /></Button>
      </div>
      <pre ref={pre} onScroll={(e) => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40 }}
        className="min-h-0 flex-1 overflow-auto bg-[#0a0a0a] p-4 font-mono text-[12.5px] leading-relaxed text-neutral-200 whitespace-pre-wrap break-words">
        {text.split('\n').map((ln, i) => (
          <div key={i} className={cn(
            ln.startsWith('▶') && 'text-sky-300 font-medium',
            ln.startsWith('$ ') && 'text-neutral-400',
            ln.startsWith('✓') && 'text-emerald-400 font-medium',
            (ln.startsWith('✗') || ln.startsWith('■')) && 'text-red-400 font-medium',
            ln.startsWith('↩') && 'text-amber-300 font-medium',
          )}>{ln || ' '}</div>
        ))}
        {running && <span className="inline-block h-3.5 w-2 animate-pulse bg-neutral-400 align-middle" />}
      </pre>
    </Dialog>
  )
}

// ---------------------------------------------------------------- geçmiş ve geri alma

type Commit = { hash: string; subject: string; author: string; time: string; current: string }

function HistoryDialog({ project, onClose, onRun, onLog }: { project: DeployItem | null; onClose: () => void; onRun: (target: string) => void; onLog: (id: string) => void }) {
  const [runs, setRuns] = useState<DeployRun[] | null>(null)
  const [commits, setCommits] = useState<Commit[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    setRuns(null); setCommits(null); setErr('')
    if (!project) return
    api.get<DeployRun[]>(`/api/deploy/${project.id}/runs`).then(setRuns).catch((e) => setErr(e.message))
    if (project.branch) api.get<Commit[]>(`/api/deploy/${project.id}/commits`).then(setCommits).catch(() => setCommits([]))
  }, [project])
  if (!project) return null
  return (
    <Dialog open onClose={onClose} wide title={`${project.name} geçmişi`} description="Önceki bir sürüme dönmek, o commit'i çekip adımları yeniden çalıştırır.">
      {err && <ErrorBox>{err}</ErrorBox>}
      <div className="space-y-6">
        <div>
          <div className="mb-2 text-sm font-medium">Deploy'lar</div>
          {!runs ? <Loading /> : runs.length === 0 ? <div className="text-sm text-muted-foreground">Henüz deploy yapılmadı.</div> : (
            <ul className="max-h-64 divide-y overflow-y-auto rounded-lg border">
              {runs.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
                  <RunTag r={r} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{r.to ? <><span className="font-mono text-xs">{short(r.to)}</span> {r.message}</> : (r.error || '—')}</div>
                    <div className="text-xs text-muted-foreground">{dateTime(r.start)} · {who(r)}{r.end ? ` · ${duration(r.end - r.start)}` : ''}{r.target ? ' · geri alma' : ''}</div>
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => onLog(r.id)}>Çıktı</Button>
                </li>
              ))}
            </ul>
          )}
        </div>
        {project.branch && (
          <div>
            <div className="mb-2 text-sm font-medium">Sürümler ({project.branch})</div>
            {!commits ? <Loading /> : commits.length === 0 ? <div className="text-sm text-muted-foreground">Commit bulunamadı.</div> : (
              <ul className="max-h-64 divide-y overflow-y-auto rounded-lg border">
                {commits.map((c) => (
                  <li key={c.hash} className="flex items-center gap-3 px-3 py-2 text-sm">
                    <span className="font-mono text-xs text-muted-foreground">{short(c.hash)}</span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate">{c.subject}</div>
                      <div className="text-xs text-muted-foreground">{c.author} · {ago(Number(c.time))}</div>
                    </div>
                    {c.current === 'true' ? <Tag tone="yesil">Şu anki</Tag> : (
                      <ConfirmButton size="sm" variant="outline" onConfirm={() => onRun(c.hash)} confirmText="Bu sürüme dönülsün mü?" disabled={!!project.busy}>
                        <RotateCcw size={12} /> Bu sürüme dön
                      </ConfirmButton>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}

// ---------------------------------------------------------------- proje formu

const blank: DeployProject = { id: '', name: '', dir: '/var/www/', branch: 'main', steps: [], pm2: '', healthUrl: '', autoRollback: true, telegram: false, runAs: '', createdAt: 0 }

function ProjectForm({ open, initial, pm2, onClose, onSaved }: { open: boolean; initial: DeployProject | null; pm2: string[]; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<DeployProject>(blank)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => { if (open) { setF(initial ? { ...initial, steps: initial.steps.map((s) => ({ ...s })) } : { ...blank, steps: [] }); setErr('') } }, [open, initial])
  const set = <K extends keyof DeployProject>(k: K, v: DeployProject[K]) => setF((x) => ({ ...x, [k]: v }))
  const setStep = (i: number, p: Partial<DeployStep>) => setF((x) => ({ ...x, steps: x.steps.map((s, j) => (j === i ? { ...s, ...p } : s)) }))
  const move = (i: number, d: number) => setF((x) => {
    const st = [...x.steps]; const [a] = st.splice(i, 1); st.splice(i + d, 0, a); return { ...x, steps: st }
  })

  async function save() {
    setBusy(true); setErr('')
    try {
      if (initial?.id) await api.put(`/api/deploy/${initial.id}`, f)
      else await api.post('/api/deploy', f)
      toast.ok(initial?.id ? 'Proje güncellendi' : 'Proje eklendi')
      onSaved(); onClose()
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }

  return (
    <Dialog open={open} onClose={onClose} wide title={initial?.id ? 'Projeyi düzenle' : 'Deploy projesi ekle'}
      description="Deploy sırasıyla şunları yapar: git'ten en yeni sürümü çeker, adımları çalıştırır, PM2 uygulamasını yeniden başlatır ve siteyi kontrol eder."
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="default" onClick={save} loading={busy}>{initial?.id ? 'Kaydet' : 'Projeyi ekle'}</Button></>}>
      <div className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Proje adı"><Input value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="Web sitesi" maxLength={50} autoFocus={!initial} /></Field>
          <Field label="Klasör" hint="Sunucudaki proje klasörü (git deposu)"><Input value={f.dir} onChange={(e) => set('dir', e.target.value)} className="font-mono" /></Field>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Git dalı" hint="Boş bırakırsan git kullanılmaz, sadece adımlar çalışır"><Input value={f.branch} onChange={(e) => set('branch', e.target.value)} placeholder="main" className="font-mono" /></Field>
          <Field label="PM2 uygulaması" hint="Deploy sonunda yeniden başlatılır">
            <Select value={f.pm2} onChange={(e) => set('pm2', e.target.value)}>
              <option value="">Yeniden başlatma</option>
              {[...new Set([...pm2, ...(f.pm2 ? [f.pm2] : [])])].map((n) => <option key={n} value={n}>{n}</option>)}
            </Select>
          </Field>
        </div>

        <div>
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <div className="text-sm font-medium">Adımlar <span className="font-normal text-muted-foreground">· git'ten sonra sırayla çalışır</span></div>
            <div className="flex flex-wrap gap-1.5">
              {presets.map((p) => <Button key={p.label} size="sm" variant="secondary" onClick={() => set('steps', p.steps.map((s) => ({ ...s })))}>{p.label}</Button>)}
            </div>
          </div>
          {f.steps.length === 0 ? (
            <div className="rounded-lg border border-dashed px-4 py-5 text-center text-sm text-muted-foreground">Adım yok. Hazır şablonlardan birini seç veya adım ekle.</div>
          ) : (
            <ol className="space-y-2">
              {f.steps.map((s, i) => (
                <li key={i} className="flex items-start gap-2 rounded-lg border p-2">
                  <span className="mt-2 grid size-5 shrink-0 place-items-center rounded-full bg-muted text-[11px] font-medium">{i + 1}</span>
                  <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[10rem_1fr]">
                    <Input value={s.name} onChange={(e) => setStep(i, { name: e.target.value })} placeholder="Ad" aria-label="Adım adı" />
                    <Input value={s.cmd} onChange={(e) => setStep(i, { cmd: e.target.value })} placeholder="npm run build" className="font-mono" aria-label="Komut" />
                  </div>
                  <div className="flex shrink-0">
                    <Button size="icon" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} aria-label="Yukarı"><ArrowUp size={14} /></Button>
                    <Button size="icon" variant="ghost" disabled={i === f.steps.length - 1} onClick={() => move(i, 1)} aria-label="Aşağı"><ArrowDown size={14} /></Button>
                    <Button size="icon" variant="ghost" onClick={() => set('steps', f.steps.filter((_, j) => j !== i))} aria-label="Sil"><X size={14} /></Button>
                  </div>
                </li>
              ))}
            </ol>
          )}
          <Button size="sm" variant="ghost" className="mt-2" onClick={() => set('steps', [...f.steps, { name: '', cmd: '' }])}><Plus size={14} /> Adım ekle</Button>
          <p className="mt-1 text-xs text-muted-foreground">Komutlar proje klasöründe, klasörün sahibi olan kullanıcıyla bash üzerinden çalışır. Bir adım hata verirse deploy durur.</p>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Sağlık kontrolü adresi" hint="İsteğe bağlı; deploy sonrası bu adres yanıt vermezse deploy başarısız sayılır">
            <Input value={f.healthUrl} onChange={(e) => set('healthUrl', e.target.value)} placeholder="https://app.example.com" />
          </Field>
          <Field label="Çalıştıran kullanıcı" hint="Boş: klasörün sahibi">
            <Input value={f.runAs} onChange={(e) => set('runAs', e.target.value)} placeholder="klasörün sahibi" className="font-mono" />
          </Field>
        </div>
        <div className="divide-y rounded-lg border">
          <label className="flex items-start justify-between gap-6 px-4 py-3 cursor-pointer">
            <div>
              <div className="text-sm font-medium">Hata olursa otomatik geri al</div>
              <p className="text-xs text-muted-foreground">Bir adım veya sağlık kontrolü başarısız olursa önceki sürüm çekilir ve adımlar yeniden çalıştırılır.</p>
            </div>
            <Switch checked={f.autoRollback} onChange={(v) => set('autoRollback', v)} label="Otomatik geri al" />
          </label>
          <label className="flex items-start justify-between gap-6 px-4 py-3 cursor-pointer">
            <div>
              <div className="text-sm font-medium">Telegram'dan deploy edilebilir</div>
              <p className="text-xs text-muted-foreground">Telegram komutları açıksa /deploy menüsünde görünür; onay butonu ile başlatılır.</p>
            </div>
            <Switch checked={f.telegram} onChange={(v) => set('telegram', v)} label="Telegram'dan deploy" />
          </label>
        </div>
        {err && <ErrorBox>{err}</ErrorBox>}
      </div>
    </Dialog>
  )
}

// ---------------------------------------------------------------- sayfa

export function Deploy() {
  const { data, error, reload } = usePoll<ListResp>('/api/deploy', 5000)
  const [form, setForm] = useState<{ open: boolean; p: DeployProject | null }>({ open: false, p: null })
  const [log, setLog] = useState<string | null>(null)
  const [hist, setHist] = useState<DeployItem | null>(null)
  const [starting, setStarting] = useState('')
  const [params, setParams] = useSearchParams()
  useEffect(() => {
    if (params.get('yeni') === '1') {
      const name = params.get('ad') ?? ''
      setForm({ open: true, p: { ...blank, name, dir: params.get('klasor') || '/var/www/', healthUrl: name.includes('.') ? `https://${name}` : '' } as DeployProject })
      setParams({}, { replace: true })
    }
  }, [params, setParams])

  async function run(p: DeployItem, target = '') {
    setStarting(p.id)
    try {
      const r = await api.post<DeployRun>(`/api/deploy/${p.id}/run`, { target })
      setHist(null)
      setLog(r.id)
      reload()
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setStarting('')
    }
  }

  async function remove(p: DeployItem) {
    try { await api.del(`/api/deploy/${p.id}`); toast.ok('Proje silindi'); reload() } catch (e) { toast.err((e as Error).message) }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const projects = data.projects
  const name = (id: string) => projects.find((p) => p.id === id)?.name ?? 'silinmiş proje'

  return (
    <div className="space-y-5">
      <PageHeader
        title="Deploy"
        desc="Projelerini tek tıkla güncelle: git'ten son sürüm çekilir, derlenir ve uygulama yeniden başlatılır. Sorun olursa önceki sürüme dönülür."
        action={<Button variant="default" onClick={() => setForm({ open: true, p: null })}><Plus size={14} /> Proje ekle</Button>}
      />

      {projects.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <Rocket className="text-muted-foreground" />
            <div className="font-medium">Henüz deploy projesi yok</div>
            <p className="max-w-md text-sm text-muted-foreground">Sunucudaki bir git deposunu ekle; hangi komutlarla derleneceğini ve hangi PM2 uygulamasının yeniden başlatılacağını belirle.</p>
            <Button variant="default" onClick={() => setForm({ open: true, p: null })}><Plus size={14} /> Proje ekle</Button>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {projects.map((p) => (
            <Card key={p.id} className="flex flex-col">
              <div className="flex items-start gap-3 px-6 pt-5">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted"><FolderGit2 size={18} /></div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{p.name}</span>
                    {p.telegram && <span title="Telegram'dan deploy edilebilir"><Send size={12} className="text-muted-foreground" /></span>}
                  </div>
                  <div className="truncate font-mono text-xs text-muted-foreground">{p.dir}</div>
                </div>
                <Menu trigger={(o) => <Button size="icon" variant="ghost" onClick={o} aria-label="Diğer işlemler"><EllipsisVertical size={14} /></Button>}>
                  {(close) => <>
                    <MenuItem onClick={() => { close(); setForm({ open: true, p }) }}><Pencil size={14} /> Düzenle</MenuItem>
                    <MenuItem onClick={() => { close(); setHist(p) }}><History size={14} /> Geçmiş ve geri alma</MenuItem>
                    <div className="px-1 pt-1">
                      <ConfirmButton size="sm" variant="ghost" className="w-full justify-start text-destructive" onConfirm={() => { close(); remove(p) }} confirmText="Silinsin mi?">
                        <Trash2 size={14} /> Sil
                      </ConfirmButton>
                    </div>
                  </>}
                </Menu>
              </div>
              <div className="flex-1 space-y-3 px-6 py-4 text-sm">
                {p.branch ? (
                  <div className="flex items-start gap-2">
                    <GitBranch size={14} className="mt-0.5 shrink-0 text-muted-foreground" />
                    <div className="min-w-0">
                      <span className="font-mono text-xs">{p.branch}</span>
                      {p.git.hash && <> · <span className="font-mono text-xs text-muted-foreground">{short(p.git.hash)}</span> <span>{p.git.subject}</span>
                        <span className="text-muted-foreground"> · {ago(Number(p.git.time))}</span></>}
                    </div>
                  </div>
                ) : <div className="text-muted-foreground">Git kullanılmıyor</div>}
                <div className="flex flex-wrap gap-1.5">
                  {p.steps.map((s, i) => <span key={i} className="rounded-md bg-muted px-2 py-0.5 text-xs" title={s.cmd}>{s.name}</span>)}
                  {p.pm2 && <span className="rounded-md bg-muted px-2 py-0.5 text-xs">PM2: {p.pm2}</span>}
                  {p.healthUrl && <span className="rounded-md bg-muted px-2 py-0.5 text-xs">sağlık kontrolü</span>}
                </div>
              </div>
              <div className="flex items-center gap-3 border-t px-6 py-3">
                <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                  {p.last ? (
                    <button className="flex max-w-full items-center gap-2 cursor-pointer hover:text-foreground" onClick={() => setLog(p.last!.id)}>
                      <RunTag r={p.last} />
                      <span className="truncate">{p.last.status === 'running' ? (p.last.step || 'başlıyor') : `${ago(p.last.end || p.last.start)} · ${who(p.last)}`}</span>
                    </button>
                  ) : 'Henüz deploy edilmedi'}
                </div>
                {p.busy ? (
                  <Button size="sm" onClick={() => setLog(p.busy)}><LoaderCircle size={14} className="animate-spin" /> Çıktıyı izle</Button>
                ) : (
                  <ConfirmButton size="sm" variant="default" onConfirm={() => run(p)} loading={starting === p.id} confirmText="Deploy edilsin mi?">
                    <Rocket size={14} /> Deploy et
                  </ConfirmButton>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {data.runs.length > 0 && (
        <Card>
          <CardHeader title="Son deploy'lar" />
          <CardContent className="px-0 pb-0">
            <ul>
              {data.runs.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center gap-3 border-t px-6 py-2.5 text-sm">
                  <RunTag r={r} />
                  <span className="font-medium">{name(r.project)}</span>
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">
                    {r.to && <span className="font-mono text-xs">{short(r.to)} </span>}{r.status === 'ok' || r.status === 'running' ? r.message : r.error}
                  </span>
                  <span className="text-xs text-muted-foreground">{ago(r.start)} · {who(r)}{r.end ? ` · ${duration(r.end - r.start)}` : ''}</span>
                  <Button size="sm" variant="ghost" onClick={() => setLog(r.id)}>Çıktı</Button>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
      {data.runs.length === 0 && projects.length > 0 && <Empty>Henüz deploy yapılmadı.</Empty>}

      <ProjectForm open={form.open} initial={form.p} pm2={data.pm2 ?? []} onClose={() => setForm({ open: false, p: null })} onSaved={reload} />
      <HistoryDialog project={hist} onClose={() => setHist(null)} onRun={(t) => hist && run(hist, t)} onLog={(id) => { setHist(null); setLog(id) }} />
      <LogDialog runId={log} onClose={() => setLog(null)} onDone={reload} />
    </div>
  )
}
