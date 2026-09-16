import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { hostKind, hostGlyph, hostLabel } from '../state/execPolicy'
import { plugin } from '../state/plugin'
import { ContentHead, Shell, type ShellProps } from './layout/Shell'
import type { ActivityRecord, Agent, ClawHQConfig, Delegation, NodeNotification, Presence, RemoteNode, SessionInfo, Task } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'

type Props = {
  shell: ShellProps
  agents: Agent[]
  sessions: SessionInfo[]
  config: ClawHQConfig | null
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
  onConfigChanged: (config: ClawHQConfig) => void
}

type DeskState = 'idle' | 'working' | 'waiting' | 'failed'

type Desk = {
  agent: Agent
  state: DeskState
  bubble: string
  since?: number
  presence?: Presence
  waiting: number
  openTasks: number
}

const ago = (ms?: number): string => {
  if (!ms) return ''
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  return `${Math.round(s / 3600)}h`
}

const prettyTool = (t: string): string => t.replace(/^mcp__[^_]+__/, '').replace(/_/g, ' ')

/**
 * The floor plan: a room per department, a desk per agent, and the truth about each
 * desk. Working desks show what the agent is doing (its current tool, or the last
 * thing it said). A line runs from a parent desk to a child it spawned. Click a
 * desk for the agent's card: today's runs, context health, tasks, and a move.
 *
 * Live state comes from the gateway plugin's presence feed; without the plugin
 * the session list's "run in flight" flag stands in and the bubbles stay empty.
 */
