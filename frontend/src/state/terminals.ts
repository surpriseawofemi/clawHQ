import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { api } from '../api'

/**
 * Terminals live outside React so they survive page switches: each one is an
 * xterm instance plus a shell in Go, kept here until closed. A view attaches
 * the terminal's element into its own container and detaches on unmount
 * without disposing anything.
 */
export type Term = {
  id: string
  serverId: string
  projectId: string
  dir: string
  /** tmux session name on the server; empty for a plain shell. */
  session: string
  title: string
  term: Terminal
  fit: FitAddon
  shellId: string | null
  status: 'connecting' | 'connected' | 'ended' | 'error' | 'reconnecting'
  error?: string
  attached: HTMLElement | null
  menuWired?: boolean
  retries?: number
  retryTimer?: number
}

const terms: Term[] = []
const subs = new Set<() => void>()
let seq = 0
let wired = false

/** Saved tabs per server+project, so they come back after a restart. */
type SavedTab = { title: string; session: string; dir: string }
const savedKey = (serverId: string, projectId: string): string => `clawhq.terms.${serverId}.${projectId || 'home'}`
const readSaved = (serverId: string, projectId: string): SavedTab[] => {
  try {
    const raw = localStorage.getItem(savedKey(serverId, projectId))
    return raw ? (JSON.parse(raw) as SavedTab[]) : []
  } catch {
    return []
  }
}
const writeSaved = (serverId: string, projectId: string, tabs: SavedTab[]): void => {
  try {
    if (tabs.length === 0) localStorage.removeItem(savedKey(serverId, projectId))
    else localStorage.setItem(savedKey(serverId, projectId), JSON.stringify(tabs))
  } catch {
    /* per-machine convenience */
  }
}
const slug = (s: string): string => s.toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40)

const notify = (): void => subs.forEach((cb) => cb())

