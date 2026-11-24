import { useCallback, useEffect, useState } from 'react'
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
          <Route path="/projeler" element={<Projects />} />
          <Route path="/siteler" element={<Sites />} />
          <Route path="/guvenlik" element={<Security />} />
          <Route path="/bildirimler" element={<Alerts />} />
          <Route path="/kayitlar" element={<Audit />} />
          <Route path="/guncellemeler" element={<Updates />} />
          <Route path="*" element={<Overview />} />
        </Routes>
      </Layout>
      <Toaster />
    </BrowserRouter>
  )
}
