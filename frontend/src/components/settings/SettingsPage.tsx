import { useEffect, useState } from 'react'
import type { Agent, ClawHQConfig, ConnectionStatus, DaemonStatus, Department } from '../../types'
import { UsagePage } from './UsagePage'
import { AppPanel } from '../AppPanel'
import { PageTop } from '../PageTop'
import { api } from '../../api'
import { ConnectionPanel } from '../ConnectionPanel'
import { NodePanel } from '../NodePanel'
import { PairingApprovals } from '../PairingApprovals'
import { UpdatePanel } from '../UpdatePanel'
import { PluginsPage } from './PluginsPage'
import { McpServersPage } from './McpServersPage'
import { AutomationsPage } from './AutomationsPage'
import { HealthPage } from './HealthPage'
import { ChannelsPage } from './ChannelsPage'
import { NotificationsPage } from './NotificationsPage'
import { CommandHistoryPage } from './CommandHistoryPage'

type Props = {
  status: ConnectionStatus
  daemon: DaemonStatus | null
  config: ClawHQConfig | null
  agents?: Agent[]
  pendingCount: number
  initialSection?: SettingsSection
  onOpenAgent: (agentId: string) => void
  onClose: () => void
  onConfigChanged: (config: ClawHQConfig) => void
  onDaemonChanged: (daemon: DaemonStatus) => void
  onReconnected: () => void
}

export type SettingsSection =
  | 'gateways'
  | 'machine'
  | 'departments'
  | 'notifications'
  | 'commands'
  | 'health'
  | 'channels'
  | 'usage'
  | 'service'
  | 'plugins'
  | 'mcp'
  | 'automations'
  | 'updates'

type NavGroup = { label: string; items: { id: SettingsSection; label: string }[] }

/** Grouped by what a person is trying to do, not by how the gateway is built. */
const NAV: NavGroup[] = [
  {
    label: 'Connection',
    items: [
      { id: 'gateways', label: 'Gateways' },
      { id: 'machine', label: 'This machine' }
    ]
  },
  {
    label: 'Organisation',
    items: [
      { id: 'departments', label: 'Departments' },
      { id: 'usage', label: 'Usage' },
      { id: 'notifications', label: 'Notifications' }
    ]
  },
  { label: 'Trust', items: [{ id: 'commands', label: 'Command history' }] },
  {
    label: 'Gateway',
    items: [
      { id: 'service', label: 'Service' },
      { id: 'plugins', label: 'Plugins' },
      { id: 'mcp', label: 'MCP servers' },
      { id: 'channels', label: 'Channels' },
      { id: 'automations', label: 'Automations' },
      { id: 'health', label: 'Health and logs' }
    ]
  },
  { label: 'App', items: [{ id: 'updates', label: 'This app and updates' }] }
]

const TITLES: Record<SettingsSection, string> = {
  gateways: 'Gateways',
  machine: 'This machine',
  departments: 'Departments',
  notifications: 'Notifications',
  commands: 'Command history',
  health: 'Health and logs',
  channels: 'Channels',
  usage: 'Usage',
  service: 'Gateway service',
  plugins: 'Plugins',
  mcp: 'MCP servers',
  automations: 'Automations',
  updates: 'This app and updates'
}

