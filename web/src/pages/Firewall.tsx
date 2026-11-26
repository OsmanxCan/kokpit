import { useEffect, useState } from 'react'
import { RefreshCw, Plus, Trash2, ShieldAlert, ShieldCheck, ShieldOff, TerminalSquare } from 'lucide-react'
import { api, type FwRule, type FwStatus, type ListenPort } from '../lib/api'
import { usePoll } from '../lib/hooks'
import {
  Alert, Button, Card, CardContent, CardHeader, ConfirmButton, Dialog, Empty, ErrorBox, Field, Input, Loading,
  PageHeader, Select, Table, Tag, Td, Th, THead, Tr, toast,
} from '../components/ui'

const actionTone = (a: string) => a.startsWith('ALLOW') ? 'yesil' : a.startsWith('LIMIT') ? 'amber' : 'kirmizi'
const actionLabel = (a: string) => {
  const [act, dir] = a.split(' ')
  const t = { ALLOW: 'İzin ver', DENY: 'Engelle', REJECT: 'Reddet', LIMIT: 'Sınırla' }[act] ?? act
  return dir && dir !== 'IN' ? `${t} (${dir === 'OUT' ? 'giden' : 'yönlendirme'})` : t
}
const fromLabel = (f: string) => f.startsWith('Anywhere') ? 'Herkes' + (f.includes('(v6)') ? ' (IPv6)' : '') : f

function Countdown({ until }: { until: number }) {
  const [now, setNow] = useState(Date.now() / 1000)
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now() / 1000), 1000)
    return () => window.clearInterval(t)
  }, [])
  const s = Math.max(0, Math.round(until - now))
  return <span className="font-mono tabular-nums">{Math.floor(s / 60)}:{String(s % 60).padStart(2, '0')}</span>
}

