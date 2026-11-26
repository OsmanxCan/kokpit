import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { api, setUnauthorizedHandler, type Me } from './lib/api'
import { Layout } from './components/Layout'
import { Loading, Toaster } from './components/ui'
import { Login } from './pages/Login'
import { Overview } from './pages/Overview'
import { System } from './pages/System'
import { Projects } from './pages/Projects'
import { Sites } from './pages/Sites'
import { Security } from './pages/Security'
import { Alerts } from './pages/Alerts'
import { Audit } from './pages/Audit'
import { Updates } from './pages/Updates'
import { Settings } from './pages/Settings'
import { Uptime } from './pages/Uptime'
import { Firewall } from './pages/Firewall'
import { Attacks } from './pages/Attacks'
import { Files } from './pages/Files'
import { Deploy } from './pages/Deploy'
import { LogRules } from './pages/LogRules'

// xterm büyük olduğu için terminal sayfası ayrı yüklenir
const Terminal = lazy(() => import('./pages/Terminal').then((m) => ({ default: m.Terminal })))


export default function App() {
  const [me, setMe] = useState<Me | null | undefined>(undefined)

  const check = useCallback(async () => {
    try {
      setMe(await api.get<Me>('/api/auth/me'))
    } catch {
      setMe(null)
    }
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => setMe(null))
    check()
  }, [check])

  if (me === undefined) return <Loading />
  if (me === null) return <><Login onDone={check} /><Toaster /></>

  return (
    <BrowserRouter>
      <Layout me={me} onLogout={() => setMe(null)}>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/sistem" element={<System />} />
          <Route path="/uptime" element={<Uptime />} />
          <Route path="/firewall" element={<Firewall />} />
          <Route path="/saldirilar" element={<Attacks />} />
          <Route path="/log-kurallari" element={<LogRules />} />
          <Route path="/deploy" element={<Deploy />} />
          <Route path="/dosyalar" element={<Files />} />
          <Route path="/terminal" element={<Suspense fallback={<Loading />}><Terminal /></Suspense>} />
          <Route path="/projeler" element={<Projects />} />
          <Route path="/siteler" element={<Sites />} />
          <Route path="/guvenlik" element={<Security />} />
          <Route path="/bildirimler" element={<Alerts />} />
          <Route path="/kayitlar" element={<Audit />} />
          <Route path="/guncellemeler" element={<Updates />} />
          <Route path="/ayarlar" element={<Settings />} />
          <Route path="*" element={<Overview />} />
        </Routes>
      </Layout>
      <Toaster />
    </BrowserRouter>
  )
}
