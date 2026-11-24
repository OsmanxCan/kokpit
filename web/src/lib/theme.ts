// Açık / koyu tema. Tercih tarayıcıda saklanır; saklanamazsa sistem ayarı kullanılır.
export type Theme = 'light' | 'dark' | 'system'

const KEY = 'kokpit-tema'

export function getTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch { /* depolama kapalı */ }
  return 'dark'
}

export function applyTheme(t: Theme) {
  const dark = t === 'dark' || (t === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
}

export function setTheme(t: Theme) {
  try { localStorage.setItem(KEY, t) } catch { /* yoksay */ }
  applyTheme(t)
}
