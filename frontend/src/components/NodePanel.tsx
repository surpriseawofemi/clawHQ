import { useEffect, useState } from 'react'
import { api } from '../api'
import type { NodeStatus } from '../types'

/**
 * The node role: this machine exposed to agents running on the gateway.
 *
 * It is off by default and scoped to an explicit folder list, because turning it on is
 * the moment agents elsewhere gain reach into this computer.
 */
export function NodePanel(): React.JSX.Element {
  const [status, setStatus] = useState<NodeStatus | null>(null)
  const [token, setToken] = useState('')
  const [folder, setFolder] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.node
      .status()
      .then(setStatus)
      .catch(() => undefined)
    return api.onNodeStatus(setStatus)
  }, [])

  const run = async (label: string, fn: () => Promise<NodeStatus>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      setStatus(await fn())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const folders = status?.sharedFolders ?? []

  const addFolder = (): Promise<void> =>
    run('add', async () => {
      const next = [...folders, folder.trim()].filter(Boolean)
      setFolder('')
      return await api.node.setSharedFolders(next)
    })

  const removeFolder = (path: string): Promise<void> =>
    run(`rm-${path}`, () => api.node.setSharedFolders(folders.filter((f) => f !== path)))

  return (
    <section className="panel">
      <h3>This machine as a node</h3>
      <p className="field-hint">
        Lets agents on the gateway browse the folders you list below, even when the
        gateway runs on another computer. Off by default.
      </p>

      <dl className="kv">
        <dt>Status</dt>
        <dd>
          <i className={`dot ${status?.connected ? 'dot-ok' : 'dot-off'}`} />
          {status?.connected ? 'connected as a node' : status?.enabled ? 'enabled, offline' : 'off'}
        </dd>
        {status?.deviceId && (
          <>
            <dt>Device</dt>
            <dd className="mono">{status.deviceId.slice(0, 24)}</dd>
          </>
        )}
        {status?.lastInvoke && (
          <>
            <dt>Last call</dt>
            <dd className="mono">{status.lastInvoke}</dd>
          </>
        )}
      </dl>

      <div className="field">
        <span>Shared folders</span>
        <p className="field-hint">
          Agents can browse inside these and nothing else. Paths outside them are refused.
        </p>
        <div className="dept-editor">
          {folders.map((f) => (
            <div key={f} className="dept-edit-row">
              <input className="mono" value={f} readOnly />
              <button className="icon-btn" title="Stop sharing" onClick={() => removeFolder(f)}>
                🗑
              </button>
            </div>
          ))}
          {folders.length === 0 && <p className="field-hint">Nothing shared yet.</p>}
        </div>
        <div className="dept-edit-row">
          <input
            className="mono"
            placeholder="/Users/you/code"
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void addFolder()
            }}
          />
          <button className="btn" disabled={!folder.trim() || busy !== null} onClick={addFolder}>
            Share
          </button>
        </div>
      </div>

      <div className="field">
        <label className="check-row">
          <input
            type="checkbox"
            checked={status?.desktopControl ?? false}
            disabled={busy !== null}
            onChange={(e) => run('control', () => api.node.setDesktopControl(e.target.checked))}
          />
          <span>Allow desktop control</span>
        </label>
        <p className="field-hint">
          Lets ClawHQ on another machine — and agents, through <code>computer.act</code> —
          move the mouse and type on this computer. Off by default. Windows only for now.
        </p>
      </div>

      {status?.enabled && (
        <div className="field">
          <p className="field-hint">
            If this node was paired before desktop control and notifications existed, the
            gateway still holds the old command list. Re-pairing raises a fresh approval with
            the current one.
          </p>
          <button
            className="btn"
            disabled={busy !== null}
            onClick={() => run('repair', () => api.node.rePair())}
          >
            {busy === 'repair' ? 'Forgetting pairing…' : 'Re-pair with new command list'}
          </button>
        </div>
      )}

      {!status?.enabled && (
        <label className="field">
          <span>Gateway token (first time only)</span>
          <input
            className="mono"
            type="password"
            value={token}
            placeholder="needed once to pair the node role"
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
      )}

      <div className="btn-row">
        {status?.enabled ? (
          <button
            className="btn btn-danger"
            disabled={busy !== null}
            onClick={() => run('disable', () => api.node.disable())}
          >
            {busy === 'disable' ? 'Stopping…' : 'Turn off node role'}
          </button>
        ) : (
          <button
            className="btn btn-primary"
            disabled={busy !== null}
            onClick={() => run('enable', () => api.node.enable(token.trim()))}
          >
            {busy === 'enable' ? 'Connecting…' : 'Turn on node role'}
          </button>
        )}
      </div>

      {error && <p className="error-text">{error}</p>}
    </section>
  )
}
