import { useEffect, useState } from 'react'
import { Plus, Pencil, Trash2, FlaskConical, ScrollText, ShieldBan } from 'lucide-react'
import { api, type LogRule, type LogRuleHit, type LogRuleTest } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { ago } from '../lib/format'
import {
  Alert, Button, Card, CardContent, CardHeader, ConfirmButton, Dialog, Empty, ErrorBox, Field, Input, Loading, PageHeader, Select,
  Switch, Tag, toast,
} from '../components/ui'

type Resp = { rules: LogRule[]; status: Record<string, string>; hits: LogRuleHit[]; pm2: string[] | null; cloudflare: boolean }

const blank: LogRule = { id: '', name: '', source: 'journal', path: '', pattern: '', regex: false, threshold: 10, window: 5, banHours: 1, action: 'auto', enabled: true }

const sourceLabel = { file: 'Log dosyası', journal: 'systemd servisi', pm2: 'PM2 uygulaması' }
const actionLabel = { auto: 'Otomatik', cloudflare: 'Cloudflare', firewall: 'Sunucu firewall\'u', notify: 'Sadece bildir' }
const hourOptions = [{ v: 1, l: '1 saat' }, { v: 6, l: '6 saat' }, { v: 24, l: '24 saat' }, { v: 168, l: '1 hafta' }, { v: 0, l: 'Kalıcı' }]

