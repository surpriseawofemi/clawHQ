import { useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'
import { terminals, type Term } from '../state/terminals'
import { api } from '../api'

/**
 * Terminal tabs for one server. Terminals are kept in the registry, so leaving
 * the page and coming back finds them where they were, still connected.
 */
/** Which tab was active per project, so leaving and coming back lands on the same one. */
const activeMemory = new Map<string, string>()

export function TerminalTabs({ serverId, projectId = '', dir = '', tmux = false }: { serverId: string; projectId?: string; dir?: string; tmux?: boolean }): React.JSX.Element {
  const [, bump] = useState(0)
  const memKey = `${serverId}:${projectId}`
  const [active, setActiveState] = useState<string | null>(activeMemory.get(memKey) ?? null)
  const setActive = (id: string | null): void => {
    setActiveState(id)
    if (id) activeMemory.set(memKey, id)
  }
  useEffect(() => terminals.subscribe(() => bump((n) => n + 1)), [])

  const list = terminals.list(serverId, projectId)
  useEffect(() => {
    const remembered = activeMemory.get(memKey)
    if (list.length === 0) {
      const restored = terminals.restore(serverId, projectId, dir, tmux)
      setActive(restored.find((t) => t.id === remembered)?.id ?? restored[restored.length - 1]?.id ?? null)
    } else if (!active || !list.some((t) => t.id === active)) {
      setActive(list.find((t) => t.id === remembered)?.id ?? list[list.length - 1].id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, projectId, list.length])

  const current = list.find((t) => t.id === active) ?? null

  return (
    <div className="term-tabs-wrap">
      <div className="term-tabs">
        {list.map((t) => (
          <div key={t.id} className={`term-tab${t.id === active ? ' is-active' : ''}`} onClick={() => setActive(t.id)}>
            <i className={`desk-dot ${t.status === 'connected' ? 'is-online' : t.status === 'connecting' || t.status === 'reconnecting' ? 'is-working' : 'is-idle'}`} title={t.status} />
            <span className="term-tab-title" title={t.session ? `tmux session ${t.session}: survives closing ClawHQ` : 'plain shell'}>{t.session ? '⟳ ' : ''}{t.title}</span>
            {t.session && (
              <button className="term-tab-close" title="Detach: keep it running on the server; it comes back next time" onClick={(e) => { e.stopPropagation(); terminals.close(t.id, false) }}>⇣</button>
            )}
            <button className="term-tab-close" title={t.session ? 'Close and end the session on the server' : 'Close this terminal'} onClick={(e) => { e.stopPropagation(); terminals.close(t.id, true) }}>×</button>
          </div>
        ))}
        <button className="term-tab-add" title="New terminal in this project" onClick={() => setActive(terminals.create(serverId, projectId, dir, tmux).id)}>＋</button>
        {current && (
          <>
            <button className="term-tab-add term-paste" title="Copy the current selection (a drag inside tmux copies by itself; hold ⌥ while dragging to select in the page)" onClick={() => { const sel = current.term.getSelection(); if (sel) void api.clipboard.write(sel).catch(() => undefined) }}>Copy</button>
            <button className="term-tab-add" title="Paste the clipboard (right-click in the terminal does the same)" onClick={() => void terminals.paste(current.id)}>Paste</button>
          </>
        )}
      </div>
      {current && <TerminalPane key={current.id} t={current} />}
    </div>
  )
}

function TerminalPane({ t }: { t: Term }): React.JSX.Element {
  const host = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = host.current
    if (!el) return
    terminals.attach(t.id, el)
    const ro = new ResizeObserver(() => terminals.resize(t.id))
    ro.observe(el)
    return () => {
      ro.disconnect()
      terminals.detach(t.id)
    }
  }, [t.id])
  return (
    <div className="term-wrap">
      {t.status === 'error' && (
        <div className="term-error">
          <span>{t.error}</span>
          <button className="btn btn-sm" onClick={() => terminals.reconnect(t.id)}>Reconnect</button>
        </div>
      )}
      {t.status === 'ended' && (
        <div className="term-error term-ended">
          <span>Session ended.</span>
          <button className="btn btn-sm" onClick={() => terminals.reconnect(t.id)}>Reconnect</button>
        </div>
      )}
      {t.status === 'reconnecting' && (
        <div className="term-error term-ended">
          <span>Link dropped; reconnecting to the tmux session…</span>
          <button className="btn btn-sm" onClick={() => terminals.reconnect(t.id)}>Now</button>
        </div>
      )}
      <div className="term-host" ref={host} />
    </div>
  )
}
