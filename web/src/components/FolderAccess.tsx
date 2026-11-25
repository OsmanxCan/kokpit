// Dosya gezgini erişim izinleri: klasör seçici ile izin verme ve izinleri düzenleme.
import { useEffect, useState } from 'react'
import { ChevronRight, Folder, FolderLock, HardDrive, Link2, Eye, Pencil, Trash2, ShieldAlert, Check } from 'lucide-react'
import { api, type FileRoot } from '../lib/api'
import { Button, cn, Dialog, ErrorBox, Field, Input, Loading, Switch, toast } from './ui'

type DirInfo = { name: string; path: string; link?: boolean; forbidden?: string; hidden?: boolean }
type Browse = { path: string; parent: string; dirs: DirInfo[]; files: number; forbidden: string; truncated: boolean }

// Hassas konumlar: izin verilebilir ama kullanıcı uyarılır.
const SENSITIVE = ['/root', '/home', '/etc', '/var/lib', '/opt', '/usr/local']
function sensitive(p: string) {
  return SENSITIVE.some((s) => p === s || p.startsWith(s + '/')) || p.includes('/.ssh') || p.includes('/.gnupg')
}

export function PermRows({ write, del, onWrite, onDelete, disabled }: { write: boolean; del: boolean; onWrite: (v: boolean) => void; onDelete: (v: boolean) => void; disabled?: boolean }) {
  const rows = [
    { icon: Eye, title: 'Okuma', desc: 'Klasörü gezme, dosyaları görüntüleme ve indirme', on: true, set: undefined },
    { icon: Pencil, title: 'Yazma', desc: 'Yükleme, yeni dosya/klasör, düzenleme, yeniden adlandırma ve taşıma', on: write, set: onWrite },
    { icon: Trash2, title: 'Silme', desc: 'Dosya ve klasör silme, yüklerken aynı adlı dosyanın üzerine yazma', on: del, set: onDelete },
  ]
  return (
    <ul className="divide-y rounded-lg border">
      {rows.map((r) => (
        <li key={r.title} className="flex items-start gap-3 px-4 py-3">
          <r.icon size={16} className="mt-0.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{r.title}</div>
            <div className="text-xs text-muted-foreground">{r.desc}</div>
          </div>
          {r.set ? <Switch checked={r.on} onChange={r.set} disabled={disabled} label={r.title} /> : <span className="text-xs text-muted-foreground">her zaman</span>}
        </li>
      ))}
    </ul>
  )
}

export function permLabel(r: FileRoot) {
  if (r.write && r.delete) return 'okuma, yazma, silme'
  if (r.write) return 'okuma, yazma'
  if (r.delete) return 'okuma, silme'
  return 'sadece okuma'
}

