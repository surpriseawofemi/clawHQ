/**
 * Per-machine viewing preferences, kept in localStorage: nothing here is fleet
 * state, so it never needs the gateway. Components subscribe to change events so a
 * switch flipped in Settings applies to an open thread at once.
 */

const KEY = 'clawhq.prefs'
const EVENT = 'clawhq:prefs'

export type Prefs = {
  /** Show each assistant message's tool calls in the thread. Off by default: noise for most reading. */
  showToolCalls: boolean
  /** Activity: hide automation (cron) runs and runs that said nothing. On by default. */
  hideActivityNoise: boolean
  /** Chat sidebar: order of agents. recent = who spoke last, first. */
  sidebarSort: 'recent' | 'name' | 'department'
  /** Chat sidebar: group agents under their departments. */
  sidebarGroup: boolean
}

const DEFAULTS: Prefs = { showToolCalls: false, hideActivityNoise: true, sidebarSort: 'recent', sidebarGroup: true }

export function getPrefs(): Prefs {
  try {
    const raw = localStorage.getItem(KEY)
    return raw ? { ...DEFAULTS, ...(JSON.parse(raw) as Partial<Prefs>) } : DEFAULTS
  } catch {
    return DEFAULTS
  }
}

export function setPref<K extends keyof Prefs>(key: K, value: Prefs[K]): Prefs {
  const next = { ...getPrefs(), [key]: value }
  try {
    localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    /* private mode: the change lasts for this window only */
  }
  window.dispatchEvent(new CustomEvent(EVENT, { detail: next }))
  return next
}

export function onPrefs(cb: (p: Prefs) => void): () => void {
  const handler = (e: Event): void => cb((e as CustomEvent<Prefs>).detail ?? getPrefs())
  window.addEventListener(EVENT, handler)
  return () => window.removeEventListener(EVENT, handler)
}