function RuleForm({ open, initial, pm2, cf, onClose, onSaved }: { open: boolean; initial: LogRule | null; pm2: string[]; cf: boolean; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<LogRule>(blank)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')
  const [test, setTest] = useState<LogRuleTest | null>(null)
  useEffect(() => { if (open) { setF(initial ?? blank); setErr(''); setTest(null) } }, [open, initial])
  const set = <K extends keyof LogRule>(k: K, v: LogRule[K]) => { setF((x) => ({ ...x, [k]: v })); setTest(null) }

  async function runTest() {
    setBusy('test'); setErr('')
    try { setTest(await api.post<LogRuleTest>('/api/logrules/test', f)) } catch (e) { setErr((e as Error).message) } finally { setBusy('') }
  }
  async function save() {
    setBusy('save'); setErr('')
    try {
      if (initial) await api.put(`/api/logrules/${initial.id}`, f)
      else await api.post('/api/logrules', f)
      toast.ok(initial ? 'Kural güncellendi' : 'Kural eklendi, izleme başladı')
      onSaved(); onClose()
    } catch (e) { setErr((e as Error).message) } finally { setBusy('') }
  }

  return (
    <Dialog open={open} onClose={onClose} wide title={initial ? 'Kuralı düzenle' : 'Log kuralı ekle'}
      description="Uygulamanın logunda kalıba uyan satırlar izlenir. Aynı IP belirlenen sürede eşiği aşarsa engellenir."
      footer={<>
        <Button onClick={runTest} loading={busy === 'test'} className="mr-auto"><FlaskConical size={14} /> Mevcut loglarda dene</Button>
        <Button onClick={onClose}>Vazgeç</Button>
        <Button variant="default" onClick={save} loading={busy === 'save'}>{initial ? 'Kaydet' : 'Kuralı ekle'}</Button>
      </>}>
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Kural adı"><Input value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="Giriş denemeleri" maxLength={50} autoFocus={!initial} /></Field>
          <Field label="Kaynak">
            <Select value={f.source} onChange={(e) => set('source', e.target.value as LogRule['source'])}>
              {Object.entries(sourceLabel).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </Select>
          </Field>
        </div>
        {f.source === 'pm2' ? (
          <Field label="PM2 uygulaması">
            <Select value={f.path} onChange={(e) => set('path', e.target.value)}>
              <option value="">Seç</option>
              {pm2.map((n) => <option key={n} value={n}>{n}</option>)}
            </Select>
          </Field>
        ) : (
          <Field label={f.source === 'file' ? 'Log dosyası' : 'Servis adı'} hint={f.source === 'file' ? 'Tam yol' : 'journalctl -u ile okunur'}>
            <Input value={f.path} onChange={(e) => set('path', e.target.value)} className="font-mono" placeholder={f.source === 'file' ? '/var/log/uygulama/app.log' : 'uygulama.service'} />
          </Field>
        )}
        <Field label="Kalıp" hint={<>IP adresinin yerine <span className="font-mono">&lt;IP&gt;</span>, değişen kısımlar için <span className="font-mono">*</span> yaz. Örnek: <span className="font-mono">login failed * ip=&lt;IP&gt;</span></>}>
          <Input value={f.pattern} onChange={(e) => set('pattern', e.target.value)} className="font-mono" placeholder="login failed ip=<IP>" />
        </Field>
        <label className="flex items-center gap-2 text-sm cursor-pointer">
          <input type="checkbox" checked={f.regex} onChange={(e) => set('regex', e.target.checked)} className="accent-[var(--brand)]" />
          Kalıp düzenli ifade (regex)
        </label>
        <div className="grid gap-3 sm:grid-cols-4">
          <Field label="Eşik"><Input type="number" min={1} value={f.threshold} onChange={(e) => set('threshold', Number(e.target.value))} /></Field>
          <Field label="Süre (dakika)"><Input type="number" min={1} value={f.window} onChange={(e) => set('window', Number(e.target.value))} /></Field>
          <Field label="İşlem">
            <Select value={f.action} onChange={(e) => set('action', e.target.value as LogRule['action'])}>
              {Object.entries(actionLabel).filter(([k]) => cf || k !== 'cloudflare').map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </Select>
          </Field>
          <Field label="Engelleme süresi">
            <Select value={f.banHours} onChange={(e) => set('banHours', Number(e.target.value))} disabled={f.action === 'notify'}>
              {hourOptions.map((o) => <option key={o.v} value={o.v}>{o.l}</option>)}
            </Select>
          </Field>
        </div>
        <p className="text-xs text-muted-foreground">
          {f.threshold} eşleşme / {f.window} dakika. {f.action === 'auto' && (cf ? 'Otomatik: hem Cloudflare\'de hem sunucu firewall\'unda engellenir.' : 'Otomatik: sunucu firewall\'unda engellenir. Uygulama Cloudflare Tünel arkasındaysa bu engel işe yaramaz; Cloudflare token ekle.')}
        </p>
        <label className="flex items-center justify-between gap-4 text-sm font-medium cursor-pointer">
          Kural açık
          <Switch checked={f.enabled} onChange={(v) => set('enabled', v)} label="Kural açık" />
        </label>
        {err && <ErrorBox>{err}</ErrorBox>}
        {test && (
          <div className="rounded-lg border p-4 text-sm">
            <div className="font-medium">Son {test.lines} satırda {test.matches} eşleşme · {Object.keys(test.ips).length} farklı IP{test.wouldBan > 0 && <> · <span className="text-destructive">{test.wouldBan} IP eşiği aşıyor</span></>}</div>
            {test.matches > 0 ? (
              <>
                <div className="mt-2 flex flex-wrap gap-1.5">{Object.entries(test.ips).sort((a, b) => b[1] - a[1]).map(([ip, n]) => <Tag key={ip} tone={n >= f.threshold ? 'kirmizi' : 'soluk'}>{ip} · {n}</Tag>)}</div>
                <ul className="mt-2 space-y-0.5">{test.samples?.map((l, i) => <li key={i} className="truncate font-mono text-xs text-muted-foreground">{l}</li>)}</ul>
              </>
            ) : (
              <>
                <div className="mt-1 text-muted-foreground">Kalıp hiçbir satırla eşleşmedi. Logun son satırları:</div>
                <ul className="mt-2 space-y-0.5">{test.unmatched?.map((l, i) => <li key={i} className="truncate font-mono text-xs text-muted-foreground">{l}</li>)}</ul>
              </>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}

export function LogRules() {
  const { data, error, reload } = usePoll<Resp>('/api/logrules', 15000)
  const [form, setForm] = useState<{ open: boolean; r: LogRule | null }>({ open: false, r: null })

  async function toggle(r: LogRule) {
    try { await api.put(`/api/logrules/${r.id}`, { ...r, enabled: !r.enabled }); reload() } catch (e) { toast.err((e as Error).message) }
  }
  async function remove(r: LogRule) {
    try { await api.del(`/api/logrules/${r.id}`); toast.ok('Kural silindi'); reload() } catch (e) { toast.err((e as Error).message) }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />

  return (
    <div className="space-y-5">
      <PageHeader title="Log kuralları"
        desc="Uygulamalarının loglarındaki şüpheli satırlara göre IP engelle. Örneğin giriş ekranına çok sayıda hatalı şifre deneyen adresler."
        action={<Button variant="default" onClick={() => setForm({ open: true, r: null })}><Plus size={14} /> Kural ekle</Button>} />

      {!data.cloudflare && data.rules.some((r) => r.action === 'auto' || r.action === 'firewall') && (
        <Alert tone="amber" title="Cloudflare ayarlanmamış">Uygulamaların Cloudflare Tünel arkasındaysa sunucu firewall'u bu IP'leri durduramaz. Ayarlar → Cloudflare bölümünden token ekle.</Alert>
      )}

      {data.rules.length === 0 ? (
        <Card><CardContent className="flex flex-col items-center gap-3 py-14 text-center">
          <ScrollText className="text-muted-foreground" />
          <div className="font-medium">Henüz kural yok</div>
          <p className="max-w-md text-sm text-muted-foreground">Uygulaman başarısız girişleri IP adresiyle logluyorsa, bir kural ekleyip eşiği aşan IP'leri otomatik engelleyebilirsin. Kaydetmeden önce mevcut loglarda deneyebilirsin.</p>
          <Button variant="default" onClick={() => setForm({ open: true, r: null })}><Plus size={14} /> Kural ekle</Button>
        </CardContent></Card>
      ) : (
        <Card>
          <CardContent className="px-0 py-0">
            <ul>
              {data.rules.map((r, i) => {
                const st = data.status[r.id]
                return (
                  <li key={r.id} className={`flex flex-wrap items-center gap-4 px-6 py-3.5 ${i ? 'border-t' : ''}`}>
                    <div className="min-w-0 flex-1 basis-64">
                      <div className="flex items-center gap-2 font-medium">{r.name}{!r.enabled && <Tag>Kapalı</Tag>}</div>
                      <div className="truncate text-xs text-muted-foreground">{sourceLabel[r.source]}: <span className="font-mono">{r.path}</span> · <span className="font-mono">{r.pattern}</span></div>
                    </div>
                    <div className="text-xs text-muted-foreground">{r.threshold} / {r.window} dk · {actionLabel[r.action]}{r.action !== 'notify' && ` · ${hourOptions.find((o) => o.v === r.banHours)?.l ?? r.banHours + ' saat'}`}</div>
                    <div className="w-32 text-xs">{r.enabled ? <Tag tone={st === 'izleniyor' ? 'yesil' : 'amber'}>{st ?? 'bekliyor'}</Tag> : null}</div>
                    <div className="flex items-center gap-1">
                      <Switch checked={r.enabled} onChange={() => toggle(r)} label={`${r.name} açık`} />
                      <Button size="icon" variant="ghost" onClick={() => setForm({ open: true, r })} aria-label="Düzenle"><Pencil size={14} /></Button>
                      <ConfirmButton size="icon" variant="ghost" onConfirm={() => remove(r)} confirmText="?" aria-label="Sil"><Trash2 size={14} /></ConfirmButton>
                    </div>
                  </li>
                )
              })}
            </ul>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader title="Son işlemler" description="Panel yeniden başlayana kadar tutulur; kalıcı kayıt İşlem kayıtları ve Bildirimler'de." />
        <CardContent className="px-0 pb-0">
          {data.hits.length === 0 ? <Empty>Henüz eşiği aşan IP olmadı.</Empty> : (
            <ul>
              {data.hits.map((h, i) => (
                <li key={i} className="flex flex-wrap items-center gap-3 border-t px-6 py-2.5 text-sm">
                  <ShieldBan size={14} className={h.blocked ? 'text-success' : 'text-muted-foreground'} />
                  <span className="font-mono text-xs">{h.ip}</span>
                  <span className="text-muted-foreground">{h.rule} · {h.count} eşleşme</span>
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">{h.result}</span>
                  <span className="text-xs text-muted-foreground">{ago(h.ts)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <RuleForm open={form.open} initial={form.r} pm2={data.pm2 ?? []} cf={data.cloudflare} onClose={() => setForm({ open: false, r: null })} onSaved={reload} />
    </div>
  )
}
