// shadcn/ui "dashboard-01" düzeni: inset kenar çubuğu + yuvarlatılmış içerik çerçevesi.
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Cpu, Boxes, Globe, ShieldCheck, Bell, ScrollText, LogOut, PanelLeft, X,
  Settings, ArrowUpCircle, Sun, Moon, Monitor, EllipsisVertical, UserRound, Github,
} from 'lucide-react'
import { api, type Me } from '../lib/api'
import { getTheme, setTheme, type Theme } from '../lib/theme'
import { cn, Menu, MenuItem } from './ui'

const groups = [
  { label: 'İzleme', items: [
    { to: '/', label: 'Genel bakış', icon: LayoutDashboard, end: true },
    { to: '/sistem', label: 'Sistem', icon: Cpu },
  ] },
  { label: 'Yönetim', items: [
    { to: '/projeler', label: 'Projeler', icon: Boxes },
    { to: '/siteler', label: 'Siteler', icon: Globe },
  ] },
  { label: 'Güvenlik', items: [
    { to: '/guvenlik', label: 'Güvenlik', icon: ShieldCheck },
    { to: '/bildirimler', label: 'Bildirimler', icon: Bell },
    { to: '/kayitlar', label: 'İşlem kayıtları', icon: ScrollText },
  ] },
]

const titles: Record<string, string> = {
  '/': 'Genel bakış', '/sistem': 'Sistem', '/projeler': 'Projeler', '/siteler': 'Siteler', '/guvenlik': 'Güvenlik',
  '/bildirimler': 'Bildirimler', '/kayitlar': 'İşlem kayıtları', '/ayarlar': 'Ayarlar', '/guncellemeler': 'Güncellemeler',
}

const SIDEBAR_KEY = 'kokpit-kenar-cubugu'

export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden className="shrink-0">
      <rect width="32" height="32" rx="8" className="fill-foreground" />
      <circle cx="16" cy="16" r="8.5" fill="none" stroke="var(--brand)" strokeWidth="2.6" strokeDasharray="40 14" strokeLinecap="round" transform="rotate(-90 16 16)" />
      <circle cx="16" cy="16" r="2.8" className="fill-background" />
    </svg>
  )
}

function NavItem({ to, label, icon: Icon, end, onClick, badge }: { to: string; label: string; icon: typeof Cpu; end?: boolean; onClick: () => void; badge?: ReactNode }) {
  return (
    <NavLink
      to={to}
      end={end}
      onClick={onClick}
      className={({ isActive }) => cn(
        'flex h-8 items-center gap-2 rounded-md px-2 text-sm outline-none transition-[background-color,color] [&_svg]:size-4 [&_svg]:shrink-0',
        isActive ? 'bg-sidebar-accent font-medium text-foreground' : 'text-foreground/80 hover:bg-sidebar-accent hover:text-foreground',
      )}
    >
      <Icon strokeWidth={1.75} />
      <span className="truncate">{label}</span>
      {badge && <span className="ml-auto">{badge}</span>}
    </NavLink>
  )
}

function ThemeToggle() {
  const [t, setT] = useState<Theme>(getTheme())
  const next: Record<Theme, Theme> = { dark: 'light', light: 'system', system: 'dark' }
  const Icon = { dark: Moon, light: Sun, system: Monitor }[t]
  const label = { dark: 'Koyu tema', light: 'Açık tema', system: 'Sistem teması' }[t]
  return (
    <button
      onClick={() => { const n = next[t]; setTheme(n); setT(n) }}
      title={`${label} (değiştirmek için tıkla)`}
      aria-label={label}
      className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground cursor-pointer"
    >
      <Icon size={16} />
    </button>
  )
}

function readCollapsed() {
  try { return localStorage.getItem(SIDEBAR_KEY) === 'kapali' } catch { return false }
}

