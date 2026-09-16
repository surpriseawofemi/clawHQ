import './assets/main.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { DesktopWindow } from './components/DesktopWindow'
import { ErrorBoundary } from './components/ErrorBoundary'
import { api } from './api'
import { applyTheme, getTheme } from './theme'

applyTheme(getTheme())

// Errors outside React's render (event handlers, timers, promises) do not blank the
// window, but they still go to the log so a misbehaving page can be traced.
const report = (kind: string, message: string, stack: string): void => {
  void api.diag.report(kind, message, stack).catch(() => undefined)
}
window.addEventListener('error', (e) => {
  report('uncaught', e.message, (e.error as Error | undefined)?.stack ?? `${e.filename}:${e.lineno}:${e.colno}`)
})
window.addEventListener('unhandledrejection', (e) => {
  const r = e.reason as { message?: string; stack?: string } | string | undefined
  report('unhandled-rejection', typeof r === 'string' ? r : (r?.message ?? String(r)), typeof r === 'object' ? (r?.stack ?? '') : '')
})

// The desktop viewer is a second native window loading the same bundle; the query
// string says which face to show.
const params = new URLSearchParams(window.location.search)
const view = params.get('view')

// The macOS window keeps its traffic lights but drops the title bar, so the
// sidebar header has to leave room for them. Flagged on <html> so CSS can inset
// only on that platform.
if (/Mac|iPhone|iPad/.test(navigator.platform)) {
  document.documentElement.classList.add('is-mac')
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>{view === 'desktop' ? <DesktopWindow initialNodeId={params.get('node')} /> : <App />}</ErrorBoundary>
  </StrictMode>
)
