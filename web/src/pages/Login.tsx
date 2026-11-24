import { useRef, useState } from 'react'
import { ShieldCheck } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { Button, ErrorBox, Field, Input, Card } from '../components/ui'
import { Logo } from '../components/Layout'

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

  return (
    <div className="min-h-dvh grid place-items-center px-4 bg-sidebar">
      <div className="w-full max-w-sm sayfa-gir">
        <div className="flex flex-col items-center text-center mb-6">
          <Logo size={44} />
          <h1 className="text-xl font-semibold tracking-tight mt-4">Kokpit'e giriş yap</h1>
          <p className="text-sm text-muted-foreground mt-1">Şifren ve authenticator kodunla devam et</p>
        </div>
        <Card className="p-6">
          <form onSubmit={submit} className="grid gap-4">
            <Field label="Kullanıcı adı" htmlFor="u">
              <Input id="u" autoComplete="username" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} required />
            </Field>
            <Field label="Şifre" htmlFor="p">
              <Input id="p" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>
            <Field label="Doğrulama kodu" htmlFor="c">
              <Input
                id="c"
                ref={codeRef}
                className="h-11 font-mono tracking-[0.5em] text-center text-lg"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                placeholder="000000"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
                required
              />
            </Field>
            {err && <ErrorBox>{err}</ErrorBox>}
            <Button type="submit" variant="default" loading={busy} className="w-full mt-1">Giriş yap</Button>
          </form>
        </Card>
        <p className="text-xs text-muted-foreground mt-4 flex items-center justify-center gap-1.5">
          <ShieldCheck size={13} /> 5 hatalı denemede IP 15 dakika kilitlenir.
        </p>
      </div>
    </div>
  )
}