function AddRule({ open, onClose, onDone, sshPort }: { open: boolean; onClose: () => void; onDone: (s: FwStatus) => void; sshPort: number }) {
  const empty = { action: 'allow', port: '', proto: 'tcp', from: '', comment: '' }
  const [f, setF] = useState(empty)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => { if (open) { setF(empty); setErr('') } }, [open]) // eslint-disable-line react-hooks/exhaustive-deps
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })

  async function save() {
    setBusy(true)
    setErr('')
    try {
      const s = await api.post<FwStatus>('/api/firewall/rules', { ...f, from: f.from.trim() || 'any' })
      toast.ok('Kural eklendi')
      onDone(s)
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const presets = [
    { label: `SSH (${sshPort})`, v: { port: String(sshPort), proto: 'tcp' } },
    { label: 'HTTP', v: { port: '80', proto: 'tcp' } },
    { label: 'HTTPS', v: { port: '443', proto: 'tcp' } },
  ]

  return (
    <Dialog open={open} onClose={onClose} title="Kural ekle" description="Kural hemen uygulanır. Port boş bırakılırsa kaynak adresten gelen tüm trafik için geçerli olur."
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="default" onClick={save} loading={busy}>Kuralı ekle</Button></>}>
      <div className="space-y-4">
        <div className="flex flex-wrap gap-1.5">
          {presets.map((p) => (
            <Button key={p.label} size="sm" variant="secondary" onClick={() => setF({ ...f, ...p.v, action: 'allow' })}>{p.label}</Button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="İşlem">
            <Select value={f.action} onChange={set('action')}>
              <option value="allow">İzin ver</option>
              <option value="deny">Engelle</option>
              <option value="limit">Sınırla (30 sn'de 6 bağlantı)</option>
            </Select>
          </Field>
          <Field label="Protokol">
            <Select value={f.proto} onChange={set('proto')}>
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
              <option value="any">İkisi de</option>
            </Select>
          </Field>
        </div>
        <Field label="Port" hint="Tek port (443) veya aralık (8000:8100)">
          <Input value={f.port} onChange={set('port')} placeholder="örn. 443" inputMode="numeric" />
        </Field>
        <Field label="Kaynak" hint="Boş bırakırsan herkes. Tek IP (1.2.3.4) veya ağ (10.0.0.0/8) yazabilirsin.">
          <Input value={f.from} onChange={set('from')} placeholder="herkes" />
        </Field>
        <Field label="Açıklama" hint="Kuralın ne için olduğunu hatırlamak için (isteğe bağlı)">
          <Input value={f.comment} onChange={set('comment')} maxLength={60} placeholder="örn. OpenVPN" />
        </Field>
        {err && <ErrorBox>{err}</ErrorBox>}
      </div>
    </Dialog>
  )
}

const exposureInfo: Record<ListenPort['exposure'], { label: string; tone: 'kirmizi' | 'amber' | 'yesil' | 'soluk' }> = {
  open: { label: 'Dışarı açık', tone: 'amber' },
  limited: { label: 'Sınırlı', tone: 'soluk' },
  protected: { label: 'Firewall engelliyor', tone: 'yesil' },
  local: { label: 'Sadece sunucu içi', tone: 'soluk' },
}

function ListeningPorts() {
  const { data, error, reload, loading } = usePoll<{ items: ListenPort[]; error?: string; at: number }>('/api/security/ports', 60000)
  const [showLocal, setShowLocal] = useState(false)
  const items = (data?.items ?? []).filter((p) => showLocal || p.exposure !== 'local')
  const risky = (data?.items ?? []).filter((p) => p.risk)
  return (
    <Card>
      <CardHeader title="Dinleyen portlar"
        description="Sunucuda bağlantı bekleyen programlar ve internetten erişilip erişilemedikleri. Dışarıya yeni bir port açılırsa bildirim gelir."
        action={<Button size="sm" variant="ghost" onClick={() => { api.get('/api/security/ports?refresh=1').then(reload) }} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>} />
      <CardContent className="px-0 pb-0">
        {error && !data ? <div className="px-6 pb-6"><ErrorBox>{error}</ErrorBox></div> : !data ? <Loading /> : data.error ? <div className="px-6 pb-6"><ErrorBox>{data.error}</ErrorBox></div> : (
          <>
            {risky.length > 0 && (
              <div className="mx-6 mb-4"><Alert tone="kirmizi" title="Dışarıya açık olmaması gereken servis var">
                {risky.map((p) => `${p.risk} (${p.port}/${p.proto})`).join(', ')}. Sadece sunucu içinden (127.0.0.1) dinleyecek şekilde ayarla veya firewall kuralını kaldır.
              </Alert></div>
            )}
            <Table>
              <THead><Tr><Th>Port</Th><Th>Program</Th><Th>Dinlenen adres</Th><Th>Durum</Th><Th>Açıklama</Th></Tr></THead>
              <tbody>
                {items.map((p) => (
                  <Tr key={p.proto + p.port}>
                    <Td className="font-mono text-xs whitespace-nowrap">{p.port}/{p.proto}</Td>
                    <Td>{p.program || '—'}{p.docker && <span className="ml-1.5"><Tag tone="mavi">Docker</Tag></span>}</Td>
                    <Td className="font-mono text-xs text-muted-foreground">{p.addrs.map((a) => (a === '0.0.0.0' || a === '*' || a === '::' ? 'tümü' : a)).filter((a, i, l) => l.indexOf(a) === i).join(', ')}</Td>
                    <Td><Tag tone={p.risk ? 'kirmizi' : exposureInfo[p.exposure].tone}>{exposureInfo[p.exposure].label}</Tag></Td>
                    <Td className="text-muted-foreground">{p.reason}{p.risk && <span className="text-destructive"> · {p.risk}</span>}</Td>
                  </Tr>
                ))}
              </tbody>
            </Table>
            <div className="border-t px-6 py-3">
              <Button size="sm" variant="ghost" onClick={() => setShowLocal(!showLocal)}>
                {showLocal ? 'Sadece sunucu içi olanları gizle' : `Sadece sunucu içi olanları da göster (${data.items.filter((p) => p.exposure === 'local').length})`}
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function Firewall() {
  const { data, error, reload, loading, setData } = usePoll<FwStatus>('/api/firewall', 15000)
  const [add, setAdd] = useState(false)
  const [enableOpen, setEnableOpen] = useState(false)
  const [rollback, setRollback] = useState(5)
  const [busy, setBusy] = useState('')
  const [raw, setRaw] = useState(false)

  async function state(action: 'enable' | 'disable' | 'confirm') {
    setBusy(action)
    try {
      const s = await api.post<FwStatus>('/api/firewall/state', { action, rollback })
      setData(s)
      setEnableOpen(false)
      toast.ok({ enable: 'Firewall açıldı', disable: 'Firewall kapatıldı', confirm: 'Onaylandı, otomatik geri alma iptal edildi' }[action])
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function del(r: FwRule) {
    try {
      setData(await api.del<FwStatus>(`/api/firewall/rules/${r.num}`, { to: r.to }))
      toast.ok('Kural silindi')
    } catch (e) {
      toast.err((e as Error).message)
      reload()
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  if (!data.installed) {
    return (
      <div className="space-y-5">
        <PageHeader title="Firewall" />
        <Alert tone="amber" title="UFW kurulu değil">Sunucuda <code>sudo apt install ufw</code> komutuyla kurabilirsin.</Alert>
      </div>
    )
  }

  const rules = data.rules ?? []
  const sshRules = rules.filter((r) => r.ssh)

  return (
    <div className="space-y-5">
      <PageHeader
        title="Firewall"
        desc="Sunucuya hangi portlardan, kimin bağlanabileceğini belirleyen UFW kuralları. Değişiklikler hemen uygulanır."
        action={
          <div className="flex gap-2">
            <Button onClick={reload} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>
            <Button variant="default" onClick={() => setAdd(true)}><Plus size={14} /> Kural ekle</Button>
          </div>
        }
      />

      {data.rollback > 0 && (
        <Alert tone="amber" title="Firewall deneme modunda">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span>Paneli ve SSH'ı kullanabiliyorsan onayla. Onaylamazsan firewall <Countdown until={data.rollback} /> sonra kendiliğinden kapanır.</span>
            <Button size="sm" variant="default" onClick={() => state('confirm')} loading={busy === 'confirm'}>Her şey çalışıyor, onayla</Button>
          </div>
        </Alert>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <Card className="md:col-span-2">
          <CardContent className="flex flex-wrap items-center justify-between gap-4 py-5">
            <div className="flex items-center gap-3">
              <div className={`flex size-10 items-center justify-center rounded-lg ${data.active ? 'bg-success/10 text-success' : 'bg-destructive/10 text-destructive'}`}>
                {data.active ? <ShieldCheck size={20} /> : <ShieldOff size={20} />}
              </div>
              <div>
                <div className="font-medium">{data.active ? 'Firewall açık' : 'Firewall kapalı'}</div>
                <div className="text-sm text-muted-foreground">
                  {data.active
                    ? <>Gelen bağlantılar: <b className="text-foreground">{data.incoming === 'allow' ? 'izinli' : 'engelli'}</b>, kurallarda izin verilenler hariç</>
                    : 'Tüm portlar dışarıya açık. Açmadan önce gerekli portlara izin verdiğinden emin ol.'}
                </div>
              </div>
            </div>
            {data.active
              ? <ConfirmButton variant="outline" onConfirm={() => state('disable')} loading={busy === 'disable'} confirmText="Kapatılsın mı?">Firewall'u kapat</ConfirmButton>
              : <Button variant="default" onClick={() => setEnableOpen(true)}>Firewall'u aç</Button>}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-5">
            <div className="flex items-center gap-2 text-sm font-medium">
              {data.sshAllowed ? <ShieldCheck size={16} className="text-success" /> : <ShieldAlert size={16} className="text-destructive" />}
              SSH erişimi
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              {data.sshAllowed
                ? <>Port {data.sshPort} için {sshRules.length} kural var. Bu kurallar silinirken ayrıca uyarılırsın.</>
                : <>Port {data.sshPort} için herkese açık bir kural yok. Firewall açılırsa SSH bağlantın kopabilir.</>}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader title="Kurallar" description={`${rules.length} kural · yukarıdan aşağıya ilk eşleşen kural uygulanır`}
          action={<Button size="sm" variant="ghost" onClick={() => setRaw(!raw)}><TerminalSquare size={14} /> {raw ? 'Tabloyu göster' : 'Ham çıktı'}</Button>} />
        <CardContent className="px-0 pb-0">
          {raw ? (
            <pre className="mx-6 mb-6 overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs">{data.raw || '(boş)'}</pre>
          ) : rules.length === 0 ? <Empty>Henüz kural yok.</Empty> : (
            <Table>
              <THead>
                <Tr><Th className="w-12 pl-6">#</Th><Th>Port / hedef</Th><Th>İşlem</Th><Th>Kaynak</Th><Th>Açıklama</Th><Th className="pr-6" /></Tr>
              </THead>
              <tbody>
                {rules.map((r) => (
                  <Tr key={r.num}>
                    <Td className="pl-6 text-muted-foreground tabular-nums">{r.num}</Td>
                    <Td>
                      {r.to.startsWith('Anywhere') ? <span>Tüm portlar</span> : <span className="font-mono text-xs">{r.to.replace(' (v6)', '')}</span>}
                      {r.v6 && <span className="ml-1.5 text-xs text-muted-foreground">IPv6</span>}
                      {r.ssh && <span className="ml-1.5"><Tag tone="mavi">SSH</Tag></span>}
                    </Td>
                    <Td><Tag tone={actionTone(r.action)}>{actionLabel(r.action)}</Tag></Td>
                    <Td className={r.from.startsWith('Anywhere') ? '' : 'font-mono text-xs'}>{fromLabel(r.from)}</Td>
                    <Td className="text-muted-foreground">{r.comment || '—'}</Td>
                    <Td className="pr-6 text-right">
                      <ConfirmButton size="sm" variant="ghost" onConfirm={() => del(r)}
                        confirmText={r.ssh ? 'SSH kuralı, silinsin mi?' : 'Silinsin mi?'} aria-label="Kuralı sil">
                        <Trash2 size={14} />
                      </ConfirmButton>
                    </Td>
                  </Tr>
                ))}
              </tbody>
            </Table>
          )}
        </CardContent>
      </Card>

      <ListeningPorts />

      <AddRule open={add} onClose={() => setAdd(false)} onDone={setData} sshPort={data.sshPort} />

      <Dialog open={enableOpen} onClose={() => setEnableOpen(false)} title="Firewall açılsın mı?"
        description="Kurallarda izin verilmeyen tüm gelen bağlantılar engellenecek."
        footer={<><Button onClick={() => setEnableOpen(false)}>Vazgeç</Button><Button variant="default" onClick={() => state('enable')} loading={busy === 'enable'} disabled={!data.sshAllowed}>Firewall'u aç</Button></>}>
        <div className="space-y-4">
          {!data.sshAllowed && <ErrorBox>SSH portu ({data.sshPort}) için herkese izin veren bir kural yok. Önce bu kuralı ekle.</ErrorBox>}
          <Field label="Güvenlik için otomatik geri alma" hint="Bu süre içinde onaylamazsan firewall kendiliğinden kapanır. Yanlış bir kural yüzünden sunucuya erişimin kesilirse kurtarır.">
            <Select value={rollback} onChange={(e) => setRollback(Number(e.target.value))}>
              <option value={2}>2 dakika</option>
              <option value={5}>5 dakika</option>
              <option value={10}>10 dakika</option>
              <option value={0}>Kullanma</option>
            </Select>
          </Field>
        </div>
      </Dialog>
    </div>
  )
}
