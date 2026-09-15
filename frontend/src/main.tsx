import './assets/main.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { DesktopWindow } from './components/DesktopWindow'
import { applyTheme, getTheme } from './theme'

applyTheme(getTheme())

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
    {view === 'desktop' ? <DesktopWindow initialNodeId={params.get('node')} /> : <App />}
  </StrictMode>
)
