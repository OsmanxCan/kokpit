import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import QRCode from 'qrcode'
import {
  SlidersHorizontal, Send, BellRing, Server, UserRound, Users, Check, RotateCcw, Copy, Trash2, Plus, KeyRound, ShieldCheck, Search,
  PenLine, X, Bold, Italic, Code, Braces, Timer, FolderOpen, Lock, Cloud, ShieldAlert,
} from 'lucide-react'
import { api, type CFEvent, type CFResp, type EventDef, type FileRoot, type SettingsT, type TelegramStatus, type UserT, type VarDef } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { GrantDialog, RootDialog } from '../components/FolderAccess'
import { dateTime } from '../lib/format'
import {
  Card, CardHeader, CardContent, CardFooter, Button, ConfirmButton, Input, Field, Switch, Tag, Alert,
  ErrorBox, Loading, Dialog, Separator, toast, cn,
} from '../components/ui'

type TabId = 'genel' | 'telegram' | 'cloudflare' | 'bildirimler' | 'servisler' | 'dosyalar' | 'hesap' | 'kullanicilar'

const tabs: { id: TabId; label: string; icon: typeof Send; desc: string }[] = [
  { id: 'genel', label: 'Genel', icon: SlidersHorizontal, desc: 'Uyarı eşikleri, oturum ve güncelleme' },
  { id: 'telegram', label: 'Telegram', icon: Send, desc: 'Bot bağlantısı' },
  { id: 'bildirimler', label: 'Bildirimler', icon: BellRing, desc: 'Olaylar ve mesaj şablonları' },
  { id: 'servisler', label: 'İzlenen servisler', icon: Server, desc: 'Çökünce haber verilecek servisler' },
  { id: 'cloudflare', label: 'Cloudflare', icon: Cloud, desc: 'İsteğe bağlı: IP engelleme ve saldırı modu için API token' },
  { id: 'dosyalar', label: 'Dosyalar ve terminal', icon: FolderOpen, desc: 'Dosya gezgininde açılabilen klasörler ve web terminali' },
  { id: 'hesap', label: 'Hesabım', icon: UserRound, desc: 'Şifre ve iki adımlı doğrulama' },
  { id: 'kullanicilar', label: 'Kullanıcılar', icon: Users, desc: 'Panele erişebilen kişiler' },
]

async function save(patch: object): Promise<SettingsT | null> {
  try {
    const r = await fetch('/api/settings', {
      method: 'PUT', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-Panel': '1' }, body: JSON.stringify(patch),
    })
    const d = await r.json()
    if (!r.ok) throw new Error(d.error || `HTTP ${r.status}`)
    toast.ok('Ayarlar kaydedildi')
    return d as SettingsT
  } catch (e) {
    toast.err((e as Error).message)
    return null
  }
}

