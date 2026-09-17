import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { api } from '../api'

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

/**
 * An interactive shell on a server: xterm in the page, a PTY over SSH in Go,
 * bytes both ways as base64 events. Closes the session when unmounted.
 */
export function TerminalView({ serverId, onStatus }: { serverId: string; onStatus?: (s: string) => void }): React.JSX.Element {
  const host = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [gen, setGen] = useState(0)

  useEffect(() => {
    const el = host.current
    if (!el) return
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      theme: { background: '#0b0d13', foreground: '#e6e9f0', cursor: '#7c6cff' },
      scrollback: 5000,
      allowProposedApi: true
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    fit.fit()
    term.focus()

    let shellId: string | null = null
    let closed = false
    const offOut = api.onShellOut((e) => {
      if (e.id === shellId) term.write(dec(e.data))
    })
    const offExit = api.onShellExit((e) => {
      if (e.id !== shellId) return
      closed = true
      term.write(`\r\n\x1b[90m[session ended${e.error ? `: ${e.error}` : ''}]\x1b[0m\r\n`)
      onStatus?.('ended')
    })
    const onData = term.onData((data) => {
      if (shellId && !closed) void api.servers.write(shellId, enc(data)).catch(() => undefined)
    })
    const ro = new ResizeObserver(() => {
      fit.fit()
      if (shellId && !closed) void api.servers.resize(shellId, term.cols, term.rows).catch(() => undefined)
    })
    ro.observe(el)

    onStatus?.('connecting')
    api.servers
      .openShell(serverId, term.cols, term.rows)
      .then((id) => {
        shellId = id
        onStatus?.('connected')
        setError(null)
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err))
        onStatus?.('error')
      })

    return () => {
      offOut()
      offExit()
      onData.dispose()
      ro.disconnect()
      if (shellId) void api.servers.closeShell(shellId).catch(() => undefined)
      term.dispose()
    }
  }, [serverId, gen, onStatus])

  return (
    <div className="term-wrap">
      {error && (
        <div className="term-error">
          <span>{error}</span>
          <button className="btn btn-sm" onClick={() => { setError(null); setGen((n) => n + 1) }}>
            Reconnect
          </button>
        </div>
      )}
      <div className="term-host" ref={host} />
    </div>
  )
}
