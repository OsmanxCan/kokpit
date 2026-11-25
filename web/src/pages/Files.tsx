import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  ChevronRight, Download, EllipsisVertical, File, FileArchive, FileCode2, FileImage, FileText, FileVideo, FileAudio, Folder,
  FolderPlus, FilePlus, HardDrive, Link2, Lock, Pencil, RefreshCw, Trash2, Upload, X, FolderInput, CheckCircle2, AlertCircle,
  LoaderCircle, FolderLock,
} from 'lucide-react'
import { GrantDialog, RootDialog, permLabel } from '../components/FolderAccess'
import { api, type FileEntry, type FileListing, type FileRoot, type TextFileT } from '../lib/api'
import { bytes, dateTime } from '../lib/format'
import {
  Button, Card, cn, ConfirmButton, Dialog, Empty, ErrorBox, Field, Input, Loading, Menu, MenuItem, PageHeader, Select, toast,
} from '../components/ui'

const CHUNK = 50 * 1024 * 1024 // Cloudflare tek istekte en fazla 100 MB kabul eder

const join = (dir: string, name: string) => (dir === '/' ? '' : dir) + '/' + name
const q = (o: Record<string, string>) => new URLSearchParams(o).toString()

const ext = (n: string) => n.slice(n.lastIndexOf('.') + 1).toLowerCase()
const IMG = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'ico']
const VID = ['mp4', 'webm']
const AUD = ['mp3', 'ogg', 'wav']
const ARC = ['zip', 'gz', 'tgz', 'xz', 'bz2', '7z', 'rar', 'zst', 'tar']
const CODE = ['js', 'ts', 'tsx', 'jsx', 'json', 'go', 'py', 'sh', 'php', 'css', 'html', 'yml', 'yaml', 'toml', 'conf', 'env', 'sql', 'rs', 'c', 'h', 'cpp', 'java', 'xml', 'ini', 'service']

function kind(e: FileEntry) {
  if (e.dir) return 'dir'
  const x = ext(e.name)
  if (IMG.includes(x)) return 'img'
  if (VID.includes(x)) return 'vid'
  if (AUD.includes(x)) return 'aud'
  if (x === 'pdf') return 'pdf'
  if (ARC.includes(x)) return 'arc'
  if (CODE.includes(x) || e.name.startsWith('.')) return 'code'
  if (['txt', 'md', 'log', 'csv'].includes(x)) return 'text'
  return 'file'
}

function Icon({ e }: { e: FileEntry }) {
  const k = kind(e)
  const I = { dir: Folder, img: FileImage, vid: FileVideo, aud: FileAudio, pdf: FileText, arc: FileArchive, code: FileCode2, text: FileText, file: File }[k]
  return <I size={16} strokeWidth={1.75} className={cn('shrink-0', k === 'dir' ? 'text-brand fill-brand/15' : 'text-muted-foreground')} />
}

// ---------------------------------------------------------------- yükleme

type Up = { id: string; name: string; size: number; sent: number; status: 'bekliyor' | 'yukleniyor' | 'bitti' | 'hata' | 'iptal'; error?: string; root: string; dir: string; xhr?: XMLHttpRequest; overwrite?: boolean }

const newId = () => Array.from(crypto.getRandomValues(new Uint8Array(10)), (b) => b.toString(16).padStart(2, '0')).join('')

function sendChunk(u: Up, file: Blob, offset: number, onProgress: (n: number) => void): Promise<{ received: number; done: boolean }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    u.xhr = xhr
    const end = Math.min(offset + CHUNK, u.size)
    xhr.open('POST', '/api/files/upload?' + q({ root: u.root, path: u.dir, name: u.name, id: u.id, offset: String(offset), total: String(u.size), overwrite: u.overwrite ? '1' : '0' }))
    xhr.setRequestHeader('X-Panel', '1')
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.upload.onprogress = (e) => onProgress(offset + e.loaded)
    xhr.onload = () => {
      let d: { received?: number; done?: boolean; error?: string } = {}
      try { d = JSON.parse(xhr.responseText) } catch { /* yoksay */ }
      if (xhr.status === 200) resolve({ received: d.received ?? end, done: !!d.done })
      else if (xhr.status === 409 && d.error === 'offset') resolve({ received: d.received ?? 0, done: false })
      else reject(new Error(d.error || `HTTP ${xhr.status}`))
    }
    xhr.onerror = () => reject(new Error('bağlantı hatası'))
    xhr.onabort = () => reject(new Error('iptal'))
    xhr.send(file.slice(offset, end))
  })
}