// GrantDialog sunucunun klasör ağacında gezip bir klasöre erişim izni verir.
// Seçim aşamasında sadece klasör adları görünür; dosyalar izin verilmeden gösterilmez.
export function GrantDialog({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: (roots: FileRoot[], added: FileRoot) => void }) {
  const [path, setPath] = useState('/')
  const [data, setData] = useState<{ browse: Browse; granted: Record<string, string> } | null>(null)
  const [err, setErr] = useState('')
  const [step, setStep] = useState<'sec' | 'izin'>('sec')
  const [hidden, setHidden] = useState(false)
  const [name, setName] = useState('')
  const [write, setWrite] = useState(true)
  const [del, setDel] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => { if (open) { setStep('sec'); setPath('/'); setErr('') } }, [open])
  useEffect(() => {
    if (!open) return
    setData(null); setErr('')
    api.get<{ browse: Browse; granted: Record<string, string> }>('/api/files/browse?path=' + encodeURIComponent(path))
      .then(setData).catch((e) => setErr(e.message))
  }, [open, path])

  const b = data?.browse
  const crumbs = path.split('/').filter(Boolean).map((p, i, a) => ({ name: p, path: '/' + a.slice(0, i + 1).join('/') }))
  const already = b ? data!.granted[b.path] : undefined

  async function grant() {
    setBusy(true)
    try {
      const roots = await api.post<FileRoot[]>('/api/files/roots', { name: name.trim(), path, write, delete: del })
      const added = roots.find((r) => r.path === path)!
      toast.ok(`${added.name} klasörüne erişim verildi`)
      onDone(roots, added)
      onClose()
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} wide
      title={step === 'sec' ? 'Klasöre erişim ver' : 'İzinleri seç'}
      description={step === 'sec'
        ? 'Sunucudaki klasörlerden birini seç. Burada sadece klasör adları görünür; içindeki dosyalar ancak izin verdikten sonra açılır.'
        : <span className="font-mono">{path}</span>}
      footer={step === 'sec' ? <>
        <Button onClick={onClose}>Vazgeç</Button>
        <Button variant="default" disabled={!b || !!b.forbidden || !!already} onClick={() => { setName(path.split('/').filter(Boolean).pop() || path); setWrite(true); setDel(false); setStep('izin') }}>
          Bu klasörü seç
        </Button>
      </> : <>
        <Button onClick={() => setStep('sec')}>Geri</Button>
        <Button variant="default" onClick={grant} loading={busy}><Check size={14} /> Erişim ver</Button>
      </>}>
      {step === 'sec' ? (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-1 rounded-md border bg-muted/40 px-2 py-1.5 text-sm">
            <button onClick={() => setPath('/')} className="flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-muted cursor-pointer"><HardDrive size={14} /> /</button>
            {crumbs.map((c) => (
              <span key={c.path} className="flex items-center gap-1">
                <ChevronRight size={12} className="text-muted-foreground" />
                <button onClick={() => setPath(c.path)} className="rounded px-1.5 py-0.5 hover:bg-muted cursor-pointer">{c.name}</button>
              </span>
            ))}
          </div>
          {err && <ErrorBox>{err}</ErrorBox>}
          {!b ? (!err && <Loading />) : (
            <>
              {b.forbidden && <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-sm text-muted-foreground">{b.forbidden}</div>}
              {already && <div className="rounded-md border border-success/30 bg-success/5 px-3 py-2 text-sm text-muted-foreground">Bu klasöre zaten erişim verilmiş: <b className="text-foreground">{already}</b></div>}
              <ul className="h-72 overflow-y-auto rounded-lg border">
                {b.parent && (
                  <li><button onClick={() => setPath(b.parent)} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-muted-foreground hover:bg-muted/60 cursor-pointer"><Folder size={16} /> ..</button></li>
                )}
                {b.dirs.filter((d) => hidden || !d.hidden).map((d) => (
                  <li key={d.name}>
                    <button disabled={!!d.forbidden} onClick={() => setPath(d.path)} title={d.forbidden}
                      className={cn('flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted/60 cursor-pointer disabled:cursor-not-allowed disabled:opacity-40', d.hidden && 'text-muted-foreground')}>
                      {data!.granted[d.path] ? <FolderLock size={16} className="shrink-0 text-success" /> : <Folder size={16} className="shrink-0 text-brand" />}
                      <span className="truncate">{d.name}</span>
                      {d.link && <Link2 size={12} className="text-muted-foreground" />}
                      {data!.granted[d.path] && <span className="ml-auto text-xs text-success">erişim var</span>}
                      {!data!.granted[d.path] && <ChevronRight size={14} className="ml-auto text-muted-foreground" />}
                    </button>
                  </li>
                ))}
                {b.dirs.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">Alt klasör yok</li>}
              </ul>
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{b.dirs.length} klasör{b.files ? `, ${b.files} dosya (adları gizli)` : ''}{b.truncated ? ' · ilk 2000 gösteriliyor' : ''}</span>
                <label className="flex items-center gap-2 cursor-pointer"><input type="checkbox" checked={hidden} onChange={(e) => setHidden(e.target.checked)} className="accent-[var(--brand)]" /> Gizli klasörleri göster</label>
              </div>
            </>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          {sensitive(path) && (
            <div className="flex gap-2 rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-sm text-muted-foreground">
              <ShieldAlert size={16} className="mt-0.5 shrink-0 text-warning" />
              <span>Bu klasör sistem veya kullanıcı ayarları, anahtarlar ya da şifreler içerebilir. Gerekmiyorsa sadece okuma izni ver.</span>
            </div>
          )}
          <Field label="Panelde görünecek ad"><Input value={name} onChange={(e) => setName(e.target.value)} maxLength={40} /></Field>
          <PermRows write={write} del={del} onWrite={setWrite} onDelete={setDel} />
        </div>
      )}
    </Dialog>
  )
}

// RootDialog var olan bir izni düzenler veya kaldırır.
export function RootDialog({ root, onClose, onDone }: { root: FileRoot | null; onClose: () => void; onDone: (roots: FileRoot[]) => void }) {
  const [name, setName] = useState('')
  const [write, setWrite] = useState(false)
  const [del, setDel] = useState(false)
  const [busy, setBusy] = useState('')
  useEffect(() => { if (root) { setName(root.name); setWrite(root.write); setDel(root.delete) } }, [root])
  if (!root) return null

  async function save() {
    setBusy('save')
    try {
      onDone(await api.put<FileRoot[]>('/api/files/roots/' + encodeURIComponent(root!.name), { name: name.trim(), write, delete: del }))
      toast.ok('İzinler güncellendi')
      onClose()
    } catch (e) { toast.err((e as Error).message) } finally { setBusy('') }
  }
  async function revoke() {
    if (!window.confirm(`${root!.path} klasörüne erişim kaldırılsın mı? Klasör silinmez, sadece panelden görünmez olur.`)) return
    setBusy('revoke')
    try {
      onDone(await api.del<FileRoot[]>('/api/files/roots/' + encodeURIComponent(root!.name)))
      toast.ok('Erişim kaldırıldı')
      onClose()
    } catch (e) { toast.err((e as Error).message) } finally { setBusy('') }
  }

  return (
    <Dialog open onClose={onClose} title="Klasör izinleri" description={<span className="font-mono">{root.path}</span>}
      footer={<>
        <Button variant="ghost" className="mr-auto text-destructive" onClick={revoke} loading={busy === 'revoke'}><Trash2 size={14} /> Erişimi kaldır</Button>
        <Button onClick={onClose}>Vazgeç</Button>
        <Button variant="default" onClick={save} loading={busy === 'save'}>Kaydet</Button>
      </>}>
      <div className="space-y-4">
        <Field label="Panelde görünecek ad"><Input value={name} onChange={(e) => setName(e.target.value)} maxLength={40} /></Field>
        <PermRows write={write} del={del} onWrite={setWrite} onDelete={setDel} />
      </div>
    </Dialog>
  )
}
