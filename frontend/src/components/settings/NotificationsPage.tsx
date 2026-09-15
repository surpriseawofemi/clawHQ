import { useCallback, useEffect, useState } from 'react'
import { api } from '../../api'
import type { NodeNotification } from '../../types'

type Props = {
  /** Jump to the agent's chat. */
  onOpenAgent: (agentId: string) => void
}

const when = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  return sameDay
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/**
 * Everything agents have asked this machine, newest first, kept on disk so a
 * request made while nobody was at the keyboard is still here later. Opening the
 * page marks it all as read.
 */
export function NotificationsPage({ onOpenAgent }: Props): React.JSX.Element {
  const [items, setItems] = useState<NodeNotification[]>([])
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setItems(await api.inbox.list())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        setItems(await api.inbox.markAllRead())
      } catch {
        await refresh()
      }
    })()
    return api.onNodeNotification(() => void refresh())
  }, [refresh])

  const remove = async (id: string): Promise<void> => {
    try {
      setItems(await api.inbox.remove(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const clear = async (): Promise<void> => {
    try {
      setItems(await api.inbox.clear())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>Notifications</h3>
        <p className="field-hint">
          Requests agents sent to this machine, newest first. They stay here until you
          delete them, so nothing is lost when you are away from the keyboard.
        </p>
        {items.length === 0 && <p className="field-hint">Nothing yet.</p>}
        <div className="inbox-list">
          {items.map((n) => (
            <article key={n.id} className={`inbox-item${n.read ? '' : ' is-unread'}`}>
              <span className="inbox-emoji">{n.agentEmoji || '🔔'}</span>
              <div className="inbox-body">
                <div className="inbox-head">
                  <strong>{n.agentName || n.agentId || 'An agent'}</strong>
                  {n.origin && <span className="plugin-desc">via {n.origin}</span>}
                  <span className="inbox-time">{when(n.atMs)}</span>
                </div>
                <div className="inbox-title">{n.title}</div>
                {n.body && <p className="inbox-text">{n.body}</p>}
              </div>
              <div className="inbox-actions">
                {n.agentId && (
                  <button className="btn btn-sm" onClick={() => onOpenAgent(n.agentId!)}>
                    Open chat
                  </button>
                )}
                <button className="icon-btn" title="Delete" onClick={() => void remove(n.id)}>
                  🗑
                </button>
              </div>
            </article>
          ))}
        </div>
        {items.length > 0 && (
          <div className="btn-row">
            <button className="btn btn-ghost" onClick={() => void clear()}>
              Clear all
            </button>
          </div>
        )}
        {error && <p className="error-text">{error}</p>}
      </section>
    </div>
  )
}