const enc = (s: string): string => {
  const bytes = new TextEncoder().encode(s)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin)
}
const dec = (b64: string): Uint8Array => {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function wire(): void {
  if (wired) return
  wired = true
  api.onShellOut((e) => {
    const t = terms.find((x) => x.shellId === e.id)
    if (t) t.term.write(dec(e.data))
  })
  api.onShellExit((e) => {
    const t = terms.find((x) => x.shellId === e.id)
    if (!t) return
    t.shellId = null
    // A tmux tab whose link dropped (an error, not a clean exit) is reattached by
    // itself: the session is still on the server. A clean exit means the user
    // left the shell, so the tab just ends.
    if (t.session && e.error) {
      t.status = 'reconnecting'
      t.retries = (t.retries ?? 0) + 1
      const delay = Math.min(30_000, 2000 * 2 ** Math.min(t.retries - 1, 4))
      t.term.write(`\r\n\x1b[90m[link dropped: ${e.error}; reconnecting in ${Math.round(delay / 1000)}s]\x1b[0m\r\n`)
      t.retryTimer = window.setTimeout(() => {
        t.retryTimer = undefined
        if (t.status === 'reconnecting') terminals.reattach(t.id)
      }, delay)
      notify()
      return
    }
    t.status = 'ended'
    t.term.write(`\r\n\x1b[90m[session ended${e.error ? `: ${e.error}` : ''}]\x1b[0m\r\n`)
    notify()
  })
}

export const terminals = {
  subscribe(cb: () => void): () => void {
    subs.add(cb)
    return () => subs.delete(cb)
  },
  list(serverId: string, projectId?: string): Term[] {
    return terms.filter((t) => t.serverId === serverId && (projectId === undefined || t.projectId === projectId))
  },
  get(id: string): Term | undefined {
    return terms.find((t) => t.id === id)
  },
  /**
   * Brings back the tabs saved for a project (their tmux sessions reattach),
   * or opens one fresh terminal when nothing was saved.
   */
  restore(serverId: string, projectId: string, dir: string, tmux: boolean): Term[] {
    const existing = terms.filter((t) => t.serverId === serverId && t.projectId === projectId)
    if (existing.length > 0) return existing
    const saved = readSaved(serverId, projectId)
    if (saved.length === 0) return [terminals.create(serverId, projectId, dir, tmux)]
    return saved.map((sv) => terminals.create(serverId, projectId, sv.dir || dir, tmux, sv.session, sv.title))
  },
  create(serverId: string, projectId = '', dir = '', tmux = false, session?: string, title?: string): Term {
    wire()
    seq++
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      theme: { background: '#0b0d13', foreground: '#e6e9f0', cursor: '#7c6cff' },
      scrollback: 8000,
      allowProposedApi: true,
      // tmux takes the mouse, so a plain drag is tmux's selection; holding ⌥ selects in the page.
      macOptionClickForcesSelection: true
    })
    // tmux copies (mouse drag, copy-mode) arrive as OSC 52 when set-clipboard is on.
    term.parser.registerOscHandler(52, (data) => {
      const i = data.indexOf(';')
      const b64 = i >= 0 ? data.slice(i + 1) : data
      if (!b64 || b64 === '?') return true
      try {
        const bytes = dec(b64)
        const text = new TextDecoder().decode(bytes)
        if (text) void api.clipboard.write(text).catch(() => undefined)
      } catch {
        /* not base64 */
      }
      return true
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    const n = terms.filter((t) => t.serverId === serverId && t.projectId === projectId).length + 1
    const name = session ?? (tmux ? `clawhq-${slug(projectId || 'home')}-${Date.now().toString(36).slice(-5)}` : '')
    const t: Term = { id: `term-${Date.now()}-${seq}`, serverId, projectId, dir, session: name, title: title ?? `Terminal ${n}`, term, fit, shellId: null, status: 'connecting', attached: null }
    terms.push(t)
    terminals.save(serverId, projectId)
    term.onData((data) => {
      if (t.shellId && t.status === 'connected') void api.servers.write(t.shellId, enc(data)).catch(() => undefined)
    })
    // Selecting text copies it; right-click pastes. The OS clipboard goes through
    // Go because the webview's clipboard API is unreliable here.
    term.onSelectionChange(() => {
      const sel = term.getSelection()
      if (sel) void api.clipboard.write(sel).catch(() => undefined)
    })
    notify()
    return t
  },
  /** Puts the terminal's element into a container and opens the shell on first attach. */
  attach(id: string, el: HTMLElement): void {
    const t = terms.find((x) => x.id === id)
    if (!t) return
    if (!t.term.element) t.term.open(el)
    else el.appendChild(t.term.element)
    if (!t.menuWired) {
      t.menuWired = true
      const node = t.term.element as HTMLElement | undefined
      // The right button is ours: paste. Stop it before xterm forwards it to tmux,
      // whose default right-click binding opens a split/kill popup menu.
      const swallow = (e: MouseEvent): void => {
        if (e.button === 2) {
          e.preventDefault()
          e.stopImmediatePropagation()
        }
      }
      node?.addEventListener('mousedown', swallow, true)
      node?.addEventListener('mouseup', swallow, true)
      node?.addEventListener('contextmenu', (e) => {
        e.preventDefault()
        e.stopImmediatePropagation()
        void terminals.paste(t.id)
      }, true)
    }
    t.attached = el
    t.fit.fit()
    t.term.focus()
    if (!t.shellId && (t.status === 'connecting' || t.status === 'reconnecting')) terminals.open(t)
  },
  /** Opens the shell for a terminal; retries by itself on a dropped tmux link. */
  open(t: Term): void {
    api.servers
      .openShellIn(t.serverId, t.dir, t.term.cols, t.term.rows, t.session)
      .then((sid) => {
        t.shellId = sid
        t.status = 'connected'
        t.retries = 0
        t.error = undefined
        notify()
        if (t.attached) terminals.resize(t.id)
      })
      .catch((err) => {
        const msg = err instanceof Error ? err.message : String(err)
        if (t.session && (t.retries ?? 0) < 8) {
          t.status = 'reconnecting'
          t.retries = (t.retries ?? 0) + 1
          const delay = Math.min(30_000, 2000 * 2 ** Math.min(t.retries - 1, 4))
          t.term.write(`\r\n\x1b[90m[${msg}; retrying in ${Math.round(delay / 1000)}s]\x1b[0m\r\n`)
          t.retryTimer = window.setTimeout(() => {
            t.retryTimer = undefined
            if (t.status === 'reconnecting') terminals.open(t)
          }, delay)
        } else {
          t.status = 'error'
          t.error = msg
          t.term.write(`\r\n\x1b[31m${msg}\x1b[0m\r\n`)
        }
        notify()
      })
  },
  /** Reattaches a tmux tab now (after a dropped link). */
  reattach(id: string): void {
    const t = terms.find((x) => x.id === id)
    if (!t || t.shellId) return
    t.status = 'reconnecting'
    notify()
    terminals.open(t)
  },
  detach(id: string): void {
    const t = terms.find((x) => x.id === id)
    if (!t) return
    t.attached = null
    // The element stays alive off-DOM; nothing is disposed.
    t.term.element?.remove()
  },
  resize(id: string): void {
    const t = terms.find((x) => x.id === id)
    if (!t || !t.attached) return
    t.fit.fit()
    if (t.shellId && t.status === 'connected') void api.servers.resize(t.shellId, t.term.cols, t.term.rows).catch(() => undefined)
  },
  rename(id: string, title: string): void {
    const t = terms.find((x) => x.id === id)
    if (t) {
      t.title = title
      terminals.save(t.serverId, t.projectId)
      notify()
    }
  },
  /** Writes the project's tab list (titles and tmux session names) to localStorage. */
  save(serverId: string, projectId: string): void {
    const tabs = terms.filter((t) => t.serverId === serverId && t.projectId === projectId).map((t) => ({ title: t.title, session: t.session, dir: t.dir }))
    writeSaved(serverId, projectId, tabs)
  },
  /** Closes a tab. With kill, its tmux session ends too; otherwise it keeps running and comes back next time. */
  close(id: string, kill = true): void {
    const i = terms.findIndex((x) => x.id === id)
    if (i < 0) return
    const t = terms[i]
    if (t.retryTimer) window.clearTimeout(t.retryTimer)
    if (t.shellId) void api.servers.closeShell(t.shellId).catch(() => undefined)
    if (kill && t.session) void api.servers.killTmux(t.serverId, t.session).catch(() => undefined)
    t.term.dispose()
    terms.splice(i, 1)
    if (kill) terminals.save(t.serverId, t.projectId)
    else {
      // Detached: keep it in the saved list so it is reattached on the next visit.
      const saved = readSaved(t.serverId, t.projectId)
      if (!saved.some((sv) => sv.session === t.session)) saved.push({ title: t.title, session: t.session, dir: t.dir })
      writeSaved(t.serverId, t.projectId, saved)
    }
    notify()
  },
  /** Drops every terminal of a server from the page; tmux sessions stay and come back on reconnect. */
  detachAll(serverId: string): void {
    for (const t of terms.filter((x) => x.serverId === serverId)) terminals.close(t.id, false)
  },
  /** Pastes the Mac's clipboard into the terminal (right-click, or the paste button). */
  async paste(id: string): Promise<void> {
    const t = terms.find((x) => x.id === id)
    if (!t || !t.shellId || t.status !== 'connected') return
    let text = ''
    try {
      text = await api.clipboard.read()
    } catch {
      try {
        text = await navigator.clipboard.readText()
      } catch {
        return
      }
    }
    if (text) t.term.paste(text)
  },
  /** Reconnects an ended terminal in place. */
  reconnect(id: string): void {
    const t = terms.find((x) => x.id === id)
    if (!t) return
    if (t.retryTimer) window.clearTimeout(t.retryTimer)
    t.shellId = null
    t.retries = 0
    t.status = 'connecting'
    if (!t.session) t.term.clear()
    notify()
    terminals.open(t)
  }
}