function useUploader(onFileDone: () => void) {
  const [list, setList] = useState<Up[]>([])
  const files = useRef(new Map<string, File>())
  const busy = useRef(false)
  const listRef = useRef(list)
  listRef.current = list
  const patch = (id: string, p: Partial<Up>) => setList((l) => l.map((u) => (u.id === id ? { ...u, ...p } : u)))

  const pump = useCallback(async () => {
    if (busy.current) return
    const next = listRef.current.find((u) => u.status === 'bekliyor')
    if (!next) return
    busy.current = true
    const file = files.current.get(next.id)!
    patch(next.id, { status: 'yukleniyor' })
    let offset = 0
    let retries = 0
    try {
      for (;;) {
        try {
          const r = await sendChunk(next, file, offset, (n) => patch(next.id, { sent: n }))
          retries = 0
          offset = r.received
          patch(next.id, { sent: offset })
          if (r.done || (offset >= next.size && next.size > 0)) break
          if (next.size === 0) break
        } catch (e) {
          const msg = (e as Error).message
          if (msg === 'iptal' || msg !== 'bağlantı hatası' || ++retries > 5) throw e
          await new Promise((res) => setTimeout(res, 2000 * retries)) // bağlantı koptuysa kaldığı yerden devam et
        }
      }
      patch(next.id, { status: 'bitti', sent: next.size })
      onFileDone()
    } catch (e) {
      const msg = (e as Error).message
      patch(next.id, { status: msg === 'iptal' ? 'iptal' : 'hata', error: msg })
      fetch('/api/files/upload/cancel?' + q({ root: next.root, path: next.dir, id: next.id }), { method: 'POST', headers: { 'X-Panel': '1' } })
    } finally {
      files.current.delete(next.id)
      busy.current = false
      setTimeout(pump, 0)
    }
  }, [onFileDone])

  useEffect(() => { pump() }, [list, pump])

  const add = (fl: File[], root: string, dir: string, overwrite = false) => {
    const ups = fl.map((f) => {
      const id = newId()
      files.current.set(id, f)
      return { id, name: f.name, size: f.size, sent: 0, status: 'bekliyor' as const, root, dir, overwrite }
    })
    setList((l) => [...l, ...ups])
  }
  const cancel = (u: Up) => {
    if (u.status === 'yukleniyor') u.xhr?.abort()
    else if (u.status === 'bekliyor') { files.current.delete(u.id); patch(u.id, { status: 'iptal' }) }
  }
  const clear = () => setList((l) => l.filter((u) => u.status === 'yukleniyor' || u.status === 'bekliyor'))
  return { list, add, cancel, clear }
}

