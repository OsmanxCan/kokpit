import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { applyTheme, getTheme } from './lib/theme'

applyTheme(getTheme())
// "Sistem" teması seçiliyse işletim sistemi teması değişince uygula
window.matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', () => applyTheme(getTheme()))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
