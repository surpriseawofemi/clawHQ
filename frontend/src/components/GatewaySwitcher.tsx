import { useEffect, useState } from 'react'
import { EditIcon } from './icons'
import { api } from '../api'
import type { ClawHQConfig, ConnectionStatus, GatewayProfile } from '../types'

type Props = {
  status: ConnectionStatus
  onClose: () => void
  onConnected: () => void
  onConfigChanged: (config: ClawHQConfig) => void
  /** Open Settings, optionally on the Gateways section. */
  onOpenSettings: (section?: 'gateways') => void
}

const lastUsed = (ms?: number): string => {
  if (!ms) return 'never connected'
  const diff = Date.now() - ms
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return `${Math.round(diff / 86_400_000)}d ago`
}

/**
 * Switch between saved gateways, rename them, or go add one.
 *
 * Opened from the connection pill in the sidebar. One gateway is connected at a
 * time; picking another disconnects the current one and reconnects with the stored
 * device token, so switching needs no credentials.
 */
export function GatewaySwitcher({ status, onClose, onConnected, onConfigChanged, onOpenSettings }: Props): React.JSX.Element {
  const [gateways, setGateways] = useState<GatewayProfile[]>([])
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = (): void => {
    api.connection
      .gateways()
      .then((list) => setGateways([...list].sort((a, b) => (b.lastConnectedAtMs ?? 0) - (a.lastConnectedAtMs ?? 0))))
      .catch(() => undefined)
  }

  useEffect(refresh, [status.phase, status.gatewayId])

  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const connect = async (g: GatewayProfile): Promise<void> => {
    setBusy(`connect-${g.id}`)
    setError(null)
    try {
      const next = await api.connection.connect(g.id)
      if (next.phase === 'connected') {
        onConnected()
        onClose()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const saveName = async (g: GatewayProfile): Promise<void> => {
    const name = draft.trim()
    setEditing(null)
    if (!name || name === g.name) return
    try {
      const cfg = await api.connection.renameGateway(g.id, name)
      onConfigChanged(cfg)
      refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-label="Switch gateway">
        <header className="modal-head">
          <h2>Gateways</h2>
          <button className="icon-btn" onClick={onClose}>
            ✕
          </button>
        </header>
        <div className="modal-body">
          <div className="gw-list">
            {gateways.map((g) => {
              const isCurrent = g.id === status.gatewayId
              const live = isCurrent && status.phase === 'connected'
              const connecting = isCurrent && status.phase === 'connecting'
              return (
                <div key={g.id} className={`gw-row${isCurrent ? ' is-active' : ''}`}>
                  <i className={`dot ${live ? 'dot-ok' : connecting ? 'dot-warn' : 'dot-off'}`} />
                  <span className="gw-meta">
                    {editing === g.id ? (
                      <input
                        autoFocus
                        className="gw-rename"
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        onBlur={() => void saveName(g)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') void saveName(g)
                          if (e.key === 'Escape') setEditing(null)
                        }}
                      />
                    ) : (
                      <span className="gw-name">
                        {g.name}
                        <button
                          className="icon-btn gw-edit"
                          title="Rename"
                          onClick={() => {
                            setEditing(g.id)
                            setDraft(g.name)
                          }}
                        ><EditIcon /></button>
                      </span>
                    )}
                    <span className="gw-url mono">
                      {g.url} · {live ? `connected · ${status.serverVersion ?? ''}`.trim() : lastUsed(g.lastConnectedAtMs)}
                    </span>
                  </span>
                  {live ? (
                    <button className="btn btn-sm" onClick={() => void api.connection.disconnect().then(onClose)}>
                      Disconnect
                    </button>
                  ) : (
                    <button
                      className="btn btn-sm btn-primary"
                      disabled={busy !== null}
                      onClick={() => void connect(g)}
                    >
                      {busy === `connect-${g.id}` || connecting ? 'Connecting…' : 'Switch'}
                    </button>
                  )}
                </div>
              )
            })}
            {gateways.length === 0 && <p className="field-hint">No gateways saved yet.</p>}
          </div>
          {error && <p className="error-text">{error}</p>}
        </div>
        <footer className="modal-foot">
          <span className="modal-note">One gateway is connected at a time.</span>
          <div className="modal-actions">
            <button className="btn" onClick={() => onOpenSettings('gateways')}>
              Add gateway
            </button>
            <button className="btn btn-ghost" onClick={() => onOpenSettings()}>
              Settings
            </button>
          </div>
        </footer>
      </div>
    </div>
  )
}