function UploadPanel({ up }: { up: ReturnType<typeof useUploader> }) {
  const [min, setMin] = useState(false)
  if (up.list.length === 0) return null
  const active = up.list.filter((u) => u.status === 'yukleniyor' || u.status === 'bekliyor').length
  const total = up.list.reduce((a, u) => a + u.size, 0)
  const sent = up.list.reduce((a, u) => a + (u.status === 'bitti' ? u.size : u.sent), 0)
  return (
    <div className="fixed bottom-4 right-4 z-40 w-[min(380px,calc(100vw-2rem))] overflow-hidden rounded-xl border bg-popover shadow-lg">
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        {active ? <LoaderCircle size={14} className="animate-spin text-muted-foreground" /> : <CheckCircle2 size={14} className="text-success" />}
        <button className="flex-1 cursor-pointer text-left text-sm font-medium" onClick={() => setMin(!min)}>
          {active ? `${active} dosya yükleniyor · %${total ? Math.floor((sent / total) * 100) : 100}` : 'Yükleme tamamlandı'}
        </button>
        {!active && <Button size="icon" variant="ghost" onClick={up.clear} aria-label="Kapat"><X size={14} /></Button>}
      </div>
      {!min && (
        <ul className="max-h-64 overflow-y-auto">
          {up.list.map((u) => {
            const p = u.size ? (u.sent / u.size) * 100 : 100
            return (
              <li key={u.id} className="px-4 py-2 text-sm">
                <div className="flex items-center gap-2">
                  {u.status === 'bitti' ? <CheckCircle2 size={14} className="shrink-0 text-success" /> : u.status === 'hata' ? <AlertCircle size={14} className="shrink-0 text-destructive" /> : <File size={14} className="shrink-0 text-muted-foreground" />}
                  <span className="min-w-0 flex-1 truncate">{u.name}</span>
                  <span className="text-xs text-muted-foreground tabular-nums">{u.status === 'yukleniyor' ? `${bytes(u.sent)} / ${bytes(u.size)}` : u.status === 'bekliyor' ? 'sırada' : u.status === 'iptal' ? 'iptal edildi' : u.status === 'hata' ? '' : bytes(u.size)}</span>
                  {(u.status === 'yukleniyor' || u.status === 'bekliyor') && (
                    <button onClick={() => up.cancel(u)} className="cursor-pointer text-muted-foreground hover:text-foreground" aria-label="İptal"><X size={14} /></button>
                  )}
                </div>
                {u.status === 'yukleniyor' && <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted"><div className="h-full bg-brand transition-[width]" style={{ width: `${p}%` }} /></div>}
                {u.status === 'hata' && <div className="mt-0.5 text-xs text-destructive">{u.error}</div>}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

// ---------------------------------------------------------------- düzenleyici ve önizleme

function Editor({ root, path, readOnly, onClose, onSaved }: { root: string; path: string | null; readOnly: boolean; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<TextFileT | null>(null)
  const [text, setText] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    setF(null); setErr('')
    if (!path) return
    api.get<TextFileT>('/api/files/read?' + q({ root, path })).then((r) => { setF(r); setText(r.content) }).catch((e) => setErr(e.message))
  }, [root, path])
  const dirty = !!f && text !== f.content

  const save = useCallback(async () => {
    if (!f || !path || readOnly) return
    setBusy(true)
    try {
      const r = await api.post<{ mtime: string }>('/api/files/write', { root, path, content: text, mtime: f.mtime })
      setF({ ...f, content: text, mtime: r.mtime })
      toast.ok('Kaydedildi')
      onSaved()
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy(false)
    }
  }, [f, path, readOnly, root, text, onSaved])

  const close = () => { if (!dirty || window.confirm('Kaydedilmemiş değişiklikler kaybolacak. Kapatılsın mı?')) onClose() }
  const name = path?.split('/').pop()

  return (
    <Dialog open={!!path} onClose={close} title={path} bare className="max-w-5xl! h-[calc(100dvh-3rem)]">
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <FileCode2 size={16} className="text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{name}{dirty && <span className="text-muted-foreground"> · değiştirildi</span>}</div>
          <div className="truncate font-mono text-xs text-muted-foreground">{root}:{path}</div>
        </div>
        {readOnly && <span className="flex items-center gap-1 text-xs text-muted-foreground"><Lock size={12} /> yazma izni yok</span>}
        {!readOnly && f && !f.binary && !f.tooBig && <Button size="sm" variant="default" onClick={save} loading={busy} disabled={!dirty}>Kaydet <kbd className="ml-1 hidden text-[10px] opacity-60 sm:inline">Ctrl+S</kbd></Button>}
        <Button size="icon" variant="ghost" onClick={close} aria-label="Kapat"><X size={16} /></Button>
      </div>
      <div className="min-h-0 flex-1">
        {err ? <div className="p-4"><ErrorBox>{err}</ErrorBox></div> : !f ? <Loading /> : f.binary || f.tooBig ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
            <File className="text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{f.tooBig ? `Bu dosya ${bytes(f.size)}; düzenleyicide en fazla 2 MB açılabilir.` : 'Bu bir metin dosyası değil, düzenlenemez.'}</p>
            <Button onClick={() => { window.location.href = '/api/files/download?' + q({ root, path: path! }) }}><Download size={14} /> İndir</Button>
          </div>
        ) : (
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            readOnly={readOnly}
            spellCheck={false}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); save() }
              if (e.key === 'Tab' && !readOnly) {
                e.preventDefault()
                const t = e.currentTarget, s = t.selectionStart, en = t.selectionEnd
                const v = text.slice(0, s) + '\t' + text.slice(en)
                setText(v)
                requestAnimationFrame(() => { t.selectionStart = t.selectionEnd = s + 1 })
              }
            }}
            className="block h-full w-full resize-none bg-transparent p-4 font-mono text-[13px] leading-relaxed outline-none [tab-size:4]"
          />
        )}
      </div>
    </Dialog>
  )
}

function Preview({ root, entry, dir, onClose }: { root: string; entry: FileEntry | null; dir: string; onClose: () => void }) {
  if (!entry) return null
  const path = join(dir, entry.name)
  const src = '/api/files/download?' + q({ root, path, inline: '1' })
  const k = kind(entry)
  return (
    <Dialog open onClose={onClose} title={entry.name} bare className="max-w-5xl!">
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <Icon e={entry} />
        <div className="min-w-0 flex-1 truncate text-sm font-medium">{entry.name} <span className="text-muted-foreground">· {bytes(entry.size)}</span></div>
        <Button size="sm" onClick={() => { window.location.href = '/api/files/download?' + q({ root, path }) }}><Download size={14} /> İndir</Button>
        <Button size="icon" variant="ghost" onClick={onClose} aria-label="Kapat"><X size={16} /></Button>
      </div>
      <div className="flex min-h-0 flex-1 items-center justify-center bg-muted/30 p-4">
        {k === 'img' && <img src={src} alt={entry.name} className="max-h-[75dvh] max-w-full object-contain" />}
        {k === 'vid' && <video src={src} controls className="max-h-[75dvh] max-w-full" />}
        {k === 'aud' && <audio src={src} controls className="w-full" />}
      </div>
    </Dialog>
  )
}

// ---------------------------------------------------------------- küçük diyaloglar

function NameDialog({ open, title, label, initial, submit, onClose, onDone }: {
  open: boolean; title: string; label: string; initial: string; submit: string; onClose: () => void; onDone: (v: string) => Promise<void>
}) {
  const [v, setV] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => { if (open) { setV(initial); setErr('') } }, [open, initial])
  async function go() {
    if (!v.trim()) return
    setBusy(true); setErr('')
    try { await onDone(v.trim()); onClose() } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Dialog open={open} onClose={onClose} title={title}
      footer={<><Button onClick={onClose}>Vazgeç</Button><Button variant="default" onClick={go} loading={busy} disabled={!v.trim()}>{submit}</Button></>}>
      <div className="space-y-3">
        <Field label={label}>
          <Input value={v} onChange={(e) => setV(e.target.value)} autoFocus onKeyDown={(e) => e.key === 'Enter' && go()}
            onFocus={(e) => { const i = e.target.value.lastIndexOf('.'); e.target.setSelectionRange(0, i > 0 ? i : e.target.value.length) }} />
        </Field>
        {err && <ErrorBox>{err}</ErrorBox>}
      </div>
    </Dialog>
  )
}

// ---------------------------------------------------------------- sayfa

export function Files() {
  const [params, setParams] = useSearchParams()
  const [roots, setRoots] = useState<FileRoot[] | null>(null)
  const [rootsErr, setRootsErr] = useState('')
  const rootName = params.get('kok') || roots?.[0]?.name || ''
  const dir = params.get('yol') || '/'
  const root = roots?.find((r) => r.name === rootName)

  const [data, setData] = useState<FileListing | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [edit, setEdit] = useState<string | null>(null)
  const [preview, setPreview] = useState<FileEntry | null>(null)
  const [dialog, setDialog] = useState<null | { t: 'mkdir' | 'newfile' | 'rename' | 'move'; e?: FileEntry; items?: string[] }>(null)
  const [drag, setDrag] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const [grantOpen, setGrantOpen] = useState(false)
  const [rootEdit, setRootEdit] = useState<FileRoot | null>(null)

  useEffect(() => {
    api.get<FileRoot[]>('/api/files/roots').then(setRoots).catch((e) => setRootsErr(e.message))
  }, [])

  const load = useCallback(async () => {
    if (!rootName) return
    setLoading(true)
    try {
      setData(await api.get<FileListing>('/api/files/list?' + q({ root: rootName, path: dir })))
      setErr('')
    } catch (e) {
      setErr((e as Error).message)
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [rootName, dir])

  useEffect(() => { setSel(new Set()); load() }, [load])

  const up = useUploader(load)
  const go = (r: string, p: string) => setParams({ kok: r, yol: p })
  const ro = data ? !data.write : false
  const canDel = !!data?.delete

  const crumbs = useMemo(() => {
    const parts = dir.split('/').filter(Boolean)
    return parts.map((p, i) => ({ name: p, path: '/' + parts.slice(0, i + 1).join('/') }))
  }, [dir])

  function open(e: FileEntry) {
    if (e.outside) return toast.err('Bu bağlantı izin verilen klasörün dışını gösteriyor')
    if (e.dir) return go(rootName, join(dir, e.name))
    const k = kind(e)
    if (['img', 'vid', 'aud'].includes(k)) return setPreview(e)
    if (k === 'arc' || k === 'pdf' || (k === 'file' && e.size > 2 * 1024 * 1024)) return download(e)
    setEdit(join(dir, e.name))
  }

  function download(e: FileEntry) {
    const p = join(dir, e.name)
    window.location.href = e.dir ? '/api/files/zip?' + q({ root: rootName, path: p }) : '/api/files/download?' + q({ root: rootName, path: p })
  }

  function zipSelected() {
    const u = new URLSearchParams({ root: rootName })
    sel.forEach((n) => u.append('path', join(dir, n)))
    window.location.href = '/api/files/zip?' + u.toString()
  }

  async function remove(names: string[]) {
    try {
      await api.post('/api/files/delete', { root: rootName, paths: names.map((n) => join(dir, n)) })
      toast.ok(names.length > 1 ? `${names.length} öğe silindi` : `${names[0]} silindi`)
    } catch (e) {
      toast.err((e as Error).message)
    }
    setSel(new Set())
    load()
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDrag(false)
    if (ro) return toast.err('Bu klasöre yazma izni yok')
    const fl = Array.from(e.dataTransfer.files).filter((f) => f.size > 0 || f.type !== '')
    if (fl.length) startUpload(fl)
  }

  function startUpload(fl: File[]) {
    const existing = new Set(data?.entries.map((x) => x.name))
    const clash = fl.filter((f) => existing.has(f.name))
    const overwrite = clash.length > 0 && window.confirm(`${clash.map((f) => f.name).slice(0, 5).join(', ')}${clash.length > 5 ? '…' : ''} zaten var. Üzerine yazılsın mı?\n\n"İptal" dersen aynı adlı dosyalar atlanır.`)
    const list = overwrite ? fl : fl.filter((f) => !existing.has(f.name))
    if (list.length) up.add(list, rootName, dir, overwrite)
  }

  if (rootsErr) return <ErrorBox>{rootsErr}</ErrorBox>
  if (!roots) return <Loading />
  const access = <>
    <GrantDialog open={grantOpen} onClose={() => setGrantOpen(false)} onDone={(r, added) => { setRoots(r); go(added.name, '/') }} />
    <RootDialog root={rootEdit} onClose={() => setRootEdit(null)} onDone={(r) => {
      setRoots(r)
      if (!r.find((x) => x.name === rootName)) setParams({})
      else if (rootEdit && !r.find((x) => x.name === rootEdit.name)) setParams({})
    }} />
  </>

  if (roots.length === 0) {
    return (
      <div className="space-y-5">
        <PageHeader title="Dosyalar" />
        <Card>
          <div className="flex flex-col items-center gap-3 py-14 text-center">
            <FolderLock className="text-muted-foreground" />
            <div className="font-medium">Henüz hiçbir klasöre erişim verilmedi</div>
            <p className="max-w-md text-sm text-muted-foreground">Dosya gezgini sadece senin izin verdiğin klasörleri açar. Bir klasör seç, okuma, yazma ve silme izinlerini ayrı ayrı belirle.</p>
            <Button variant="default" onClick={() => setGrantOpen(true)}><FolderLock size={14} /> Klasöre erişim ver</Button>
          </div>
        </Card>
        {access}
      </div>
    )
  }

  const entries = data?.entries ?? []
  const allSel = entries.length > 0 && sel.size === entries.length
  const toggle = (n: string) => setSel((s) => { const x = new Set(s); x.has(n) ? x.delete(n) : x.add(n); return x })

  return (
    <div className="space-y-4">
      <PageHeader
        title="Dosyalar"
        desc="Sunucudaki izin verilen klasörlerde gezin, dosya yükle, indir ve düzenle. Dosyaları buraya sürükleyip bırakabilirsin."
        action={
          <div className="flex flex-wrap gap-2">
            <Select value={rootName} onChange={(e) => e.target.value === '+' ? setGrantOpen(true) : go(e.target.value, '/')} className="w-48" aria-label="Klasör">
              {roots.map((r) => <option key={r.name} value={r.name}>{r.name}</option>)}
              <option value="+">+ Klasöre erişim ver…</option>
            </Select>
            <Button onClick={load} loading={loading} aria-label="Yenile"><RefreshCw size={14} /></Button>
            {!ro && <>
              <Menu trigger={(o) => <Button onClick={o}><FolderPlus size={14} /> Yeni</Button>}>
                {(close) => <>
                  <MenuItem onClick={() => { close(); setDialog({ t: 'mkdir' }) }}><FolderPlus size={14} /> Klasör</MenuItem>
                  <MenuItem onClick={() => { close(); setDialog({ t: 'newfile' }) }}><FilePlus size={14} /> Metin dosyası</MenuItem>
                </>}
              </Menu>
              <Button variant="default" onClick={() => fileInput.current?.click()}><Upload size={14} /> Yükle</Button>
            </>}
            <input ref={fileInput} type="file" multiple hidden onChange={(e) => { if (e.target.files?.length) startUpload(Array.from(e.target.files)); e.target.value = '' }} />
          </div>
        }
      />

      <Card className="overflow-hidden">
        {/* yol çubuğu */}
        <div className="flex min-h-12 flex-wrap items-center gap-1 border-b px-4 py-2 text-sm">
          <button onClick={() => go(rootName, '/')} className="flex items-center gap-1.5 rounded-md px-1.5 py-1 font-medium hover:bg-muted cursor-pointer">
            <HardDrive size={14} className="text-muted-foreground" /> {rootName}
          </button>
          {crumbs.map((c) => (
            <span key={c.path} className="flex items-center gap-1">
              <ChevronRight size={14} className="text-muted-foreground" />
              <button onClick={() => go(rootName, c.path)} className="rounded-md px-1.5 py-1 hover:bg-muted cursor-pointer">{c.name}</button>
            </span>
          ))}
          {root && <button onClick={() => setRootEdit(root)} className="ml-2 flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground cursor-pointer" title="İzinleri düzenle">
            {!root.write && <Lock size={12} />} {permLabel(root)}
          </button>}
          <span className="ml-auto truncate font-mono text-xs text-muted-foreground" title={root?.path}>{root?.path}{dir === '/' ? '' : dir}</span>
        </div>

        {/* seçim çubuğu */}
        {sel.size > 0 && (
          <div className="flex flex-wrap items-center gap-2 border-b bg-muted/40 px-4 py-2 text-sm">
            <span className="font-medium">{sel.size} öğe seçili</span>
            <Button size="sm" variant="ghost" onClick={() => setSel(new Set())}>Seçimi kaldır</Button>
            <div className="ml-auto flex gap-2">
              <Button size="sm" onClick={zipSelected}><Download size={14} /> Zip olarak indir</Button>
              {!ro && <Button size="sm" onClick={() => setDialog({ t: 'move', items: [...sel] })}><FolderInput size={14} /> Taşı</Button>}
              {canDel && <ConfirmButton size="sm" onConfirm={() => remove([...sel])} confirmText={`${sel.size} öğe silinsin mi?`}><Trash2 size={14} /> Sil</ConfirmButton>}
            </div>
          </div>
        )}

        <div
          className={cn('relative min-h-64', drag && 'bg-brand/5')}
          onDragOver={(e) => { if (e.dataTransfer.types.includes('Files')) { e.preventDefault(); setDrag(true) } }}
          onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setDrag(false) }}
          onDrop={onDrop}
        >
          {drag && (
            <div className="pointer-events-none absolute inset-2 z-10 flex items-center justify-center rounded-lg border-2 border-dashed border-brand/60 text-sm font-medium">
              <Upload size={16} className="mr-2" /> Bırakınca {dir === '/' ? rootName : crumbs[crumbs.length - 1]?.name} klasörüne yüklenecek
            </div>
          )}
          {err ? <div className="p-4"><ErrorBox>{err}</ErrorBox></div> : !data ? <Loading /> : entries.length === 0 ? (
            <Empty>{ro ? 'Bu klasör boş.' : 'Bu klasör boş. Dosyaları buraya sürükleyip bırakarak yükleyebilirsin.'}</Empty>
          ) : (
            <table className="w-full text-sm sm:table-fixed">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="w-10 min-w-10 py-2 pl-4"><input type="checkbox" checked={allSel} onChange={() => setSel(allSel ? new Set() : new Set(entries.map((e) => e.name)))} aria-label="Tümünü seç" className="accent-[var(--brand)]" /></th>
                  <th className="py-2 font-medium">Ad</th>
                  <th className="hidden w-28 py-2 pr-4 text-right font-medium sm:table-cell">Boyut</th>
                  <th className="hidden w-36 py-2 pr-4 font-medium md:table-cell">Değiştirilme</th>
                  <th className="hidden w-32 py-2 pr-4 font-medium lg:table-cell">İzinler</th>
                  <th className="w-12" />
                </tr>
              </thead>
              <tbody>
                {dir !== '/' && (
                  <tr className="border-b hover:bg-muted/50">
                    <td />
                    <td colSpan={5} className="py-2">
                      <button onClick={() => go(rootName, dir.slice(0, dir.lastIndexOf('/')) || '/')} className="flex items-center gap-2 text-muted-foreground cursor-pointer hover:text-foreground">
                        <Folder size={16} strokeWidth={1.75} /> ..
                      </button>
                    </td>
                  </tr>
                )}
                {entries.map((e) => (
                  <tr key={e.name} className={cn('group border-b last:border-0 hover:bg-muted/50', sel.has(e.name) && 'bg-muted/60')}>
                    <td className="w-10 min-w-10 py-2 pl-4"><input type="checkbox" checked={sel.has(e.name)} onChange={() => toggle(e.name)} aria-label={`${e.name} seç`} className="accent-[var(--brand)]" /></td>
                    <td className="w-full max-w-0 py-2 pr-2 sm:w-auto sm:max-w-none">
                      <button onClick={() => open(e)} className={cn('flex max-w-full items-center gap-2 text-left cursor-pointer', e.outside && 'opacity-60 cursor-not-allowed')}>
                        <Icon e={e} />
                        <span className="truncate group-hover:underline underline-offset-4">{e.name}</span>
                        {e.link && <Link2 size={12} className="shrink-0 text-muted-foreground" aria-label="bağlantı" />}
                      </button>
                    </td>
                    <td className="hidden py-2 pr-4 text-right tabular-nums text-muted-foreground sm:table-cell">{e.dir ? '—' : bytes(e.size)}</td>
                    <td className="hidden whitespace-nowrap py-2 pr-4 text-muted-foreground md:table-cell">{dateTime(e.mtime)}</td>
                    <td className="hidden py-2 pr-4 font-mono text-xs text-muted-foreground lg:table-cell">{e.mode}</td>
                    <td className="py-1 pr-2 text-right">
                      <Menu trigger={(o) => <Button size="icon" variant="ghost" onClick={o} aria-label="İşlemler"><EllipsisVertical size={14} /></Button>}>
                        {(close) => <>
                          {!e.dir && !['arc', 'img', 'vid', 'aud', 'pdf'].includes(kind(e)) && <MenuItem onClick={() => { close(); setEdit(join(dir, e.name)) }}><FileCode2 size={14} /> {ro ? 'Görüntüle' : 'Düzenle'}</MenuItem>}
                          <MenuItem onClick={() => { close(); download(e) }}><Download size={14} /> {e.dir ? 'Zip olarak indir' : 'İndir'}</MenuItem>
                          {!ro && <MenuItem onClick={() => { close(); setDialog({ t: 'rename', e }) }}><Pencil size={14} /> Yeniden adlandır</MenuItem>}
                          {!ro && <MenuItem onClick={() => { close(); setDialog({ t: 'move', items: [e.name] }) }}><FolderInput size={14} /> Taşı</MenuItem>}
                          {canDel && <div className="px-1 pt-1">
                            <ConfirmButton size="sm" variant="ghost" className="w-full justify-start text-destructive" onConfirm={() => { close(); remove([e.name]) }} confirmText={e.dir ? 'Klasör içindekilerle silinsin mi?' : 'Silinsin mi?'}>
                              <Trash2 size={14} /> Sil
                            </ConfirmButton>
                          </div>}
                        </>}
                      </Menu>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
        {data && (
          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-4 py-2.5 text-xs text-muted-foreground">
            <span>{entries.filter((e) => e.dir).length} klasör, {entries.filter((e) => !e.dir).length} dosya · {bytes(entries.reduce((a, e) => a + (e.dir ? 0 : e.size), 0))}</span>
            {data.total > 0 && (
              <span className="flex items-center gap-2">
                Diskte {bytes(data.free)} boş
                <span className="inline-block h-1.5 w-24 overflow-hidden rounded-full bg-muted"><span className="block h-full bg-foreground/60" style={{ width: `${((data.total - data.free) / data.total) * 100}%` }} /></span>
              </span>
            )}
          </div>
        )}
      </Card>

      <Editor root={rootName} path={edit} readOnly={ro} onClose={() => setEdit(null)} onSaved={load} />
      <Preview root={rootName} dir={dir} entry={preview} onClose={() => setPreview(null)} />

      <NameDialog open={dialog?.t === 'mkdir'} title="Yeni klasör" label="Klasör adı" initial="" submit="Oluştur" onClose={() => setDialog(null)}
        onDone={async (v) => { await api.post('/api/files/mkdir', { root: rootName, path: join(dir, v) }); load() }} />
      <NameDialog open={dialog?.t === 'newfile'} title="Yeni metin dosyası" label="Dosya adı" initial="yeni.txt" submit="Oluştur" onClose={() => setDialog(null)}
        onDone={async (v) => { await api.post('/api/files/write', { root: rootName, path: join(dir, v), content: '', create: true }); load(); setEdit(join(dir, v)) }} />
      <NameDialog open={dialog?.t === 'rename'} title="Yeniden adlandır" label="Yeni ad" initial={dialog?.e?.name ?? ''} submit="Kaydet" onClose={() => setDialog(null)}
        onDone={async (v) => { await api.post('/api/files/move', { root: rootName, from: [join(dir, dialog!.e!.name)], name: v }); load() }} />
      <NameDialog open={dialog?.t === 'move'} title={`${dialog?.items?.length ?? 0} öğeyi taşı`} label={`Hedef klasör (${rootName} içinde)`} initial={dir} submit="Taşı" onClose={() => setDialog(null)}
        onDone={async (v) => {
          await api.post('/api/files/move', { root: rootName, from: dialog!.items!.map((n) => join(dir, n)), to: v.startsWith('/') ? v : join(dir, v) })
          setSel(new Set()); load(); toast.ok('Taşındı')
        }} />

      <UploadPanel up={up} />
      {access}
    </div>
  )
}
