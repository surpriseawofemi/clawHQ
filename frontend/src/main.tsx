import './assets/main.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'

// The macOS window keeps its traffic lights but drops the title bar, so the
// sidebar header has to leave room for them. Flagged on <html> so CSS can inset
// only on that platform.
if (/Mac|iPhone|iPad/.test(navigator.platform)) {
  document.documentElement.classList.add('is-mac')
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>
)