export function Settings() {
  const [params, setParams] = useSearchParams()
  const tab = (params.get('sekme') as TabId) || 'genel'
  const { data, error, setData } = usePoll<SettingsT>('/api/settings')
  const current = tabs.find((t) => t.id === tab) ?? tabs[0]

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Ayarlar</h1>
        <p className="text-muted-foreground">Kokpit'in tüm ayarları burada. Değişiklikler kaydedildiği anda geçerli olur, yeniden başlatma gerekmez.</p>
      </div>
      <Separator />
      <div className="flex flex-col lg:flex-row gap-8">
        <nav className="lg:w-56 shrink-0">
          <ul className="flex lg:flex-col gap-1 overflow-x-auto pb-1 lg:pb-0">
            {tabs.map((t) => (
              <li key={t.id}>
                <button
                  onClick={() => setParams({ sekme: t.id })}
                  className={cn('flex w-full items-center gap-2.5 h-9 px-3 rounded-md text-sm whitespace-nowrap transition-colors cursor-pointer',
                    tab === t.id ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground')}
                >
                  <t.icon size={16} strokeWidth={1.75} /> {t.label}
                </button>
              </li>
            ))}
          </ul>
        </nav>
        <div className="flex-1 min-w-0 max-w-3xl space-y-6">
          <div className="space-y-1">
            <h2 className="text-lg font-semibold tracking-tight">{current.label}</h2>
            <p className="text-sm text-muted-foreground">{current.desc}</p>
          </div>
          {error && !data ? <ErrorBox>{error}</ErrorBox> : !data ? <Loading /> : (
            <>
              {tab === 'genel' && <GeneralTab s={data} onSaved={setData} />}
              {tab === 'telegram' && <TelegramTab s={data} onSaved={setData} />}
              {tab === 'bildirimler' && <EventsTab s={data} onSaved={setData} />}
              {tab === 'servisler' && <ServicesTab onSaved={setData} />}
              {tab === 'dosyalar' && <FilesTab s={data} onSaved={setData} />}
              {tab === 'cloudflare' && <CloudflareTab />}
              {tab === 'hesap' && <AccountTab />}
              {tab === 'kullanicilar' && <UsersTab />}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

// ======================================================================= Cloudflare

const levelLabel: Record<string, string> = {
  essentially_off: 'Kapalı', low: 'Düşük', medium: 'Orta', high: 'Yüksek', under_attack: 'Under Attack', off: 'Kapalı',
}

function CloudflareTab() {
  const { data, reload, error } = usePoll<CFResp>('/api/cloudflare', 60000)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState('')
  const [events, setEvents] = useState<{ zone: string; list: CFEvent[] | null; err: string } | null>(null)

  async function saveToken() {
    setBusy('token')
    try {
      await api.post('/api/cloudflare/token', { token })
      toast.ok('Token doğrulandı ve kaydedildi')
      setToken('')
      reload()
    } catch (e) { toast.err((e as Error).message) } finally { setBusy('') }
  }
  async function removeToken() {
    try { await api.del('/api/cloudflare/token'); toast.ok('Token silindi'); reload() } catch (e) { toast.err((e as Error).message) }
  }
  async function attack(id: string, on: boolean) {
    setBusy(id)
    try { await api.post(`/api/cloudflare/zones/${id}/attack`, { on }); toast.ok(on ? 'Under Attack modu açıldı' : 'Under Attack modu kapatıldı'); reload() } catch (e) { toast.err((e as Error).message) } finally { setBusy('') }
  }
  async function showEvents(zone: string, name: string) {
    setEvents({ zone: name, list: null, err: '' })
    try { setEvents({ zone: name, list: await api.get<CFEvent[]>(`/api/cloudflare/zones/${zone}/events`), err: '' }) } catch (e) { setEvents({ zone: name, list: [], err: (e as Error).message }) }
  }
  async function unban(ip: string) {
    try { await api.del(`/api/cloudflare/bans/${encodeURIComponent(ip)}`); toast.ok(`${ip} engeli kaldırıldı`); reload() } catch (e) { toast.err((e as Error).message) }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader title="API token" description="İsteğe bağlı. Token girilirse saldırgan IP'ler Cloudflare'de engellenebilir, Under Attack modu panelden veya Telegram'dan açılabilir ve yeni site sihirbazı DNS kaydını kendisi ekler."
          action={data.enabled ? <Tag tone="yesil"><Check size={12} /> Bağlı</Tag> : <Tag>Kapalı</Tag>} />
        <CardContent className="space-y-4">
          <Field label="Token" hint={data.enabled ? <>Kayıtlı: <span className="font-mono">{data.masked}</span>. Değiştirmek için yenisini gir.</> : 'Kaydetmeden önce Cloudflare\'de doğrulanır.'}>
            <Input type="password" autoComplete="off" spellCheck={false} value={token} onChange={(e) => setToken(e.target.value.trim())} className="font-mono" placeholder="Cloudflare API token" />
          </Field>
          {data.error && <ErrorBox>{data.error}</ErrorBox>}
        </CardContent>
        <CardFooter className="justify-between">
          {data.enabled ? <ConfirmButton variant="ghost" size="sm" confirmText="Silinsin mi?" onConfirm={removeToken}><Trash2 size={14} /> Token'ı sil</ConfirmButton> : <span />}
          <Button variant="default" onClick={saveToken} loading={busy === 'token'} disabled={token.length < 20}>Doğrula ve kaydet</Button>
        </CardFooter>
      </Card>

      {data.enabled && (data.zones?.length ?? 0) > 0 && (
        <Card>
          <CardHeader title="Alan adları" description="Under Attack modu açıkken ziyaretçiler siteye girmeden önce birkaç saniyelik bir tarayıcı kontrolünden geçer. Yoğun bir saldırı sırasında kullan; kapatınca önceki güvenlik seviyesine dönülür." />
          <CardContent className="px-0 pb-0">
            <ul>
              {data.zones!.map((z) => {
                const on = z.securityLevel === 'under_attack'
                return (
                  <li key={z.id} className="flex flex-wrap items-center gap-3 border-t px-6 py-3 text-sm">
                    <div className="min-w-0 flex-1">
                      <div className="font-medium">{z.name}</div>
                      <div className="text-xs text-muted-foreground">{z.plan} · güvenlik seviyesi: {levelLabel[z.securityLevel] ?? z.securityLevel ?? '—'}</div>
                    </div>
                    <Button size="sm" variant="ghost" onClick={() => showEvents(z.id, z.name)}>Olaylar</Button>
                    {on
                      ? <Button size="sm" onClick={() => attack(z.id, false)} loading={busy === z.id}>Under Attack'ı kapat</Button>
                      : <ConfirmButton size="sm" variant="outline" onConfirm={() => attack(z.id, true)} loading={busy === z.id} confirmText="Açılsın mı?"><ShieldAlert size={14} /> Under Attack</ConfirmButton>}
                  </li>
                )
              })}
            </ul>
          </CardContent>
        </Card>
      )}

      {data.bans.length > 0 && (
        <Card>
          <CardHeader title="Cloudflare'de engellenen IP'ler" description="Süresi dolanlar kendiliğinden kaldırılır." />
          <CardContent className="px-0 pb-0">
            <ul>
              {data.bans.map((b) => (
                <li key={b.ip} className="flex flex-wrap items-center gap-3 border-t px-6 py-2.5 text-sm">
                  <span className="font-mono text-xs">{b.ip}</span>
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">{b.reason} · {b.by}</span>
                  <span className="text-xs text-muted-foreground">{b.until ? `${dateTime(b.until)} tarihine kadar` : 'kalıcı'}</span>
                  <ConfirmButton size="sm" variant="ghost" onConfirm={() => unban(b.ip)} confirmText="Kaldırılsın mı?">Kaldır</ConfirmButton>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader title="Token nasıl oluşturulur?" />
        <CardContent>
          <ol className="space-y-3 text-sm">
            {[
              <>Cloudflare panelinde <b className="font-medium">My Profile → API Tokens → Create Token → Custom token</b>'a git.</>,
              <>İzinler: <b className="font-medium">Zone · Zone · Read</b>, <b className="font-medium">Zone · Firewall Services · Edit</b>, <b className="font-medium">Zone · Zone Settings · Edit</b>, <b className="font-medium">Zone · Analytics · Read</b>. Yeni site sihirbazının DNS kaydı ekleyebilmesi için <b className="font-medium">Zone · DNS · Edit</b> de ekle (isteğe bağlı).</>,
              <><b className="font-medium">Zone Resources</b> kısmında sadece kendi alan adını (veya alan adlarını) seç.</>,
              <>İstersen <b className="font-medium">Client IP Address Filtering</b> ile token'ı sadece sunucunun IP'sine kilitle.</>,
              <>Oluşan token'ı yukarıya yapıştırıp <b className="font-medium">Doğrula ve kaydet</b>'e bas. Token tarayıcıya hiçbir zaman geri gönderilmez.</>,
            ].map((x, i) => (
              <li key={i} className="flex gap-3">
                <span className="grid size-6 shrink-0 place-items-center rounded-full border text-xs font-medium text-muted-foreground">{i + 1}</span>
                <span className="pt-0.5 text-muted-foreground [&_b]:text-foreground">{x}</span>
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>

      <Dialog open={!!events} onClose={() => setEvents(null)} wide title={`${events?.zone ?? ''}: Cloudflare olayları`} description="Son 24 saatte Cloudflare'in engellediği veya kontrol ettiği istekler (en yeni 100).">
        {!events?.list ? <Loading /> : events.err ? <ErrorBox>{events.err}</ErrorBox> : events.list.length === 0 ? <div className="text-sm text-muted-foreground">Olay yok.</div> : (
          <ul className="max-h-[60vh] divide-y overflow-y-auto rounded-lg border text-xs">
            {events.list.map((e, i) => (
              <li key={i} className="flex items-center gap-3 px-3 py-1.5">
                <span className="w-12 shrink-0 text-muted-foreground">{new Date(e.time).toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })}</span>
                <Tag tone={e.action === 'block' ? 'kirmizi' : 'amber'}>{e.action}</Tag>
                <span className="w-28 shrink-0 truncate font-mono">{e.ip}</span>
                <span className="w-8 shrink-0">{e.country}</span>
                <span className="min-w-0 flex-1 truncate font-mono" title={e.path}>{e.host}{e.path}</span>
                <span className="shrink-0 text-muted-foreground">{e.source}</span>
              </li>
            ))}
          </ul>
        )}
      </Dialog>
    </div>
  )
}

// ======================================================================= Dosyalar

function FilesTab({ s, onSaved }: { s: SettingsT; onSaved: (s: SettingsT) => void }) {
  const [roots, setRoots] = useState<FileRoot[]>(s.fileRoots)
  const [term, setTerm] = useState(!s.terminalOff)
  const [busy, setBusy] = useState(false)
  const [grant, setGrant] = useState(false)
  const [edit, setEdit] = useState<FileRoot | null>(null)
  const update = (r: FileRoot[]) => { setRoots(r); onSaved({ ...s, fileRoots: r }) }

  async function saveTerm(v: boolean) {
    setBusy(true)
    const r = await save({ terminalOff: !v })
    if (r) { onSaved(r); setTerm(!r.terminalOff) }
    setBusy(false)
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader title="Klasör erişimleri" description="Dosya gezgini sadece burada izin verdiğin klasörleri açar ve dışına çıkamaz. Okuma her zaman vardır; yazma ve silme ayrı ayrı verilir."
          action={<Button size="sm" variant="default" onClick={() => setGrant(true)}><Plus size={14} /> Erişim ver</Button>} />
        <CardContent>
          {roots.length === 0 ? <div className="rounded-lg border border-dashed py-6 text-center text-sm text-muted-foreground">Hiçbir klasöre erişim verilmemiş; dosya gezgini kapalı.</div> : (
            <ul className="divide-y rounded-lg border">
              {roots.map((r) => (
                <li key={r.name} className="flex flex-wrap items-center gap-3 px-4 py-2.5">
                  {r.write ? <FolderOpen size={16} className="text-muted-foreground" /> : <Lock size={16} className="text-muted-foreground" />}
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium">{r.name}</div>
                    <div className="truncate font-mono text-xs text-muted-foreground">{r.path}</div>
                  </div>
                  <div className="flex gap-1">
                    <Tag tone="soluk">okuma</Tag>
                    {r.write && <Tag tone="mavi">yazma</Tag>}
                    {r.delete && <Tag tone="amber">silme</Tag>}
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => setEdit(r)}>Düzenle</Button>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader title="Web terminali" />
        <CardContent>
          <label className="flex items-start justify-between gap-6 cursor-pointer">
            <div>
              <div className="text-sm font-medium">Terminal açık</div>
              <p className="text-sm text-muted-foreground">Panelden sunucuda root yetkili bir kabuk açılabilir. Her açılış işlem kayıtlarına yazılır ve Telegram'a bildirilir; 15 dakika işlem yapılmazsa kapanır.</p>
            </div>
            <Switch checked={term} onChange={saveTerm} disabled={busy} />
          </label>
        </CardContent>
      </Card>

      <GrantDialog open={grant} onClose={() => setGrant(false)} onDone={(r) => update(r)} />
      <RootDialog root={edit} onClose={() => setEdit(null)} onDone={update} />
    </div>
  )
}

// ======================================================================= Genel

function NumberField({ label, hint, value, onChange, suffix, min, max }: {
  label: string; hint?: string; value: number; onChange: (v: number) => void; suffix: string; min: number; max: number
}) {
  return (
    <Field label={label} hint={hint}>
      <div className="relative">
        <Input type="number" min={min} max={max} value={Number.isFinite(value) ? value : ''} onChange={(e) => onChange(Number(e.target.value))} className="pr-16" />
        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground pointer-events-none">{suffix}</span>
      </div>
    </Field>
  )
}

function GeneralTab({ s, onSaved }: { s: SettingsT; onSaved: (s: SettingsT) => void }) {
  const [t, setT] = useState(s.thresholds)
  const [ipBind, setIpBind] = useState(s.sessionIpBind)
  const [upd, setUpd] = useState(s.updateCheck)
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof t) => (v: number) => setT({ ...t, [k]: v })
  const dirty = JSON.stringify(t) !== JSON.stringify(s.thresholds) || ipBind !== s.sessionIpBind || upd !== s.updateCheck

  async function submit() {
    setBusy(true)
    const r = await save({ thresholds: t, sessionIpBind: ipBind, updateCheck: upd })
    if (r) onSaved(r)
    setBusy(false)
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader title="Uyarı eşikleri" description="Bu değerler aşılınca ilgili bildirim tetiklenir. Hangi bildirimlerin açık olduğunu Bildirimler sekmesinden seçebilirsin." />
        <CardContent className="grid gap-5 sm:grid-cols-2">
          <NumberField label="CPU eşiği" value={t.cpu} onChange={set('cpu')} suffix="%" min={10} max={100} />
          <NumberField label="CPU süresi" hint="Eşik bu kadar süre aşılırsa uyar" value={t.cpuMinutes} onChange={set('cpuMinutes')} suffix="dakika" min={1} max={120} />
          <NumberField label="RAM eşiği" value={t.ram} onChange={set('ram')} suffix="%" min={10} max={100} />
          <NumberField label="RAM süresi" value={t.ramMinutes} onChange={set('ramMinutes')} suffix="dakika" min={1} max={120} />
          <NumberField label="Disk eşiği" hint="Herhangi bir disk bu kadar dolunca" value={t.disk} onChange={set('disk')} suffix="%" min={10} max={100} />
          <NumberField label="SSH saldırı eşiği" hint="Bir dakikadaki başarısız deneme sayısı" value={t.sshBrute} onChange={set('sshBrute')} suffix="deneme" min={1} max={100000} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader title="Oturum ve güncelleme" />
        <CardContent className="space-y-0 divide-y">
          <label className="flex items-start justify-between gap-6 py-4 first:pt-0 cursor-pointer">
            <div className="space-y-1">
              <div className="text-sm font-medium">Oturumu IP adresine bağla</div>
              <p className="text-[13px] text-muted-foreground">IP değişirse tekrar giriş istenir. Çerez çalınsa bile başka bir yerden kullanılamaz. Mobil veride sık çıkış yapıyorsan kapatabilirsin.</p>
            </div>
            <Switch checked={ipBind} onChange={setIpBind} label="Oturumu IP adresine bağla" />
          </label>
          <label className="flex items-start justify-between gap-6 py-4 last:pb-0 cursor-pointer">
            <div className="space-y-1">
              <div className="text-sm font-medium">Yeni sürümleri otomatik kontrol et</div>
              <p className="text-[13px] text-muted-foreground">GitHub'daki son sürüm 6 saatte bir kontrol edilir. Güncelleme sadece sen onaylayınca kurulur.</p>
            </div>
            <Switch checked={upd} onChange={setUpd} label="Yeni sürümleri otomatik kontrol et" />
          </label>
        </CardContent>
      </Card>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" disabled={!dirty} onClick={() => { setT(s.thresholds); setIpBind(s.sessionIpBind); setUpd(s.updateCheck) }}>Vazgeç</Button>
        <Button variant="default" onClick={submit} loading={busy} disabled={!dirty}>Değişiklikleri kaydet</Button>
      </div>
    </div>
  )
}

// ======================================================================= Telegram

function TelegramTab({ s, onSaved }: { s: SettingsT; onSaved: (s: SettingsT) => void }) {
  const [token, setToken] = useState('')
  const [chat, setChat] = useState(s.telegram.chatId)
  const [prefix, setPrefix] = useState(s.telegram.prefix)
  const [busy, setBusy] = useState<'' | 'kaydet' | 'test'>('')
  const ready = s.tokenSet && !!s.telegram.chatId
  const dirty = token !== '' || chat !== s.telegram.chatId || prefix !== s.telegram.prefix

  async function submit() {
    setBusy('kaydet')
    const r = await save({ telegram: { token, chatId: chat, prefix } })
    if (r) { onSaved(r); setToken('') }
    setBusy('')
  }
  async function test() {
    setBusy('test')
    try {
      await api.post('/api/settings/telegram/test', { token, chatId: chat })
      toast.ok('Test mesajı gönderildi. Telegram\'ı kontrol et.')
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader
          title="Bot bağlantısı"
          description="Bildirimler bu bot üzerinden sana gönderilir."
          action={ready ? <Tag tone="yesil"><Check size={12} /> Bağlı</Tag> : <Tag tone="amber">Ayarlanmadı</Tag>}
        />
        <CardContent className="space-y-5">
          <Field
            label="Bot token"
            htmlFor="tg-token"
            hint={s.tokenSet ? <>Kayıtlı: <span className="font-mono">{s.tokenMasked}</span>. Değiştirmek istemiyorsan boş bırak.</> : 'BotFather\'ın verdiği token\'ın tamamı: 7123456789:AAH…'}
          >
            <Input id="tg-token" type="password" autoComplete="off" spellCheck={false} placeholder={s.tokenSet ? 'Yeni token girmek için yaz' : '7123456789:AAH…'}
              value={token} onChange={(e) => setToken(e.target.value.trim())} className="font-mono" />
          </Field>
          <Field label="Chat ID" htmlFor="tg-chat" hint="Telegram'da @userinfobot'a /start yazınca gelen Id. Grup için başında - olur.">
            <Input id="tg-chat" inputMode="numeric" placeholder="123456789" value={chat} onChange={(e) => setChat(e.target.value.trim())} className="font-mono" />
          </Field>
          <label className="flex items-start justify-between gap-6 cursor-pointer">
            <div className="space-y-1">
              <div className="text-sm font-medium">Mesajların başına sunucu adını ekle</div>
              <p className="text-[13px] text-muted-foreground">Birden fazla sunucun varsa hangi sunucudan geldiğini görmek için.</p>
            </div>
            <Switch checked={prefix} onChange={setPrefix} label="Sunucu adını ekle" />
          </label>
        </CardContent>
        <CardFooter className="justify-between">
          {s.tokenSet ? (
            <ConfirmButton variant="ghost" size="sm" confirmText="Silinsin mi?" onConfirm={async () => { const r = await save({ telegram: { clearToken: true } }); if (r) onSaved(r) }}>
              <Trash2 size={14} /> Token'ı sil
            </ConfirmButton>
          ) : <span />}
          <div className="flex gap-2">
            <Button onClick={test} loading={busy === 'test'} disabled={!(token || s.tokenSet) || !chat}><Send size={14} /> Test mesajı gönder</Button>
            <Button variant="default" onClick={submit} loading={busy === 'kaydet'} disabled={!dirty}>Kaydet</Button>
          </div>
        </CardFooter>
      </Card>

      <CommandsCard s={s} onSaved={onSaved} />

      <Card>
        <CardHeader title="Bot nasıl oluşturulur?" />
        <CardContent>
          <ol className="space-y-3 text-sm">
            {[
              <>Telegram'da <b className="font-medium">@BotFather</b>'ı aç, <span className="font-mono text-[13px]">/newbot</span> yaz ve bota bir ad ver.</>,
              <>Verdiği <b className="font-medium">token</b>'ın tamamını kopyala ve yukarıya yapıştır.</>,
              <>Botunun sayfasını açıp <b className="font-medium">Başlat</b>'a bas. Bu olmadan bot sana yazamaz.</>,
              <><b className="font-medium">@userinfobot</b>'a <span className="font-mono text-[13px]">/start</span> yaz, gelen <b className="font-medium">Id</b>'yi Chat ID alanına gir.</>,
              <>Önce <b className="font-medium">Test mesajı gönder</b>, sonra <b className="font-medium">Kaydet</b>.</>,
            ].map((x, i) => (
              <li key={i} className="flex gap-3">
                <span className="grid size-6 shrink-0 place-items-center rounded-full border text-xs font-medium text-muted-foreground">{i + 1}</span>
                <span className="pt-0.5 text-muted-foreground [&_b]:text-foreground">{x}</span>
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>
    </div>
  )
}

function CommandsCard({ s, onSaved }: { s: SettingsT; onSaved: (s: SettingsT) => void }) {
  const [busy, setBusy] = useState(false)
  const { data: st } = usePoll<TelegramStatus>('/api/telegram/status', 5000)
  const ready = s.tokenSet && !!s.telegram.chatId
  async function toggle(v: boolean) {
    setBusy(true)
    const r = await save({ telegram: { commands: v } })
    if (r) onSaved(r)
    setBusy(false)
  }
  const on = s.telegram.commands
  return (
    <Card>
      <CardHeader title="Telegram'dan komutlar"
        description="Açıksa bota yazarak sunucunun durumunu görebilir, deploy başlatabilir ve PM2 uygulamalarını yeniden başlatabilirsin."
        action={on ? (st?.running ? <Tag tone="yesil">Dinliyor {st.bot}</Tag> : st?.error ? <Tag tone="kirmizi">Hata</Tag> : <Tag tone="amber">Bağlanıyor</Tag>) : <Tag>Kapalı</Tag>} />
      <CardContent className="space-y-4">
        <label className="flex items-start justify-between gap-6 cursor-pointer">
          <div className="space-y-1">
            <div className="text-sm font-medium">Komutları aç</div>
            <p className="text-[13px] text-muted-foreground">Sadece yukarıdaki Chat ID'den gelen mesajlara yanıt verilir; başka biri bota yazsa da hiçbir şey yapılmaz. İşlem yapan her komut önce onay butonu ister.</p>
          </div>
          <Switch checked={on} onChange={toggle} disabled={busy || !ready} label="Telegram komutları" />
        </label>
        {!ready && <p className="text-sm text-muted-foreground">Önce bot token ve Chat ID'yi kaydet.</p>}
        {on && st?.error && <ErrorBox>{st.error}</ErrorBox>}
        {on && (
          <div className="rounded-lg border">
            {[
              ['/durum', 'CPU, RAM, disk, PM2 ve site özeti'],
              ['/servisler', 'PM2 uygulamaları ve durumları'],
              ['/siteler', 'Uptime izleyicileri'],
              ['/deploy', 'Proje seç → onayla → deploy (sadece izin verilen projeler)'],
              ['/yenidenbaslat', 'PM2 uygulaması seç → onayla → yeniden başlat'],
            ].map(([c, d]) => (
              <div key={c} className="flex gap-3 border-b px-4 py-2 text-sm last:border-0">
                <span className="w-32 shrink-0 font-mono text-[13px]">{c}</span>
                <span className="text-muted-foreground">{d}</span>
              </div>
            ))}
          </div>
        )}
        {on && st && st.ignored > 0 && <p className="text-xs text-muted-foreground">Başka sohbetlerden gelen {st.ignored} mesaj yok sayıldı.</p>}
      </CardContent>
    </Card>
  )
}

// ======================================================================= Bildirimler

const levelTone: Record<string, 'kirmizi' | 'amber' | 'mavi' | 'yesil'> = { critical: 'kirmizi', warning: 'amber', info: 'mavi', ok: 'yesil' }
const levelName: Record<string, string> = { critical: 'Kritik', warning: 'Uyarı', info: 'Bilgi', ok: 'Düzeldi' }

function cooldownText(m: number) {
  if (m === 0) return 'her seferinde'
  if (m < 60) return `${m} dk`
  if (m < 1440) return `${Math.round(m / 60)} saat`
  return `${Math.round(m / 1440)} gün`
}

function EventsTab({ s, onSaved }: { s: SettingsT; onSaved: (s: SettingsT) => void }) {
  const defs = usePoll<{ events: EventDef[]; common: VarDef[] }>('/api/settings/events')
  const [editing, setEditing] = useState<EventDef | null>(null)

  if (!defs.data) return <Loading />
  const groups = [...new Set(defs.data.events.map((e) => e.group))]

  async function toggle(id: string, patch: Partial<SettingsT['events'][string]>) {
    const r = await save({ events: { [id]: { ...s.events[id], ...patch } } })
    if (r) onSaved(r)
  }

  return (
    <div className="space-y-6">
      {!(s.tokenSet && s.telegram.chatId) && (
        <Alert tone="amber" title="Telegram ayarlanmadı">Bildirimler şu an sadece panelde listeleniyor. Telefonuna gelmesi için Telegram sekmesinden botu bağla.</Alert>
      )}
      {groups.map((g) => (
        <Card key={g}>
          <CardHeader title={g} />
          <div className="border-t divide-y">
            {defs.data!.events.filter((e) => e.group === g).map((d) => {
              const ev = s.events[d.id] ?? { enabled: d.defaultOn, telegram: true, cooldown: d.defaultCooldown, template: '' }
              const custom = !!ev.template
              return (
                <div key={d.id} className={cn('flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3.5', !ev.enabled && 'opacity-60')}>
                  <Switch checked={ev.enabled} onChange={(v) => toggle(d.id, { enabled: v })} label={`${d.name} bildirimi`} />
                  <div className="min-w-0 flex-1 basis-60">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-sm">{d.name}</span>
                      <Tag tone={levelTone[d.level]}>{levelName[d.level]}</Tag>
                      {custom && <Tag>özel şablon</Tag>}
                    </div>
                    <p className="text-[13px] text-muted-foreground mt-0.5">{d.desc}</p>
                  </div>
                  <div className="flex items-center gap-3 text-[13px] text-muted-foreground">
                    <span title="Aynı uyarı bu süre dolmadan tekrar gönderilmez">↻ {cooldownText(ev.cooldown)}</span>
                    <span className={ev.telegram ? '' : 'line-through'} title={ev.telegram ? 'Telegram\'a gönderilir' : 'Sadece panelde listelenir'}>Telegram</span>
                  </div>
                  <Button size="sm" onClick={() => setEditing(d)}>Düzenle</Button>
                </div>
              )
            })}
          </div>
        </Card>
      ))}
      {editing && (
        <TemplateEditor
          def={editing}
          common={defs.data.common}
          ev={s.events[editing.id]}
          canSend={s.tokenSet && !!s.telegram.chatId}
          onClose={() => setEditing(null)}
          onSaved={(r) => { onSaved(r); setEditing(null) }}
        />
      )}
    </div>
  )
}

function CmdGroup({ heading, children }: { heading: string; children: React.ReactNode }) {
  return (
    <div className="p-1">
      <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">{heading}</div>
      {children}
    </div>
  )
}

function CmdItem({ children, onClick, title }: { children: React.ReactNode; onClick?: () => void; title?: string }) {
  const cls = 'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm outline-none [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-muted-foreground'
  return onClick
    ? <button type="button" title={title} onClick={onClick} className={cn(cls, 'cursor-pointer hover:bg-accent focus-visible:bg-accent')}>{children}</button>
    : <div className={cls}>{children}</div>
}

// Şablon düzenleyici: shadcn Command diyaloğu düzeninde (üstte giriş, altta gruplu liste, en altta eylemler).
function TemplateEditor({ def, common, ev, canSend, onClose, onSaved }: {
  def: EventDef; common: VarDef[]; ev: SettingsT['events'][string]; canSend: boolean; onClose: () => void; onSaved: (s: SettingsT) => void
}) {
  const [tpl, setTpl] = useState(ev?.template || def.defaultTemplate)
  const [cooldown, setCooldown] = useState(ev?.cooldown ?? def.defaultCooldown)
  const [telegram, setTelegram] = useState(ev?.telegram ?? true)
  const [html, setHtml] = useState('')
  const [busy, setBusy] = useState<'' | 'kaydet' | 'test'>('')
  const ta = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const t = window.setTimeout(async () => {
      try {
        const r = await api.post<{ html: string }>('/api/settings/preview', { event: def.id, template: tpl })
        setHtml(r.html)
      } catch { /* önizleme alınamadı */ }
    }, 250)
    return () => window.clearTimeout(t)
  }, [tpl, def.id])

  // Textarea içeriğine göre büyüsün
  useEffect(() => {
    const el = ta.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 260) + 'px'
  }, [tpl])

  function insert(text: string) {
    const el = ta.current
    if (!el) return setTpl(tpl + text)
    const a = el.selectionStart, b = el.selectionEnd
    setTpl(tpl.slice(0, a) + text + tpl.slice(b))
    requestAnimationFrame(() => { el.focus(); el.selectionStart = el.selectionEnd = a + text.length })
  }
  function wrap(tag: string) {
    const el = ta.current
    if (!el) return
    const a = el.selectionStart, b = el.selectionEnd
    setTpl(tpl.slice(0, a) + `<${tag}>` + tpl.slice(a, b) + `</${tag}>` + tpl.slice(b))
    requestAnimationFrame(() => { el.focus(); el.selectionStart = a + tag.length + 2; el.selectionEnd = b + tag.length + 2 })
  }

  async function submit() {
    setBusy('kaydet')
    const r = await save({ events: { [def.id]: { enabled: ev?.enabled ?? true, telegram, cooldown, template: tpl } } })
    setBusy('')
    if (r) onSaved(r)
  }
  async function test() {
    setBusy('test')
    try {
      await api.post(`/api/settings/events/${def.id}/test`, { template: tpl })
      toast.ok('Örnek mesaj Telegram\'a gönderildi')
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  return (
    <Dialog open bare onClose={onClose} title={def.name} className="max-w-xl">
      {/* Üst satır: başlık (CommandInput yerine) */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <PenLine size={16} className="shrink-0 text-muted-foreground" />
        <span className="truncate text-sm font-medium">{def.name}</span>
        <Tag tone={levelTone[def.level]}>{levelName[def.level]}</Tag>
        <button onClick={onClose} aria-label="Kapat" className="ml-auto inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground cursor-pointer"><X size={16} /></button>
      </div>

      {/* Şablon girişi */}
      <div className="shrink-0 border-b">
        <textarea
          ref={ta}
          value={tpl}
          onChange={(e) => setTpl(e.target.value)}
          spellCheck={false}
          rows={4}
          aria-label="Mesaj şablonu"
          placeholder="Mesaj şablonunu yaz…"
          className="block w-full resize-none bg-transparent px-4 py-3 font-mono text-[13px] leading-relaxed outline-none placeholder:text-muted-foreground"
        />
        <div className="flex items-center gap-1 px-3 pb-2">
          {[['b', 'Kalın', Bold], ['i', 'İtalik', Italic], ['code', 'Kod', Code]].map(([t, l, Icon]) => {
            const I = Icon as typeof Bold
            return (
              <button key={t as string} type="button" onClick={() => wrap(t as string)} title={`${l} (<${t}>)`} aria-label={l as string}
                className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground cursor-pointer">
                <I size={14} />
              </button>
            )
          })}
          <span className="ml-2 text-xs text-muted-foreground">Seçili metni biçimlendir · değişken eklemek için aşağıdan seç</span>
        </div>
      </div>

      {/* Liste */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <CmdGroup heading="Önizleme">
          <div className="px-2 pb-2">
            <div className="rounded-lg bg-[#e7ebf0] p-3 dark:bg-[#0e1621]">
              <div className="ml-auto w-fit max-w-full whitespace-pre-wrap break-words rounded-2xl rounded-br-sm bg-white px-3 py-2 text-[13.5px] leading-snug text-black shadow-sm dark:bg-[#182533] dark:text-[#e9eef3] [&_code]:font-mono [&_code]:text-[12.5px] [&_pre]:font-mono"
                // Sunucu tüm metni kaçışlar, sadece <b> <i> <u> <s> <code> <pre> bırakır
                dangerouslySetInnerHTML={{ __html: html || '&nbsp;' }} />
            </div>
          </div>
        </CmdGroup>
        <div className="-mx-1 h-px bg-border" />
        <CmdGroup heading="Değişkenler">
          {[...def.vars, ...common].map((v) => (
            <CmdItem key={v.name} onClick={() => insert(`{{${v.name}}}`)} title="Şablona ekle">
              <Braces />
              <span className="font-mono text-[13px]">{`{{${v.name}}}`}</span>
              <span className="truncate text-muted-foreground">{v.desc}</span>
              <span className="ml-auto shrink-0 pl-3 text-xs tracking-wide text-muted-foreground">{v.sample}</span>
            </CmdItem>
          ))}
        </CmdGroup>
        <div className="-mx-1 h-px bg-border" />
        <CmdGroup heading="Gönderim">
          <CmdItem>
            <Timer />
            <span>Tekrar bekleme süresi</span>
            <span className="ml-auto flex items-center gap-2">
              <Input type="number" min={0} max={10080} value={cooldown} onChange={(e) => setCooldown(Number(e.target.value))} className="h-7 w-20 text-right" aria-label="Tekrar bekleme süresi (dakika)" />
              <span className="text-xs text-muted-foreground">dakika</span>
            </span>
          </CmdItem>
          <CmdItem>
            <Send />
            <span>Telegram'a gönder</span>
            <span className="ml-auto"><Switch checked={telegram} onChange={setTelegram} label="Telegram'a gönder" /></span>
          </CmdItem>
        </CmdGroup>
      </div>

      {/* Alt eylemler */}
      <div className="flex shrink-0 items-center gap-2 border-t px-3 py-2.5">
        <Button variant="ghost" size="sm" onClick={() => setTpl(def.defaultTemplate)} disabled={tpl === def.defaultTemplate}><RotateCcw size={14} /> Varsayılana dön</Button>
        <div className="ml-auto flex gap-2">
          <Button size="sm" onClick={test} loading={busy === 'test'} disabled={!canSend} title={canSend ? '' : 'Önce Telegram\'ı bağla'}><Send size={14} /> Örnek gönder</Button>
          <Button size="sm" variant="default" onClick={submit} loading={busy === 'kaydet'}>Kaydet</Button>
        </div>
      </div>
    </Dialog>
  )
}

// ======================================================================= Servisler

type SvcRow = { name: string; desc: string; status: string; healthy: boolean; watched: boolean }

function ServicesTab({ onSaved }: { onSaved: (s: SettingsT) => void }) {
  const { data, error } = usePoll<SvcRow[]>('/api/settings/services')
  const [sel, setSel] = useState<Set<string> | null>(null)
  const [q, setQ] = useState('')
  const [onlyRunning, setOnlyRunning] = useState(true)
  const [busy, setBusy] = useState(false)

  const initial = useMemo(() => new Set((data ?? []).filter((x) => x.watched).map((x) => x.name)), [data])
  const selected = sel ?? initial
  const dirty = sel !== null && (sel.size !== initial.size || [...sel].some((x) => !initial.has(x)))

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  const rows = data.filter((r) =>
    (!q || r.name.toLowerCase().includes(q.toLowerCase()) || r.desc.toLowerCase().includes(q.toLowerCase())) &&
    (!onlyRunning || r.status === 'active/running' || selected.has(r.name)))

  function flip(n: string) {
    const s = new Set(selected)
    if (s.has(n)) s.delete(n)
    else s.add(n)
    setSel(s)
  }

  async function submit() {
    setBusy(true)
    const r = await save({ watchServices: [...selected].sort() })
    if (r) { onSaved(r); setSel(null) }
    setBusy(false)
  }

  return (
    <Card>
      <CardHeader
        title={`${selected.size} servis izleniyor`}
        description="Seçili servislerden biri durursa “Servis çöktü” bildirimi gönderilir. PM2 süreçleri ve Docker container'ları her zaman izlenir."
      />
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="relative flex-1 min-w-48">
            <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Servis ara" className="pl-9" />
          </div>
          <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer">
            <Switch checked={onlyRunning} onChange={setOnlyRunning} label="Sadece çalışanlar" /> Sadece çalışanlar
          </label>
        </div>
        <div className="rounded-lg border divide-y max-h-[460px] overflow-y-auto">
          {rows.map((r) => (
            <label key={r.name} className="flex items-center gap-3 px-3 py-2.5 hover:bg-muted/50 cursor-pointer">
              <input type="checkbox" checked={selected.has(r.name)} onChange={() => flip(r.name)} className="size-4 accent-[var(--primary)] cursor-pointer" />
              <span className={cn('size-1.5 rounded-full shrink-0', r.healthy ? 'bg-success' : 'bg-muted-foreground/40')} />
              <span className="font-medium text-sm">{r.name}</span>
              <span className="text-[13px] text-muted-foreground truncate">{r.desc}</span>
            </label>
          ))}
          {rows.length === 0 && <div className="text-sm text-muted-foreground text-center py-8">Eşleşen servis yok.</div>}
        </div>
      </CardContent>
      <CardFooter className="justify-end">
        <Button variant="ghost" disabled={!dirty} onClick={() => setSel(null)}>Vazgeç</Button>
        <Button variant="default" onClick={submit} loading={busy} disabled={!dirty}>Kaydet</Button>
      </CardFooter>
    </Card>
  )
}

// ======================================================================= Hesap ve kullanıcılar

function QR({ url }: { url: string }) {
  const [src, setSrc] = useState('')
  useEffect(() => { QRCode.toDataURL(url, { margin: 1, width: 360, errorCorrectionLevel: 'M' }).then(setSrc) }, [url])
  return src ? <img src={src} alt="2FA QR kodu" className="size-44 rounded-lg bg-white p-2 border" /> : <div className="size-44 rounded-lg bg-muted animate-pulse" />
}

// TwoFASetup QR'ı gösterir, kodu ve onay şifresini alır.
function TwoFASetup({ username, password, onDone, submitLabel }: { username: string; password?: string; onDone: () => void; submitLabel: string }) {
  const [secret, setSecret] = useState<{ secret: string; url: string } | null>(null)
  const [code, setCode] = useState('')
  const [current, setCurrent] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    api.post<{ secret: string; url: string }>('/api/users/2fa', { username }).then(setSecret).catch((e) => setErr(e.message))
  }, [username])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      await api.post('/api/users', { username, password: password ?? '', code, currentPassword: current })
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (!secret) return err ? <ErrorBox>{err}</ErrorBox> : <Loading />
  const grouped = secret.secret.match(/.{1,4}/g)?.join(' ')

  return (
    <form onSubmit={submit} className="space-y-5">
      <div className="flex flex-col sm:flex-row gap-5 items-start">
        <QR url={secret.url} />
        <div className="space-y-3 text-sm min-w-0">
          <p className="text-muted-foreground">Aegis, 2FAS veya Google Authenticator ile QR kodu okut.</p>
          <div>
            <div className="text-xs text-muted-foreground mb-1">QR okunmazsa elle gir</div>
            <button type="button" onClick={() => { navigator.clipboard?.writeText(secret.secret); toast.ok('Anahtar kopyalandı') }}
              className="inline-flex items-center gap-2 rounded-md border bg-muted/50 px-2.5 py-1.5 font-mono text-xs break-all text-left hover:bg-accent cursor-pointer">
              {grouped} <Copy size={12} className="shrink-0" />
            </button>
          </div>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Uygulamadaki 6 haneli kod">
          <Input inputMode="numeric" autoComplete="one-time-code" maxLength={6} placeholder="000000" value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} className="font-mono tracking-[0.3em] text-center" required />
        </Field>
        <Field label="Senin mevcut şifren" hint="Onay için">
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
      </div>
      {err && <ErrorBox>{err}</ErrorBox>}
      <div className="flex justify-end">
        <Button type="submit" variant="default" loading={busy} disabled={code.length !== 6 || !current}>{submitLabel}</Button>
      </div>
    </form>
  )
}

function AccountTab() {
  const me = usePoll<{ username: string }>('/api/auth/me')
  const [cur, setCur] = useState('')
  const [nw, setNw] = useState('')
  const [nw2, setNw2] = useState('')
  const [busy, setBusy] = useState(false)
  const [reset2fa, setReset2fa] = useState(false)

  async function changePw(e: React.FormEvent) {
    e.preventDefault()
    if (nw !== nw2) return toast.err('Yeni şifreler eşleşmiyor')
    setBusy(true)
    try {
      await api.post('/api/account/password', { current: cur, new: nw })
      toast.ok('Şifren değiştirildi. Diğer cihazlardaki oturumların kapatıldı.')
      setCur(''); setNw(''); setNw2('')
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const strength = nw.length === 0 ? 0 : nw.length < 12 ? 1 : nw.length < 16 ? 2 : 3

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader title="Şifre" description="Değiştirdiğinde diğer cihazlardaki oturumların kapanır." />
        <form onSubmit={changePw}>
          <CardContent className="grid gap-4 max-w-md">
            <Field label="Mevcut şifre"><Input type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} required /></Field>
            <Field label="Yeni şifre" hint="En az 12 karakter. Uzun bir cümle hatırlaması kolay ve güçlüdür.">
              <Input type="password" autoComplete="new-password" value={nw} onChange={(e) => setNw(e.target.value)} required />
              <div className="flex gap-1 mt-1" aria-hidden>
                {[1, 2, 3].map((i) => <div key={i} className={cn('h-1 flex-1 rounded-full', strength >= i ? ['', 'bg-destructive', 'bg-warning', 'bg-success'][strength] : 'bg-muted')} />)}
              </div>
            </Field>
            <Field label="Yeni şifre (tekrar)"><Input type="password" autoComplete="new-password" value={nw2} onChange={(e) => setNw2(e.target.value)} required aria-invalid={!!nw2 && nw !== nw2} /></Field>
          </CardContent>
          <CardFooter className="justify-end">
            <Button type="submit" variant="default" loading={busy} disabled={!cur || nw.length < 12 || nw !== nw2}><KeyRound size={14} /> Şifreyi değiştir</Button>
          </CardFooter>
        </form>
      </Card>

      <Card>
        <CardHeader title="İki adımlı doğrulama (2FA)" description="Telefonunu değiştirdiysen veya authenticator uygulamasını yeniden kurduysan buradan yeni bir QR oluştur."
          action={<Tag tone="yesil"><ShieldCheck size={12} /> Açık</Tag>} />
        <CardContent>
          {reset2fa && me.data ? (
            <TwoFASetup username={me.data.username} submitLabel="Yeni 2FA'yı etkinleştir" onDone={() => { toast.ok('2FA yenilendi. Artık yeni koddan giriş yapacaksın.'); setReset2fa(false) }} />
          ) : (
            <div className="flex items-center justify-between gap-4">
              <p className="text-sm text-muted-foreground">Yeni kodu onaylayana kadar eskisi çalışmaya devam eder.</p>
              <Button onClick={() => setReset2fa(true)}>2FA'yı yeniden kur</Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function UsersTab() {
  const { data, error, reload } = usePoll<{ users: UserT[]; me: string }>('/api/users')
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [step, setStep] = useState<1 | 2>(1)
  const [del, setDel] = useState<string | null>(null)
  const [delPw, setDelPw] = useState('')

  function closeAdd() { setAdding(false); setStep(1); setName(''); setPw('') }

  async function remove() {
    try {
      await fetch(`/api/users/${encodeURIComponent(del!)}`, {
        method: 'DELETE', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-Panel': '1' },
        body: JSON.stringify({ currentPassword: delPw }),
      }).then(async (r) => { if (!r.ok) throw new Error((await r.json()).error) })
      toast.ok(`${del} silindi`)
      setDel(null); setDelPw(''); reload()
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  return (
    <Card>
      <CardHeader title={`${data.users.length} kullanıcı`} description="Her kullanıcının kendi şifresi ve 2FA'sı vardır. Hepsinin yetkisi aynıdır."
        action={<Button size="sm" onClick={() => setAdding(true)}><Plus size={14} /> Kullanıcı ekle</Button>} />
      <div className="border-t divide-y">
        {data.users.map((u) => (
          <div key={u.username} className="flex items-center gap-3 px-5 py-3">
            <div className="grid size-8 place-items-center rounded-full bg-muted text-xs font-semibold">{u.username.slice(0, 2).toUpperCase()}</div>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium flex items-center gap-2">{u.username} {u.username === data.me && <Tag>sen</Tag>}</div>
              <div className="text-xs text-muted-foreground">Eklendi {dateTime(u.createdAt)} · son değişiklik {dateTime(u.updatedAt)}</div>
            </div>
            {u.username !== data.me && <Button variant="ghost" size="icon" onClick={() => setDel(u.username)} aria-label={`${u.username} kullanıcısını sil`}><Trash2 size={15} /></Button>}
          </div>
        ))}
      </div>

      <Dialog open={adding} onClose={closeAdd} title="Kullanıcı ekle" wide={step === 2}
        description={step === 1 ? 'Yeni kullanıcı panelde senin kadar yetkili olur.' : `${name} için 2FA kurulumu. QR'ı yeni kullanıcının telefonuyla okutun.`}
        footer={step === 1 ? <><Button variant="ghost" onClick={closeAdd}>Vazgeç</Button><Button variant="default" disabled={!/^[A-Za-z0-9._-]{3,32}$/.test(name) || pw.length < 12} onClick={() => setStep(2)}>Devam</Button></> : undefined}>
        {step === 1 ? (
          <div className="grid gap-4">
            <Field label="Kullanıcı adı" hint="3–32 karakter: harf, rakam, nokta, tire"><Input value={name} onChange={(e) => setName(e.target.value.trim())} autoFocus /></Field>
            <Field label="Şifre" hint="En az 12 karakter"><Input type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} /></Field>
          </div>
        ) : (
          <TwoFASetup username={name} password={pw} submitLabel="Kullanıcıyı oluştur" onDone={() => { toast.ok(`${name} eklendi`); closeAdd(); reload() }} />
        )}
      </Dialog>

      <Dialog open={!!del} onClose={() => { setDel(null); setDelPw('') }} title={`${del} silinsin mi?`}
        description="Bu kullanıcının tüm oturumları kapanır ve bir daha giriş yapamaz."
        footer={<><Button variant="ghost" onClick={() => setDel(null)}>Vazgeç</Button><Button variant="destructive" disabled={!delPw} onClick={remove}>Sil</Button></>}>
        <Field label="Onay için şifren"><Input type="password" autoComplete="current-password" value={delPw} onChange={(e) => setDelPw(e.target.value)} autoFocus /></Field>
      </Dialog>
    </Card>
  )
}
