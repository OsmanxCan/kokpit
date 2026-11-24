import { api, type AuditEntry } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { dateTime, ago } from '../lib/format'
import { Panel, PageHeader, Loading, ConfirmButton, Tag, Empty, ErrorBox, toast } from '../components/ui'

type Sess = { id: string; username: string; ip: string; ua: string; created: number; lastSeen: number; current: boolean }

function browser(ua: string) {
  const b = /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Tarayıcı'
  const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Windows/.test(ua) ? 'Windows' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : ''
  return `${b}${os ? ' · ' + os : ''}`
}

export function Audit() {
  const log = usePoll<AuditEntry[]>('/api/audit', 30000)
  const sess = usePoll<Sess[]>('/api/auth/sessions', 30000)

  async function killOthers() {
    try {
      const r = await api.post<{ closed: number }>('/api/auth/sessions/kill-others')
      toast.ok(`${r.closed} oturum kapatıldı`)
      sess.reload()
    } catch (e) {
      toast.err((e as Error).message)
    }
  }

  if (log.error && !log.data) return <ErrorBox>{log.error}</ErrorBox>
  if (!log.data) return <Loading />

  return (
    <div className="space-y-5">
      <PageHeader title="İşlem kayıtları" desc="Panelde yapılan her giriş ve işlem burada saklanır: kim, ne zaman, hangi IP'den, ne yaptı." />

      <Panel
        title="Açık oturumlar"
        pad={false}
        action={(sess.data?.length ?? 0) > 1 && <ConfirmButton onConfirm={killOthers} variant="tehlike" confirmText="Hepsini kapat?">Diğer oturumları kapat</ConfirmButton>}
      >
        <ul className="border-t border-cizgi">
          {(sess.data ?? []).map((s) => (
            <li key={s.id} className="px-4 py-2.5 border-b border-cizgi/50 last:border-0 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              <span className="font-medium">{s.username}</span>
              <span className="font-mono text-xs">{s.ip}</span>
              <span className="text-soluk">{browser(s.ua)}</span>
              {s.current && <Tag tone="yesil">bu oturum</Tag>}
              <span className="ml-auto text-xs text-soluk">açıldı {dateTime(s.created)} · son işlem {ago(s.lastSeen)}</span>
            </li>
          ))}
        </ul>
      </Panel>

      <Panel title={`Kayıtlar (son ${log.data.length})`} pad={false}>
        {log.data.length === 0 ? <Empty>Henüz kayıt yok.</Empty> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="text-soluk text-left border-y border-cizgi">
                <th className="font-normal px-4 py-2">Zaman</th><th className="font-normal px-2 py-2">Kullanıcı</th>
                <th className="font-normal px-2 py-2">IP</th><th className="font-normal px-2 py-2">İşlem</th>
                <th className="font-normal px-2 py-2">Hedef</th><th className="font-normal px-4 py-2">Sonuç</th>
              </tr></thead>
              <tbody>
                {log.data.map((a) => (
                  <tr key={a.id} className="border-b border-cizgi/50 last:border-0 align-top">
                    <td className="px-4 py-1.5 whitespace-nowrap text-soluk">{dateTime(a.ts)}</td>
                    <td className="px-2 py-1.5">{a.username}</td>
                    <td className="px-2 py-1.5 font-mono text-xs">{a.ip}</td>
                    <td className="px-2 py-1.5">{a.action}</td>
                    <td className="px-2 py-1.5">{a.target}</td>
                    <td className="px-4 py-1.5">
                      <Tag tone={a.ok ? 'yesil' : 'kirmizi'}>{a.ok ? 'başarılı' : 'başarısız'}</Tag>
                      {a.detail && !a.ok && <div className="text-xs text-soluk mt-0.5 max-w-[340px] truncate" title={a.detail}>{a.detail}</div>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  )
}
