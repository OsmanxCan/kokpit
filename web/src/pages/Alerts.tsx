import { useState } from 'react'
import { Send } from 'lucide-react'
import { api, type Alert } from '../lib/api'
import { usePoll } from '../lib/hooks'
import { dateTime } from '../lib/format'
import { Panel, PageHeader, Loading, Button, Tag, Empty, ErrorBox, toast } from '../components/ui'
import { plainText } from './Overview'

const tone = (l: string) => (l === 'critical' ? 'kirmizi' : l === 'warning' ? 'amber' : l === 'ok' ? 'yesil' : 'mavi') as 'kirmizi'
const levelName: Record<string, string> = { critical: 'kritik', warning: 'uyarı', info: 'bilgi', ok: 'düzeldi' }

export function Alerts() {
  const { data, error } = usePoll<{ alerts: Alert[]; telegram: boolean }>('/api/alerts', 30000)
  const [busy, setBusy] = useState(false)
  const [level, setLevel] = useState('')

  async function test() {
    setBusy(true)
    try {
      await api.post('/api/alerts/test')
      toast.ok('Test bildirimi Telegram\'a gönderildi')
    } catch (e) {
      toast.err((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (error && !data) return <ErrorBox>{error}</ErrorBox>
  if (!data) return <Loading />
  const list = level ? data.alerts.filter((a) => a.level === level) : data.alerts

  return (
    <div className="space-y-5">
      <PageHeader
        title="Bildirimler"
        desc="Servis çökmesi, yüksek kaynak kullanımı, dolmak üzere olan disk, yeni IP'den SSH girişi ve panele hatalı giriş denemeleri Telegram'a gönderilir."
        action={<Button onClick={test} loading={busy} disabled={!data.telegram}><Send size={14} /> Test bildirimi gönder</Button>}
      />
      {!data.telegram && (
        <div className="border border-amber/40 bg-amber/10 rounded-md px-4 py-3 text-sm">
          <b className="font-medium text-amber">Telegram ayarlı değil.</b> Bildirimler sadece burada listeleniyor.
          Göndermek için <span className="font-mono text-xs">/etc/kokpit/config.env</span> dosyasına <span className="font-mono text-xs">TELEGRAM_TOKEN</span> ve <span className="font-mono text-xs">TELEGRAM_CHAT_ID</span> yaz, sonra paneli yeniden başlat. Adımlar REHBER.md'de.
        </div>
      )}
      <Panel
        pad={false}
        title={`Geçmiş (${list.length})`}
        action={
          <select value={level} onChange={(e) => setLevel(e.target.value)} className="h-8 bg-gece border border-cizgi rounded-md px-2 text-sm">
            <option value="">Tüm seviyeler</option>
            <option value="critical">Kritik</option>
            <option value="warning">Uyarı</option>
            <option value="info">Bilgi</option>
            <option value="ok">Düzeldi</option>
          </select>
        }
      >
        {list.length === 0 ? <Empty>Henüz bildirim yok.</Empty> : (
          <ul className="border-t border-cizgi">
            {list.map((a) => (
              <li key={a.id} className="px-4 py-2.5 border-b border-cizgi/50 last:border-0 flex items-start gap-3 text-sm">
                <Tag tone={tone(a.level)}>{levelName[a.level] ?? a.level}</Tag>
                <div className="flex-1 min-w-0 break-words">{plainText(a.message)}</div>
                <div className="text-xs text-soluk whitespace-nowrap text-right">
                  {dateTime(a.ts)}
                  <div>{a.sent ? 'gönderildi' : 'gönderilmedi'}</div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  )
}
