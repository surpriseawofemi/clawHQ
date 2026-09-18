import { useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'
import { terminals, type Term } from '../state/terminals'

/**
 * Terminal tabs for one server. Terminals are kept in the registry, so leaving
 * the page and coming back finds them where they were, still connected.
 */
export function TerminalTabs({ serverId, projectId = '', dir = '', tmux = false }: { serverId: string; projectId?: string; dir?: string; tmux?: boolean }): React.JSX.Element {
  const [, bump] = useState(0)
  const [active, setActive] = useState<string | null>(null)
  useEffect(() => terminals.subscribe(() => bump((n) => n + 1)), [])

  const list = terminals.list(serverId, projectId)
  useEffect(() => {
    if (list.length === 0) {
      const restored = terminals.restore(serverId, projectId, dir, tmux)
      setActive(restored[restored.length - 1]?.id ?? null)
    } else if (!active || !list.some((t) => t.id === active)) {
      setActive(list[list.length - 1].id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, projectId, list.length])

  const current = list.find((t) => t.id === active) ?? null

  return (
    <div className="term-tabs-wrap">
      <div className="term-tabs">
        {list.map((t) => (
          <div key={t.id} className={`term-tab${t.id === active ? ' is-active' : ''}`} onClick={() => setActive(t.id)}>
            <i className={`desk-dot ${t.status === 'connected' ? 'is-online' : t.status === 'connecting' ? 'is-working' : 'is-idle'}`} />
            <span className="term-tab-title" title={t.session ? `tmux session ${t.session}: survives closing ClawHQ` : 'plain shell'}>{t.session ? '⟳ ' : ''}{t.title}</span>
            {t.session && (
              <button className="term-tab-close" title="Detach: keep it running on the server; it comes back next time" onClick={(e) => { e.stopPropagation(); terminals.close(t.id, false) }}>⇣</button>
            )}
            <button className="term-tab-close" title={t.session ? 'Close and end the session on the server' : 'Close this terminal'} onClick={(e) => { e.stopPropagation(); terminals.close(t.id, true) }}>×</button>
          </div>
        ))}
        <button className="term-tab-add" title="New terminal in this project" onClick={() => setActive(terminals.create(serverId, projectId, dir, tmux).id)}>＋</button>
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
      <div className="term-host" ref={host} />
    </div>
  )
}
