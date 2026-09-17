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
  title: string
  term: Terminal
  fit: FitAddon
  shellId: string | null
  status: 'connecting' | 'connected' | 'ended' | 'error'
  error?: string
  attached: HTMLElement | null
}

const terms: Term[] = []
const subs = new Set<() => void>()
let seq = 0
let wired = false

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
  list(serverId: string): Term[] {
    return terms.filter((t) => t.serverId === serverId)
  },
  get(id: string): Term | undefined {
    return terms.find((t) => t.id === id)
  },
  create(serverId: string): Term {
    wire()
    seq++
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      theme: { background: '#0b0d13', foreground: '#e6e9f0', cursor: '#7c6cff' },
      scrollback: 8000,
      allowProposedApi: true
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    const n = terms.filter((t) => t.serverId === serverId).length + 1
    const t: Term = { id: `term-${Date.now()}-${seq}`, serverId, title: `Terminal ${n}`, term, fit, shellId: null, status: 'connecting', attached: null }
    terms.push(t)
    term.onData((data) => {
      if (t.shellId && t.status === 'connected') void api.servers.write(t.shellId, enc(data)).catch(() => undefined)
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
    t.attached = el
    t.fit.fit()
    t.term.focus()
    if (!t.shellId && t.status === 'connecting') {
      api.servers
        .openShell(t.serverId, t.term.cols, t.term.rows)
        .then((sid) => {
          t.shellId = sid
          t.status = 'connected'
          notify()
        })
        .catch((err) => {
          t.status = 'error'
          t.error = err instanceof Error ? err.message : String(err)
          t.term.write(`\r\n\x1b[31m${t.error}\x1b[0m\r\n`)
          notify()
        })
    }
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
      notify()
    }
  },
  close(id: string): void {
    const i = terms.findIndex((x) => x.id === id)
    if (i < 0) return
    const t = terms[i]
    if (t.shellId) void api.servers.closeShell(t.shellId).catch(() => undefined)
    t.term.dispose()
    terms.splice(i, 1)
    notify()
  },
  /** Reconnects an ended terminal in place. */
  reconnect(id: string): void {
    const t = terms.find((x) => x.id === id)
    if (!t || !t.attached) return
    t.shellId = null
    t.status = 'connecting'
    t.term.clear()
    notify()
    terminals.attach(id, t.attached)
  }
}
