import { useState, type ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { Gauge, Cpu, Boxes, Globe, ShieldCheck, Bell, ScrollText, LogOut, Menu, X, ArrowUpCircle } from 'lucide-react'
import { api, type Me } from '../lib/api'

const nav = [
  { to: '/', label: 'Genel bakış', icon: Gauge, end: true },
  { to: '/sistem', label: 'Sistem', icon: Cpu },
  { to: '/projeler', label: 'Projeler', icon: Boxes },
  { to: '/siteler', label: 'Siteler', icon: Globe },
  { to: '/guvenlik', label: 'Güvenlik', icon: ShieldCheck },
  { to: '/bildirimler', label: 'Bildirimler', icon: Bell },
  { to: '/kayitlar', label: 'İşlem kayıtları', icon: ScrollText },
]

export function Layout({ me, children, onLogout }: { me: Me; children: ReactNode; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const side = (
    <nav className="flex flex-col h-full">
      <div className="px-4 py-4 flex items-center gap-2.5">
        <img src="/favicon.svg" alt="" className="size-7" />
        <div className="leading-tight min-w-0">
          <div className="font-semibold truncate">{me.hostname || 'Sunucu'}</div>
          <div className="text-xs text-soluk flex items-center gap-1.5">
            <span className="size-1.5 rounded-full bg-yesil nabiz" /> Bağlı
          </div>
        </div>
      </div>
      <ul className="px-2 flex-1 space-y-0.5">
        {nav.map(({ to, label, icon: Icon, end }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={end}
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-2.5 h-9 px-3 rounded-md text-sm transition-colors ${
                  isActive ? 'bg-panel2 text-metin shadow-[inset_2px_0_0_var(--color-amber)]' : 'text-soluk hover:text-metin hover:bg-panel2/60'
                }`
              }
            >
              <Icon size={16} strokeWidth={1.8} /> {label}
            </NavLink>
          </li>
        ))}
      </ul>
      <div className="px-2 pb-2">
        <NavLink
          to="/guncellemeler"
          onClick={() => setOpen(false)}
          className={({ isActive }) =>
            `flex items-center gap-2.5 h-9 px-3 rounded-md text-sm transition-colors ${
              isActive ? 'bg-panel2 text-metin' : me.updateAvailable ? 'text-amber hover:bg-panel2/60' : 'text-soluk hover:text-metin hover:bg-panel2/60'
            }`
          }
        >
          <ArrowUpCircle size={16} strokeWidth={1.8} />
          {me.updateAvailable ? `${me.latestVersion} yayında` : `Sürüm ${me.version}`}
        </NavLink>
      </div>
      <div className="p-3 border-t border-cizgi text-sm">
        <div className="px-1 mb-2">
          <div className="truncate">{me.username}</div>
          <div className="text-xs text-soluk truncate">{me.ip}</div>
        </div>
        <button
          onClick={async () => { try { await api.post('/api/auth/logout') } finally { onLogout() } }}
          className="flex items-center gap-2 w-full h-8 px-2 rounded-md text-soluk hover:text-metin hover:bg-panel2 cursor-pointer"
        >
          <LogOut size={15} /> Çıkış yap
        </button>
      </div>
    </nav>
  )
  return (
    <div className="min-h-dvh md:grid md:grid-cols-[220px_1fr]">
      <aside className="hidden md:block sticky top-0 h-dvh border-r border-cizgi bg-gece">{side}</aside>
      <div className="md:hidden flex items-center justify-between px-4 h-12 border-b border-cizgi sticky top-0 bg-gece z-30">
        <span className="font-semibold">{me.hostname}</span>
        <button onClick={() => setOpen(true)} aria-label="Menüyü aç" className="p-1.5 cursor-pointer"><Menu size={20} /></button>
      </div>
      {open && (
        <div className="md:hidden fixed inset-0 z-40 bg-gece/70" onClick={() => setOpen(false)}>
          <aside className="w-64 h-full bg-gece border-r border-cizgi" onClick={(e) => e.stopPropagation()}>
            <button onClick={() => setOpen(false)} aria-label="Menüyü kapat" className="absolute top-3 left-56 p-1 cursor-pointer"><X size={18} /></button>
            {side}
          </aside>
        </div>
      )}
      <main className="px-4 md:px-8 py-6 max-w-[1400px] w-full">{children}</main>
    </div>
  )
}
