import { useMemo } from 'react'
import type { Agent, ClawHQConfig, RemoteNode, SessionInfo } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'
import { Logo } from './Logo'

type Props = {
  agents: Agent[]
  sessions: SessionInfo[]
  config: ClawHQConfig | null
  selectedAgentId: string | null
  onSelect: (agentId: string) => void
  onAgentSettings: (agentId: string) => void
  onOpenSettings: () => void
  connected: boolean
  serverVersion: string | null
  desktops: RemoteNode[]
  selectedNodeId: string | null
  onSelectDesktop: (nodeId: string) => void
}

type Group = {
  id: string
  name: string
  emoji: string
  order: number
  agents: Agent[]
}

export function Sidebar({
  agents,
  sessions,
  config,
  selectedAgentId,
  onSelect,
  onAgentSettings,
  onOpenSettings,
  connected,
  serverVersion,
  desktops,
  selectedNodeId,
  onSelectDesktop
}: Props): React.JSX.Element {
  /** Agents grouped by department, with an "Unassigned" bucket that hides when empty. */
  const groups = useMemo<Group[]>(() => {
    const departments = config?.departments ?? []
    const assignments = config?.assignments ?? {}

    const byId = new Map<string, Group>(
      departments.map((d) => [d.id, { ...d, agents: [] as Agent[] }])
    )
    const unassigned: Group = {
      id: UNASSIGNED,
      name: 'Unassigned',
      emoji: '📥',
      order: Number.MAX_SAFE_INTEGER,
      agents: []
    }

    for (const agent of agents) {
      const deptId = assignments[agent.id]
      const group = deptId ? byId.get(deptId) : undefined
      ;(group ?? unassigned).agents.push(agent)
    }

    const result = [...byId.values()].filter((g) => g.agents.length > 0)
    if (unassigned.agents.length > 0) result.push(unassigned)
    return result.sort((a, b) => a.order - b.order)
  }, [agents, config])

  /** Agents with a run in flight get a pulsing dot, matching the chat header. */
  const activeAgents = useMemo(() => {
    const set = new Set<string>()
    for (const s of sessions) if (s.hasActiveRun) set.add(s.agentId)
    return set
  }, [sessions])

  return (
    <aside className="sidebar">
      <header className="sidebar-head">
        <span className="brand">
          <Logo size={22} className="brand-mark" />
          <span className="brand-name">ClawHQ</span>
        </span>
      </header>

      <nav className="agent-list">
        {groups.length === 0 && (
          <p className="sidebar-empty">
            {connected ? 'No agents found on this gateway.' : 'Not connected.'}
          </p>
        )}

        {groups.map((group) => (
          <section key={group.id} className="dept">
            <h2 className="dept-title">
              <span className="dept-emoji">{group.emoji}</span>
              {group.name}
              <span className="dept-count">{group.agents.length}</span>
            </h2>
            {group.agents.map((agent) => {
              const selected = agent.id === selectedAgentId
              return (
                <div
                  key={agent.id}
                  className={`agent-row${selected ? ' is-selected' : ''}`}
                  onClick={() => onSelect(agent.id)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') onSelect(agent.id)
                  }}
                >
                  <span className="agent-emoji">{agentEmoji(agent)}</span>
                  <span className="agent-meta">
                    <span className="agent-name">
                      {agentLabel(agent)}
                      {activeAgents.has(agent.id) && <i className="dot-active" title="Working" />}
                    </span>
                    <span className="agent-sub">{agent.id}</span>
                  </span>
                  <button
                    className="icon-btn agent-gear"
                    title={`${agentLabel(agent)} settings`}
                    onClick={(e) => {
                      e.stopPropagation()
                      onAgentSettings(agent.id)
                    }}
                  >
                    ⚙
                  </button>
                </div>
              )
            })}
          </section>
        ))}
        {desktops.length > 0 && (
          <section className="dept">
            <h2 className="dept-title">
              <span className="dept-emoji">🖥️</span>
              Desktops
              <span className="dept-count">{desktops.length}</span>
            </h2>
            {desktops.map((node) => (
              <div
                key={node.nodeId}
                className={`agent-row${node.nodeId === selectedNodeId ? ' is-selected' : ''}`}
                role="button"
                tabIndex={0}
                onClick={() => onSelectDesktop(node.nodeId)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') onSelectDesktop(node.nodeId)
                }}
              >
                <span className="agent-emoji">🖥️</span>
                <span className="agent-meta">
                  <span className="agent-name">
                    {node.displayName || node.platform || 'desktop'}
                    {node.connected && <i className="dot dot-ok" />}
                  </span>
                  <span className="agent-sub">{node.nodeId.slice(0, 12)}</span>
                </span>
              </div>
            ))}
          </section>
        )}
      </nav>

      <footer className="sidebar-foot">
        <button className="conn-pill" onClick={onOpenSettings} title="Open settings">
          <i className={`dot ${connected ? 'dot-ok' : 'dot-off'}`} />
          <span>{connected ? `Gateway ${serverVersion ?? ''}`.trim() : 'Disconnected'}</span>
          <span className="conn-gear">⚙</span>
        </button>
      </footer>
    </aside>
  )
}