export function Office({ shell, agents, sessions, config, connected, onOpenAgent, onConfigChanged }: Props): React.JSX.Element {
  const [presence, setPresence] = useState<Presence[]>([])
  const [delegations, setDelegations] = useState<Delegation[]>([])
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [notices, setNotices] = useState<NodeNotification[]>([])
  const [pendingByAgent, setPendingByAgent] = useState<Record<string, number>>({})
  const [tasks, setTasks] = useState<Task[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [runs, setRuns] = useState<ActivityRecord[]>([])
  const [memoryAt, setMemoryAt] = useState<Record<string, number>>({})
  const [tick, setTick] = useState(0)
  const [machines, setMachines] = useState<RemoteNode[]>([])
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const [status, inbox, node] = await Promise.all([api.plugin.status(), api.inbox.list(), api.node.status()])
      if (connected) {
        api.rpc
          .request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>('node.list')
          .then((res) => setMachines(res?.paired ?? res?.nodes ?? []))
          .catch(() => undefined)
      }
      setPluginPresent(status.present)
      setNotices(inbox)
      const pend: Record<string, number> = {}
      for (const p of node.pendingExec ?? []) pend[p.agentId] = (pend[p.agentId] ?? 0) + 1
      setPendingByAgent(pend)
      if (connected && status.present) {
        const [p, t] = await Promise.all([plugin.presence(), plugin.tasks.list()])
        setPresence(p.agents)
        setDelegations(p.delegations)
        setTasks(t)
      } else {
        setPresence([])
        setDelegations([])
        setTasks([])
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  useEffect(() => {
    void refresh()
    const clock = setInterval(() => setTick((n) => n + 1), 10_000)
    const offEvents = api.onGatewayEvent(({ event, payload }) => {
      if (event === 'clawhq.presence' && payload) {
        setPresence((payload.agents as Presence[]) ?? [])
        setDelegations((payload.delegations as Delegation[]) ?? [])
      } else if (event === 'clawhq.tasks.changed' || event === 'clawhq.notice' || event === 'clawhq.activity') {
        void refresh()
      }
    })
    const offNotice = api.onNodeNotification(() => void refresh())
    const offNode = api.onNodeStatus(() => void refresh())
    return () => {
      clearInterval(clock)
      offEvents()
      offNotice()
      offNode()
    }
  }, [refresh])

  const presenceOf = useMemo(() => new Map(presence.map((p) => [p.agentId, p])), [presence])
  const activeSessions = useMemo(() => new Set(sessions.filter((s) => s.hasActiveRun).map((s) => s.agentId)), [sessions])

  const desks = useMemo<Map<string, Desk>>(() => {
    const out = new Map<string, Desk>()
    for (const agent of agents) {
      const p = presenceOf.get(agent.id)
      const waiting = notices.filter((n) => !n.read && n.agentId === agent.id).length + (pendingByAgent[agent.id] ?? 0)
      const working = p ? p.state === 'working' : activeSessions.has(agent.id)
      let state: DeskState = 'idle'
      if (waiting > 0) state = 'waiting'
      else if (working) state = 'working'
      else if (p && p.lastSuccess === false) state = 'failed'
      let bubble = ''
      if (working && p?.tool) bubble = prettyTool(p.tool)
      else if (working) bubble = 'thinking…'
      else if (waiting > 0) bubble = 'needs you'
      else if (p?.lastLine) bubble = p.lastLine
      out.set(agent.id, {
        agent,
        state,
        bubble,
        since: working ? p?.sinceMs : p?.lastEndMs,
        presence: p,
        waiting,
        openTasks: tasks.filter((t) => t.agentId === agent.id && (t.status === 'todo' || t.status === 'doing')).length
      })
    }
    return out
    // tick keeps the "since" ages moving.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agents, presenceOf, activeSessions, notices, pendingByAgent, tasks, tick])

  const rooms = useMemo(() => {
    const depts = [...(config?.departments ?? [])].sort((a, b) => a.order - b.order)
    const list = depts.map((d) => ({ id: d.id, name: d.name, emoji: d.emoji, agents: agents.filter((a) => config?.assignments?.[a.id] === d.id) }))
    const lobby = agents.filter((a) => !config?.assignments?.[a.id] || !depts.some((d) => d.id === config?.assignments?.[a.id]))
    if (lobby.length) list.push({ id: UNASSIGNED, name: 'Lobby', emoji: '🛋️', agents: lobby })
    return list
  }, [agents, config])

  // ---- delegation lines: from a parent desk to its child's desk -------------
  const floorRef = useRef<HTMLDivElement | null>(null)
  const deskRefs = useRef<Map<string, HTMLButtonElement>>(new Map())
  const [lines, setLines] = useState<{ key: string; x1: number; y1: number; x2: number; y2: number; label?: string }[]>([])
  const measure = useCallback(() => {
    const floor = floorRef.current
    if (!floor) return
    const fr = floor.getBoundingClientRect()
    const center = (id: string) => {
      const el = deskRefs.current.get(id)
      if (!el) return null
      const r = el.getBoundingClientRect()
      return { x: r.left + r.width / 2 - fr.left + floor.scrollLeft, y: r.top + r.height / 2 - fr.top + floor.scrollTop }
    }
    const next: typeof lines = []
    for (const d of delegations) {
      if (!d.parentAgentId || d.parentAgentId === d.childAgentId) continue
      const a = center(d.parentAgentId)
      const b = center(d.childAgentId)
      if (a && b) next.push({ key: d.childSessionKey, x1: a.x, y1: a.y, x2: b.x, y2: b.y, label: d.label })
    }
    setLines(next)
  }, [delegations])
  useLayoutEffect(() => {
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [measure, rooms, desks])

  // ---- the selected agent's card --------------------------------------------
  useEffect(() => {
    if (!selected || !connected) return
    let live = true
    const startOfDay = new Date()
    startOfDay.setHours(0, 0, 0, 0)
    if (pluginPresent) {
      plugin
        .activity({ agentId: selected, sinceMs: startOfDay.getTime(), limit: 100 })
        .then((r) => live && setRuns(r))
        .catch(() => undefined)
    } else {
      setRuns([])
    }
    api.rpc
      .request<{ entries?: { name: string; kind?: string; updatedAtMs?: number }[] }>('agents.workspace.list', { agentId: selected })
      .then((res) => {
        if (!live) return
        const at = Math.max(0, ...(res?.entries ?? []).filter((e) => e.name === 'MEMORY.md' || e.name === 'memory').map((e) => e.updatedAtMs ?? 0))
        setMemoryAt((m) => ({ ...m, [selected]: at }))
      })
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [selected, connected, pluginPresent, tick])

  const move = async (agentId: string, departmentId: string): Promise<void> => {
    try {
      onConfigChanged(await api.config.assignAgent(agentId, departmentId || null))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const working = [...desks.values()].filter((d) => d.state === 'working').length
  const waiting = [...desks.values()].reduce((n, d) => n + d.waiting, 0)
  const sel = selected ? desks.get(selected) : undefined
  const selMain = selected ? sessions.find((s) => s.key === `agent:${selected}:main`) : undefined
  const ctxPct = selMain?.contextTokens && selMain.totalTokens ? Math.min(100, Math.round((selMain.totalTokens / selMain.contextTokens) * 100)) : null
  const selTasks = selected ? tasks.filter((t) => t.agentId === selected && t.status !== 'done') : []

  return (
    <Shell shell={shell} title="Office">
      <ContentHead
        title="Office"
        subtitle={
          !connected
            ? 'Not connected to a gateway.'
            : `${agents.length} agents · ${working} working · ${waiting} ${waiting === 1 ? 'thing needs' : 'things need'} you${pluginPresent === false ? ' · live detail needs the ClawHQ plugin' : ''}`
        }
      >
        <span className="office-legend">
          <i className="desk-dot is-working" /> working <i className="desk-dot is-waiting" /> needs you <i className="desk-dot is-failed" /> failed <i className="desk-dot is-idle" /> idle
        </span>
      </ContentHead>

      <div className="office-body">
        <div className="office-floor" ref={floorRef}>
          <svg className="office-lines" aria-hidden="true">
            {lines.map((l) => (
              <g key={l.key}>
                <line x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} />
                <circle cx={l.x2} cy={l.y2} r="4" />
              </g>
            ))}
          </svg>
          {rooms.length === 0 && machines.length === 0 && <p className="thread-empty">No agents yet.</p>}
          {machines.length > 0 && (
            <section className="room room-machines">
              <h2 className="room-name">
                <span>🖥️</span> Machines
                <span className="plugin-desc">{machines.filter((m) => m.connected).length} of {machines.length} online</span>
              </h2>
              <div className="room-desks">
                {machines.map((m) => (
                  <div key={m.nodeId} className={`desk is-machine${m.connected ? '' : ' is-offline'}`} title={m.nodeId}>
                    <span className="desk-avatar">
                      {m.platform === 'linux' ? '🐧' : m.platform === 'windows' ? '🪟' : m.platform === 'macos' || m.platform === 'darwin' ? '🍎' : '🖥️'}
                      <i className={`desk-dot ${m.connected ? 'is-online' : 'is-idle'}`} />
                    </span>
                    <span className="desk-name">
                      <span className="host-glyph" title={hostLabel(hostKind(m))}>{hostGlyph(hostKind(m))}</span>{' '}
                      {m.displayName || m.platform || m.nodeId.slice(0, 8)}
                    </span>
                    <span className="desk-meta">
                      {hostLabel(hostKind(m))} · {m.connected ? 'online' : 'offline'} · {(m.commands ?? []).length} commands
                    </span>
                  </div>
                ))}
              </div>
            </section>
          )}
          {rooms.map((room) => (
            <section key={room.id} className="room">
              <h2 className="room-name">
                <span>{room.emoji}</span> {room.name}
                <span className="plugin-desc">{room.agents.length}</span>
              </h2>
              <div className="room-desks">
                {room.agents.map((agent) => {
                  const d = desks.get(agent.id)!
                  return (
                    <button
                      key={agent.id}
                      ref={(el) => {
                        if (el) deskRefs.current.set(agent.id, el)
                        else deskRefs.current.delete(agent.id)
                      }}
                      className={`desk is-${d.state}${selected === agent.id ? ' is-selected' : ''}`}
                      onClick={() => setSelected(selected === agent.id ? null : agent.id)}
                      title={`${agentLabel(agent)} · ${d.state}`}
                    >
                      <span className="desk-avatar">
                        {agentEmoji(agent)}
                        <i className={`desk-dot is-${d.state}`} />
                      </span>
                      <span className="desk-name">{agentLabel(agent)}</span>
                      {d.bubble && <span className={`desk-bubble${d.state === 'working' ? ' is-live' : ''}`}>{d.bubble}</span>}
                      <span className="desk-meta">
                        {d.state === 'working' && d.since ? `for ${ago(d.since)}` : d.since ? `${ago(d.since)} ago` : ''}
                        {d.openTasks > 0 ? ` · ${d.openTasks} task${d.openTasks === 1 ? '' : 's'}` : ''}
                      </span>
                    </button>
                  )
                })}
                {room.agents.length === 0 && <span className="plugin-desc">Empty room</span>}
              </div>
            </section>
          ))}
        </div>

        {sel && (
          <aside className="office-card">
            <header className="office-card-head">
              <span className="desk-avatar">{agentEmoji(sel.agent)}</span>
              <div>
                <strong>{agentLabel(sel.agent)}</strong>
                <div className="plugin-desc">
                  {sel.agent.id} · {sel.state}
                  {sel.presence?.tool && sel.state === 'working' ? ` · ${prettyTool(sel.presence.tool)}` : ''}
                </div>
              </div>
              <button className="icon-btn" onClick={() => setSelected(null)} title="Close">
                ✕
              </button>
            </header>
            <div className="btn-row">
              <button className="btn btn-sm btn-primary" onClick={() => onOpenAgent(sel.agent.id, sel.presence?.sessionKey)}>
                Open chat
              </button>
              <select value={config?.assignments?.[sel.agent.id] ?? ''} onChange={(e) => void move(sel.agent.id, e.target.value)} aria-label="Department">
                <option value="">Lobby</option>
                {(config?.departments ?? []).map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.emoji} {d.name}
                  </option>
                ))}
              </select>
            </div>

            <dl className="kv office-kv">
              <dt>Context</dt>
              <dd>
                {ctxPct === null ? (
                  'unknown'
                ) : (
                  <>
                    <span className={`meter${ctxPct > 80 ? ' is-bad' : ctxPct > 60 ? ' is-warn' : ''}`}>
                      <i style={{ width: `${ctxPct}%` }} />
                    </span>
                    {ctxPct}% of {Math.round((selMain?.contextTokens ?? 0) / 1000)}k
                  </>
                )}
              </dd>
              <dt>Memory</dt>
              <dd>{memoryAt[sel.agent.id] ? `updated ${ago(memoryAt[sel.agent.id])} ago` : 'no memory files'}</dd>
              <dt>Last said</dt>
              <dd className="office-lastline">{sel.presence?.lastLine || '–'}</dd>
              <dt>Tasks</dt>
              <dd>
                {selTasks.length === 0 ? 'none open' : selTasks.map((t) => `${t.status === 'doing' ? '▶ ' : ''}${t.title}`).join(' · ')}
              </dd>
            </dl>

            <h3 className="office-h3">Today</h3>
            {runs.length === 0 && <p className="field-hint">{pluginPresent ? 'No runs yet today.' : 'Run history needs the ClawHQ plugin.'}</p>}
            <div className="office-runs">
              {runs.slice(0, 20).map((r) => (
                <button key={r.id} className={`home-row is-${r.success ? 'ok' : 'bad'}`} onClick={() => onOpenAgent(sel.agent.id, r.sessionKey)}>
                  <span className="home-main">
                    <span className="home-title">{r.success ? 'Finished' : 'Failed'}</span>
                    <span className="home-detail">{r.summary || r.error || ''}</span>
                  </span>
                  <span className="home-time">{ago(r.atMs)} ago</span>
                </button>
              ))}
            </div>
          </aside>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
    </Shell>
  )
}
