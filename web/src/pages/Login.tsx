import { useRef, useState } from 'react'
import { ShieldCheck } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { Button, ErrorBox } from '../components/ui'

export function Login({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const codeRef = useRef<HTMLInputElement>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      await api.post('/api/auth/login', { username, password, code })
      onDone()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Sunucuya ulaşılamadı')
      setCode('')
      codeRef.current?.focus()
    } finally {
      setBusy(false)
    }
  }

  const field = 'w-full h-10 bg-gece border border-cizgi rounded-md px-3 text-[15px] placeholder:text-soluk/70 focus:border-amber outline-none'

  return (
    <div className="min-h-dvh grid place-items-center px-4">
      <form onSubmit={submit} className="w-full max-w-sm">
        <div className="flex items-center gap-3 mb-6">
          <img src="/favicon.svg" alt="" className="size-10" />
          <div>
            <h1 className="text-xl font-semibold">Kokpit</h1>
            <p className="text-soluk text-sm">Devam etmek için giriş yap</p>
          </div>
        </div>
        <div className="bg-panel border border-cizgi rounded-lg p-5 space-y-4">
          <label className="block">
            <span className="text-sm text-soluk">Kullanıcı adı</span>
            <input className={`${field} mt-1`} autoComplete="username" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} required />
          </label>
          <label className="block">
            <span className="text-sm text-soluk">Şifre</span>
            <input className={`${field} mt-1`} type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </label>
          <label className="block">
            <span className="text-sm text-soluk">Doğrulama kodu</span>
            <input
              ref={codeRef}
              className={`${field} mt-1 tracking-[0.4em] text-center text-lg`}
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9 ]{6,7}"
              maxLength={7}
              placeholder="000000"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/[^0-9]/g, ''))}
              required
            />
            <span className="text-xs text-soluk mt-1 block">Authenticator uygulamandaki 6 haneli kod</span>
          </label>
          {err && <ErrorBox>{err}</ErrorBox>}
          <Button type="submit" variant="birincil" loading={busy} className="w-full h-10">Giriş yap</Button>
        </div>
        <p className="text-xs text-soluk mt-4 flex items-center gap-1.5">
          <ShieldCheck size={13} /> 5 hatalı denemede IP 15 dakika kilitlenir ve sahibine bildirim gider.
        </p>
      </form>
    </div>
  )
}
