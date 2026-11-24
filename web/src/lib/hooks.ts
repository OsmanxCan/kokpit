import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api'

// usePoll: veriyi çeker ve belirli aralıkla yeniler. Sekme gizliyken yenilemez.
export function usePoll<T>(url: string, intervalMs = 0) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const alive = useRef(true)

  const load = useCallback(async () => {
    try {
      const d = await api.get<T>(url)
      if (alive.current) {
        setData(d)
        setError(null)
      }
    } catch (e) {
      if (alive.current) setError((e as Error).message)
    } finally {
      if (alive.current) setLoading(false)
    }
  }, [url])

  useEffect(() => {
    alive.current = true
    setLoading(true)
    load()
    if (!intervalMs) return () => { alive.current = false }
    const t = window.setInterval(() => { if (!document.hidden) load() }, intervalMs)
    return () => {
      alive.current = false
      window.clearInterval(t)
    }
  }, [load, intervalMs])

  return { data, error, loading, reload: load, setData }
}

// useStream: SSE (EventSource) akışını dinler.
export function useStream<T>(url: string | null, onMessage: (v: T) => void) {
  const cb = useRef(onMessage)
  cb.current = onMessage
  const [connected, setConnected] = useState(false)
  useEffect(() => {
    if (!url) return
    const es = new EventSource(url, { withCredentials: true })
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (e) => {
      try { cb.current(JSON.parse(e.data)) } catch { /* yoksay */ }
    }
    return () => es.close()
  }, [url])
  return connected
}
