/**
 * Theme choice: follow the system, or force dark or light. Stored per machine in
 * localStorage (a viewer convenience, not fleet state) and stamped on <html> as
 * `data-theme`, which the stylesheet reads alongside `prefers-color-scheme`.
 */
export type Theme = 'system' | 'dark' | 'light'

const KEY = 'clawhq.theme'

export function getTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'dark' || v === 'light' ? v : 'system'
  } catch {
    return 'system'
  }
}

export function applyTheme(theme: Theme): void {
  const root = document.documentElement
  if (theme === 'system') delete root.dataset.theme
  else root.dataset.theme = theme
  try {
    if (theme === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, theme)
  } catch {
    /* private mode or blocked storage: the choice just does not persist */
  }
}