const formatUptime = (ms: number | null): string => {
  if (!ms) return 'unknown'
  const s = Math.floor(ms / 1000)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

/**
 * Settings as a page in the main pane, with a section nav on the left.
 *
 * A modal stopped fitting once connection, node, command policy, departments and
 * the gateway's own config all lived in it, and it hid banners behind itself. The
 * page takes over the main pane the way the desktop view does; the sidebar stays.
 */
export function SettingsPage({
  status,
  daemon,
  config,
  agents,
  pendingCount,
  initialSection,
  onOpenAgent,
  onClose,
  onConfigChanged,
  onDaemonChanged,
  onReconnected
}: Props): React.JSX.Element {
  const [section, setSection] = useState<SettingsSection>(initialSection ?? 'gateways')
  useEffect(() => {
    if (initialSection) setSection(initialSection)
  }, [initialSection])
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [configFilePath, setConfigFilePath] = useState('')
  const connected = status.phase === 'connected'

  useEffect(() => {
    api.config
      .path()
      .then(setConfigFilePath)
      .catch(() => undefined)
  }, [])

  // Escape closes, like the modal did.
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

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

  // A gateway reached over a tunnel cannot be started from here, but it can be
  // asked to restart itself.
  const remoteRestart = (): Promise<void> =>
    run('remote-restart', async () => {
      await api.rpc.request('gateway.restart.request', { reason: 'Requested from ClawHQ' })
      setNote('The gateway is restarting. ClawHQ reconnects on its own.')
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
      const next = await api.config.upsertDepartment({ id, name, emoji: newDept.emoji.trim() || '🏷️' })
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
    <div className="page">
    <PageTop title="Settings" onBack={onClose} />
    <div className="settings">
      <nav className="settings-nav" aria-label="Settings sections">
        <div className="settings-nav-head">
          <h2>Settings</h2>
        </div>
        {NAV.map((group) => (
          <div key={group.label}>
            <div className="settings-nav-group">{group.label}</div>
            {group.items.map((item) => (
              <button
                key={item.id}
                className={`settings-nav-item${section === item.id ? ' is-active' : ''}`}
                onClick={() => setSection(item.id)}
              >
                {item.label}
                {item.id === 'gateways' && pendingCount > 0 && <span className="badge">{pendingCount}</span>}
              </button>
            ))}
          </div>
        ))}
      </nav>

      <div className="settings-main">
        <header>
          <h2>{TITLES[section]}</h2>
        </header>

        {section === 'gateways' && (
          <div className="settings-stack">
            <PairingApprovals connected={connected} />
            <section className="panel">
              <h3>At launch</h3>
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={config?.autoConnect ?? true}
                  disabled={busy !== null}
                  onChange={(e) =>
                    run('autoconnect', async () => {
                      onConfigChanged(await api.config.setAutoConnect(e.target.checked))
                    })
                  }
                />
                <span>Connect to the last used gateway automatically</span>
              </label>
              <p className="field-hint">
                Keeps retrying while the gateway is down and reconnects when it comes back.
                Switched off, ClawHQ opens on the gateway picker instead.
              </p>
            </section>
            <ConnectionPanel status={status} onConnected={onReconnected} />
          </div>
        )}

        {section === 'machine' && (
          <div className="settings-stack">
            <NodePanel />
          </div>
        )}

        {section === 'departments' && (
          <div className="settings-stack">
            <section className="panel">
              <h3>Departments</h3>
              <p className="field-hint">
                Departments are ClawHQ&apos;s own grouping, stored in{' '}
                <code>{configFilePath || '~/.openclaw/clawhq.json'}</code>. Agents can list,
                create and assign them too, through this machine&apos;s node role.
              </p>

              <div className="dept-editor">
                {(config?.departments ?? []).map((dept) => (
                  <div key={dept.id} className="dept-edit-row">
                    <input
                      className="emoji-input"
                      value={dept.emoji}
                      onChange={(e) => updateDepartment(dept, { emoji: e.target.value })}
                    />
                    <input value={dept.name} onChange={(e) => updateDepartment(dept, { name: e.target.value })} />
                    <button className="icon-btn" title="Delete department" onClick={() => removeDepartment(dept.id)}>
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
          </div>
        )}

        {section === 'service' && (
          <div className="settings-stack">
            <section className="panel">
              <h3>OpenClaw service on this machine</h3>
              <p className="field-hint">
                Start, stop and restart control the OpenClaw service installed here. For a
                gateway you reach over a tunnel, use the remote restart below instead.
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
                  <button className="btn btn-ghost" onClick={() => api.shell.openPath(daemon.logFile as string)}>
                    Open log
                  </button>
                )}
              </div>
            </section>

            <section className="panel">
              <h3>Connected gateway</h3>
              <dl className="kv">
                <dt>Address</dt>
                <dd className="mono">{status.url ?? 'not connected'}</dd>
                <dt>Version</dt>
                <dd>{status.serverVersion ?? 'unknown'}</dd>
              </dl>
              <div className="btn-row">
                <button className="btn" disabled={!connected || busy !== null} onClick={() => void remoteRestart()}>
                  {busy === 'remote-restart' ? 'Requesting…' : 'Restart the gateway'}
                </button>
              </div>
              <p className="field-hint">
                Works wherever the gateway runs. Every connected client drops for a few
                seconds and comes back.
              </p>
            </section>
          </div>
        )}

        {section === 'notifications' && <NotificationsPage onOpenAgent={onOpenAgent} />}
        {section === 'commands' && <CommandHistoryPage />}
        {section === 'plugins' && <PluginsPage connected={connected} />}
        {section === 'mcp' && <McpServersPage connected={connected} />}
        {section === 'automations' && <AutomationsPage connected={connected} />}
        {section === 'health' && <HealthPage connected={connected} />}
        {section === 'channels' && <ChannelsPage connected={connected} />}
        {section === 'usage' && <UsagePage connected={connected} config={config} agents={agents ?? []} />}

        {section === 'updates' && (
          <div className="settings-stack">
            <AppPanel config={config} onConfigChanged={onConfigChanged} />
            <UpdatePanel />
          </div>
        )}

        {error && <p className="error-text">{error}</p>}
        {note && <p className="note-text">{note}</p>}
      </div>
    </div>
    </div>
  )
}
