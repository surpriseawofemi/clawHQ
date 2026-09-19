import { useEffect, useState } from 'react'
import { TrashIcon } from './icons'
import { api } from '../api'
import type { Agent, AgentsList, ExecMode, NodeStatus } from '../types'
import { agentLabel } from '../types'

/**
 * The node role: this machine exposed to agents running on the gateway.
 *
 * Pairing is automatic. ClawHQ requests the pairing as a node and approves it as an
 * operator, so the only decisions left here are what agents may reach: which folders,
 * whether they may drive the desktop, and how commands are handled.
 */
export function NodePanel(): React.JSX.Element {
  const [status, setStatus] = useState<NodeStatus | null>(null)
  const [folder, setFolder] = useState('')
  const [allowEntry, setAllowEntry] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [roster, setRoster] = useState<Agent[]>([])

  useEffect(() => {
    api.node
      .status()
      .then(setStatus)
      .catch(() => undefined)
    api.rpc
      .request<AgentsList>('agents.list')
      .then((res) => setRoster(res?.agents ?? []))
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
  const allow = status?.execAllow ?? []
  const mode: ExecMode = status?.execMode ?? 'ask'

  const addFolder = (): Promise<void> =>
    run('add', async () => {
      const next = [...folders, folder.trim()].filter(Boolean)
      setFolder('')
      return await api.node.setSharedFolders(next)
    })

  const removeFolder = (path: string): Promise<void> =>
    run(`rm-${path}`, () => api.node.setSharedFolders(folders.filter((f) => f !== path)))

  const setMode = (next: ExecMode): Promise<void> =>
    run('mode', () => api.node.setExecPolicy(next, allow))

  const addAllow = (): Promise<void> =>
    run('allow-add', async () => {
      const entry = allowEntry.trim()
      setAllowEntry('')
      if (!entry || allow.includes(entry)) return status as NodeStatus
      return await api.node.setExecPolicy(mode, [...allow, entry])
    })

  const removeAllow = (entry: string): Promise<void> =>
    run(`allow-rm-${entry}`, () => api.node.setExecPolicy(mode, allow.filter((a) => a !== entry)))

  // Per-agent overrides need the roster; without a gateway the overrides already set
  // are still listed by id.
  const agentModes = status?.execAgents ?? {}
  const agentIds = [...new Set([...roster.map((a) => a.id), ...Object.keys(agentModes)])].sort()
  const setAgentMode = (agentId: string, next: ExecMode | ''): Promise<void> =>
    run(`agent-${agentId}`, () => api.node.setAgentExecMode(agentId, next))

  const statusLine = ((): { dot: string; text: string } => {
    if (!status?.enabled) return { dot: 'dot-off', text: 'off' }
    if (status.connected) return { dot: 'dot-ok', text: 'connected as a node' }
    switch (status.pairing) {
      case 'awaiting-approval':
        return { dot: 'dot-warn', text: 'pairing, approving automatically…' }
      case 'reconnecting':
        return { dot: 'dot-warn', text: 'reconnecting…' }
      case 'connecting':
        return { dot: 'dot-warn', text: 'connecting…' }
      default:
        return { dot: 'dot-off', text: 'on, waiting for the gateway connection' }
    }
  })()

  return (
    <section className="panel">
      <h3>This machine as a node</h3>
      <p className="field-hint">
        Lets agents on the gateway reach this computer, even when the gateway runs
        somewhere else. ClawHQ pairs and approves the node by itself; what agents may
        touch is decided below.
      </p>

      <label className="check-row">
        <input
          type="checkbox"
          checked={status?.enabled ?? false}
          disabled={busy !== null}
          onChange={(e) =>
            run('toggle', () => (e.target.checked ? api.node.enable() : api.node.disable()))
          }
        />
        <span>Expose this machine to agents</span>
      </label>

      <dl className="kv">
        <dt>Status</dt>
        <dd>
          <i className={`dot ${statusLine.dot}`} />
          {statusLine.text}
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
        {status?.error && (
          <>
            <dt>Note</dt>
            <dd className="error-text">{status.error}</dd>
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
              <button className="icon-btn" title="Stop sharing" onClick={() => removeFolder(f)}><TrashIcon /></button>
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
        <span>Commands agents may run here</span>
        <p className="field-hint">
          An agent using its exec tool with this machine as the host goes through this
          policy. Asking shows a banner with allow, always and deny.
        </p>
        <div className="tabs tabs-inline">
          {(
            [
              ['ask', 'Ask me'],
              ['allow', 'Run without asking'],
              ['off', 'Off']
            ] as [ExecMode, string][]
          ).map(([value, label]) => (
            <button
              key={value}
              className={`tab${mode === value ? ' is-active' : ''}`}
              disabled={busy !== null}
              onClick={() => setMode(value)}
            >
              {label}
            </button>
          ))}
        </div>
        {mode !== 'off' && (
          <>
            <p className="field-hint">
              Always allowed: an exact command, a program name such as <code>git</code>, or a
              prefix ending in <code>*</code>. &ldquo;Always&rdquo; on a banner adds to this list.
            </p>
            <div className="dept-editor">
              {allow.map((entry) => (
                <div key={entry} className="dept-edit-row">
                  <input className="mono" value={entry} readOnly />
                  <button className="icon-btn" title="Remove" onClick={() => removeAllow(entry)}><TrashIcon /></button>
                </div>
              ))}
            </div>
            <div className="dept-edit-row">
              <input
                className="mono"
                placeholder="git *"
                value={allowEntry}
                onChange={(e) => setAllowEntry(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void addAllow()
                }}
              />
              <button className="btn" disabled={!allowEntry.trim() || busy !== null} onClick={addAllow}>
                Allow
              </button>
            </div>
          </>
        )}
        {agentIds.length > 0 && (
          <>
            <p className="field-hint">
              Per agent: a trusted agent runs without asking while a new one still asks.
              &ldquo;Trust&rdquo; on a banner sets this too.
            </p>
            <div className="dept-editor">
              {agentIds.map((id) => {
                const agent = roster.find((a) => a.id === id)
                const override = agentModes[id] ?? ''
                return (
                  <div key={id} className={`dept-edit-row agent-policy-row${override ? ' is-set' : ''}`}>
                    <span className="agent-policy-name">
                      {agent ? agentLabel(agent) : id}
                      {agent && agentLabel(agent) !== id && <span className="plugin-desc"> {id}</span>}
                    </span>
                    <select
                      value={override}
                      disabled={busy !== null}
                      onChange={(e) => void setAgentMode(id, e.target.value as ExecMode | '')}
                    >
                      <option value="">Same as this machine</option>
                      <option value="allow">Trusted, runs without asking</option>
                      <option value="ask">Ask me</option>
                      <option value="off">Off</option>
                    </select>
                  </div>
                )
              })}
            </div>
          </>
        )}
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
          Lets ClawHQ on another machine, and agents through <code>computer.act</code>, move
          the mouse and type on this computer. Off by default. On macOS the first use asks
          for Accessibility and Screen Recording permission.
        </p>
      </div>

      {status?.enabled && (
        <div className="field">
          <p className="field-hint">
            The gateway records the command list at pairing time. ClawHQ re-pairs by itself
            when that list changes; this forces it.
          </p>
          <button
            className="btn"
            disabled={busy !== null}
            onClick={() => run('repair', () => api.node.rePair())}
          >
            {busy === 'repair' ? 'Re-pairing…' : 'Re-pair now'}
          </button>
        </div>
      )}

      {error && <p className="error-text">{error}</p>}
    </section>
  )
}
