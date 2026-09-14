import { useEffect, useState } from 'react'
import type { ClawHQConfig, ConnectionStatus, DaemonStatus, Department } from '../types'
import { api } from '../api'
import { ConnectionPanel } from './ConnectionPanel'
import { NodePanel } from './NodePanel'
import { PairingApprovals } from './PairingApprovals'
import { UpdatePanel } from './UpdatePanel'

type Props = {
  status: ConnectionStatus
  daemon: DaemonStatus | null
  config: ClawHQConfig | null
  onClose: () => void
  onConfigChanged: (config: ClawHQConfig) => void
  onDaemonChanged: (daemon: DaemonStatus) => void
  onReconnected: () => void
}

type Tab = 'connection' | 'departments'

const formatUptime = (ms: number | null): string => {
  if (!ms) return 'unknown'
  const s = Math.floor(ms / 1000)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

export function SettingsDialog({
  status,
  daemon,
  config,
  onClose,
  onConfigChanged,
  onDaemonChanged,
  onReconnected
}: Props): React.JSX.Element {
  const [tab, setTab] = useState<Tab>('connection')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [configFilePath, setConfigFilePath] = useState('')

  useEffect(() => {
    api.config
      .path()
      .then(setConfigFilePath)
      .catch(() => undefined)
  }, [])

  const run = async (label: string, fn: () => Promise<void>): Promise<void> => {
    setBusy(label)
    setError(null)
    setNote(null)
    try {
      await fn()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const control = (action: 'start' | 'stop' | 'restart'): Promise<void> =>
    run(action, async () => {
      const next = await api.daemon.control(action)
      onDaemonChanged(next)
      setNote(`Gateway ${action} requested.`)
    })

  // ---- departments ------------------------------------------------------
  const [newDept, setNewDept] = useState({ name: '', emoji: '' })

  const addDepartment = (): Promise<void> =>
    run('dept-add', async () => {
      const name = newDept.name.trim()
      if (!name) return
      const id = name
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-|-$/g, '')
      const next = await api.config.upsertDepartment({
        id,
        name,
        emoji: newDept.emoji.trim() || '🏷️'
      })
      onConfigChanged(next)
      setNewDept({ name: '', emoji: '' })
    })

  const updateDepartment = (dept: Department, patch: Partial<Department>): Promise<void> =>
    run(`dept-${dept.id}`, async () => {
      const next = await api.config.upsertDepartment({ ...dept, ...patch })
      onConfigChanged(next)
    })

  const removeDepartment = (id: string): Promise<void> =>
    run(`dept-del-${id}`, async () => {
      const next = await api.config.removeDepartment(id)
      onConfigChanged(next)
    })

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>Settings</h2>
          <button className="icon-btn" onClick={onClose}>
            ✕
          </button>
        </header>

        <div className="tabs">
          <button
            className={`tab${tab === 'connection' ? ' is-active' : ''}`}
            onClick={() => setTab('connection')}
          >
            Connection
          </button>
          <button
            className={`tab${tab === 'departments' ? ' is-active' : ''}`}
            onClick={() => setTab('departments')}
          >
            Departments
          </button>
        </div>

        <div className="modal-body">
          {tab === 'connection' && (
            <>
              <PairingApprovals connected={status.phase === 'connected'} />

              <ConnectionPanel status={status} onConnected={onReconnected} />

              <NodePanel />

              <UpdatePanel />

              <section className="panel">
                <h3>Gateway service</h3>
                <p className="field-hint">
                  These control the OpenClaw service on <em>this</em> machine. They do
                  nothing for a gateway you reach over a tunnel or a domain.
                </p>
                <dl className="kv">
                  <dt>Service</dt>
                  <dd>
                    {daemon?.serviceLabel ?? 'unknown'}
                    {daemon?.installed ? ' (registered)' : ' (not installed)'}
                  </dd>
                  <dt>Process</dt>
                  <dd>
                    <i className={`dot ${daemon?.running ? 'dot-ok' : 'dot-off'}`} />
                    {daemon?.running ? `running - pid ${daemon.pid ?? '?'}` : 'stopped'}
                  </dd>
                  <dt>Uptime</dt>
                  <dd>{formatUptime(daemon?.uptimeMs ?? null)}</dd>
                  <dt>CLI</dt>
                  <dd>{daemon?.cliVersion ?? 'not found on PATH'}</dd>
                </dl>
                <div className="btn-row">
                  <button className="btn" onClick={() => control('start')} disabled={busy !== null}>
                    Start
                  </button>
                  <button className="btn" onClick={() => control('stop')} disabled={busy !== null}>
                    Stop
                  </button>
                  <button className="btn" onClick={() => control('restart')} disabled={busy !== null}>
                    Restart
                  </button>
                  {daemon?.logFile && (
                    <button
                      className="btn btn-ghost"
                      onClick={() => api.shell.openPath(daemon.logFile as string)}
                    >
                      Open log
                    </button>
                  )}
                </div>
              </section>
            </>
          )}

          {tab === 'departments' && (
            <section className="panel">
              <h3>Departments</h3>
              <p className="field-hint">
                Departments are ClawHQ&apos;s own grouping, stored in{' '}
                <code>{configFilePath || '~/.openclaw/clawhq.json'}</code>. Your OpenClaw config is
                never modified by this screen.
              </p>

              <div className="dept-editor">
                {(config?.departments ?? []).map((dept) => (
                  <div key={dept.id} className="dept-edit-row">
                    <input
                      className="emoji-input"
                      value={dept.emoji}
                      onChange={(e) => updateDepartment(dept, { emoji: e.target.value })}
                    />
                    <input
                      value={dept.name}
                      onChange={(e) => updateDepartment(dept, { name: e.target.value })}
                    />
                    <button
                      className="icon-btn"
                      title="Delete department"
                      onClick={() => removeDepartment(dept.id)}
                    >
                      🗑
                    </button>
                  </div>
                ))}
              </div>

              <div className="dept-edit-row">
                <input
                  className="emoji-input"
                  placeholder="🏷️"
                  value={newDept.emoji}
                  onChange={(e) => setNewDept({ ...newDept, emoji: e.target.value })}
                />
                <input
                  placeholder="New department name"
                  value={newDept.name}
                  onChange={(e) => setNewDept({ ...newDept, name: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void addDepartment()
                  }}
                />
                <button className="btn" onClick={addDepartment} disabled={!newDept.name.trim()}>
                  Add
                </button>
              </div>
            </section>
          )}

          {error && <p className="error-text">{error}</p>}
          {note && <p className="note-text">{note}</p>}
        </div>
      </div>
    </div>
  )
}