export function Layout({ me, children, onLogout }: { me: Me; children: ReactNode; onLogout: () => void }) {
  const [mobileOpen, setMobileOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const loc = useLocation()
  const nav = useNavigate()
  const close = () => setMobileOpen(false)

  const toggle = useCallback(() => {
    if (window.matchMedia('(max-width: 767px)').matches) return setMobileOpen((o) => !o)
    setCollapsed((c) => {
      try { localStorage.setItem(SIDEBAR_KEY, c ? 'acik' : 'kapali') } catch { /* yoksay */ }
      return !c
    })
  }, [])

  // shadcn'deki gibi Ctrl/⌘ + B kenar çubuğunu açar/kapatır
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'b' && (e.ctrlKey || e.metaKey) && !(e.target as HTMLElement)?.closest('input,textarea')) {
        e.preventDefault()
        toggle()
      }
    }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [toggle])

  const logout = async () => { try { await api.post('/api/auth/logout') } finally { onLogout() } }

  const sidebar = (
    <div className="flex h-full w-full flex-col">
      <div className="p-2">
        <Link to="/" onClick={close} className="flex items-center gap-2 rounded-md p-1.5 hover:bg-sidebar-accent">
          <Logo size={24} />
          <span className="text-base font-semibold tracking-tight">Kokpit</span>
        </Link>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
        {groups.map((g) => (
          <div key={g.label} className="flex flex-col p-2 py-0">
            <div className="flex h-8 items-center px-2 text-xs font-medium text-foreground/60">{g.label}</div>
            <div className="flex flex-col gap-1">
              {g.items.map((it) => <NavItem key={it.to} {...it} onClick={close} />)}
            </div>
          </div>
        ))}
        <div className="mt-auto flex flex-col gap-1 p-2">
          <NavItem to="/ayarlar" label="Ayarlar" icon={Settings} onClick={close} />
          <NavItem
            to="/guncellemeler"
            label={me.updateAvailable ? `${me.latestVersion} yayında` : `Sürüm ${me.version}`}
            icon={ArrowUpCircle}
            onClick={close}
            badge={me.updateAvailable ? <span className="block size-2 rounded-full bg-brand" /> : undefined}
          />
        </div>
      </div>
      <div className="p-2">
        <Menu
          side="top"
          align="start"
          trigger={(open) => (
            <button onClick={open} className="flex w-full items-center gap-2 rounded-md p-2 text-left hover:bg-sidebar-accent cursor-pointer">
              <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-foreground text-background text-xs font-semibold">{me.username.slice(0, 2).toUpperCase()}</div>
              <div className="grid min-w-0 flex-1 leading-tight">
                <span className="truncate text-sm font-medium">{me.username}</span>
                <span className="truncate text-xs text-muted-foreground">{me.hostname} · {me.ip}</span>
              </div>
              <EllipsisVertical size={16} className="text-muted-foreground" />
            </button>
          )}
        >
          {(c) => (
            <>
              <div className="px-2 py-1.5 text-sm">
                <div className="font-medium">{me.username}</div>
                <div className="text-xs text-muted-foreground">{me.ip}</div>
              </div>
              <div className="-mx-1 my-1 h-px bg-border" />
              <MenuItem onClick={() => { c(); close(); nav('/ayarlar?sekme=hesap') }}><UserRound size={16} /> Hesabım</MenuItem>
              <MenuItem onClick={() => { c(); close(); nav('/ayarlar') }}><Settings size={16} /> Ayarlar</MenuItem>
              <div className="-mx-1 my-1 h-px bg-border" />
              <MenuItem onClick={logout}><LogOut size={16} /> Çıkış yap</MenuItem>
            </>
          )}
        </Menu>
      </div>
    </div>
  )

  return (
    <div className="flex min-h-svh w-full bg-sidebar">
      {/* Masaüstü kenar çubuğu: boşluk tutucu + sabit panel */}
      <div className={cn('relative hidden shrink-0 bg-transparent transition-[width] duration-200 ease-linear md:block', collapsed ? 'w-0' : 'w-64')} />
      <aside aria-hidden={collapsed} className={cn('fixed inset-y-0 z-10 hidden w-64 p-2 transition-[left] duration-200 ease-linear md:flex', collapsed ? '-left-64 invisible' : 'left-0')}>
        {sidebar}
      </aside>

      {/* Mobil kenar çubuğu */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 bg-black/50 md:hidden" onClick={close}>
          <aside className="sayfa-gir relative flex h-full w-72 bg-sidebar p-2" onClick={(e) => e.stopPropagation()}>
            <button onClick={close} aria-label="Menüyü kapat" className="absolute right-3 top-3.5 z-10 p-1 text-muted-foreground cursor-pointer"><X size={18} /></button>
            {sidebar}
          </aside>
        </div>
      )}

      {/* İçerik çerçevesi (SidebarInset) */}
      <main className={cn('relative flex w-full min-w-0 flex-1 flex-col bg-background md:m-2 md:rounded-xl md:shadow-sm', collapsed ? 'md:ml-2' : 'md:ml-0')}>
        <header className="flex h-12 shrink-0 items-center gap-2 border-b">
          <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
            <button onClick={toggle} title="Kenar çubuğunu aç/kapat (Ctrl+B)" aria-label="Kenar çubuğunu aç/kapat"
              className="-ml-1 inline-flex size-7 items-center justify-center rounded-md hover:bg-accent cursor-pointer">
              <PanelLeft size={16} />
            </button>
            <div className="mx-2 h-4 w-px bg-border" />
            <h1 className="truncate text-base font-medium">{titles[loc.pathname] ?? 'Kokpit'}</h1>
            <div className="ml-auto flex items-center gap-2">
              <span className="hidden items-center gap-1.5 text-xs text-muted-foreground sm:inline-flex">
                <span className="nabiz size-1.5 rounded-full bg-success" /> {me.hostname}
              </span>
              {me.repo && <a href={`https://github.com/${me.repo}`} target="_blank" rel="noreferrer"
                className="hidden h-8 items-center gap-1.5 rounded-md px-3 text-sm font-medium hover:bg-accent sm:inline-flex">
                <Github size={15} /> GitHub
              </a>}
              <ThemeToggle />
            </div>
          </div>
        </header>
        <div key={loc.pathname} className="sayfa-gir flex flex-1 flex-col px-4 py-4 md:py-6 lg:px-6">{children}</div>
      </main>
    </div>
  )
}
