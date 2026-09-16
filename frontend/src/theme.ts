/**
 * Theme choice: follow the system, or force dark or light. Stored per machine in
 * localStorage (a viewer convenience, not fleet state) and stamped on <html> as
 * `data-theme`, which the stylesheet reads alongside `prefers-color-scheme`.
 */
export type Theme = 'system' | 'dark' | 'light'

const KEY = 'clawhq.theme'
const EVENT = 'clawhq:theme'

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
  window.dispatchEvent(new CustomEvent(EVENT, { detail: theme }))
}

/** What is actually on screen right now, with "system" resolved. */
export function effectiveTheme(theme: Theme = getTheme()): 'dark' | 'light' {
  if (theme !== 'system') return theme
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/** Flip to the opposite of what is on screen; the choice becomes explicit. */
export function toggleTheme(): Theme {
  const next: Theme = effectiveTheme() === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  return next
}

/** Runs when the theme changes anywhere: the top bar toggle or the Settings picker. */
export function onTheme(cb: (t: Theme) => void): () => void {
  const handler = (e: Event): void => cb((e as CustomEvent<Theme>).detail ?? getTheme())
  window.addEventListener(EVENT, handler)
  const mq = window.matchMedia?.('(prefers-color-scheme: light)')
  const sys = (): void => cb(getTheme())
  mq?.addEventListener('change', sys)
  return () => {
    window.removeEventListener(EVENT, handler)
    mq?.removeEventListener('change', sys)
  }
}
