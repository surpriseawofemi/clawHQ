import { useMemo, useState } from 'react'
import type { Agent, ClawHQConfig, SessionInfo } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'
import { SideHead } from './layout/Shell'

type Props = {
  agents: Agent[]
  sessions: SessionInfo[]
  config: ClawHQConfig | null
  connected: boolean
  selectedAgentId: string | null
  onSelect: (agentId: string) => void
  onAgentSettings: (agentId: string) => void
}

type Group = {
  id: string
  name: string
  emoji: string
  order: number
  agents: Agent[]
}

/**
 * The Chat page's side panel: agents grouped by department, collapsible, with a
 * pulsing dot on the ones with a run in flight. Everything global (search, bell,
 * desktops, gateway, settings) lives in the top bar, not here.
 */
export function AgentSidebar({ agents, sessions, config, connected, selectedAgentId, onSelect, onAgentSettings }: Props): React.JSX.Element {
  // Collapsed groups are remembered per machine.
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const raw = window.localStorage.getItem('clawhq.sidebar.collapsed')
      return new Set<string>(raw ? (JSON.parse(raw) as string[]) : [])
    } catch {
      return new Set<string>()
    }
  })
  const toggleGroup = (id: string): void => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      try {
        window.localStorage.setItem('clawhq.sidebar.collapsed', JSON.stringify([...next]))
      } catch {
        /* per-machine convenience only */
      }
      return next
    })
  }

  /** Agents grouped by department, with an "Unassigned" bucket that hides when empty. */
  const groups = useMemo<Group[]>(() => {
    const departments = config?.departments ?? []
    const assignments = config?.assignments ?? {}
    const byId = new Map<string, Group>(departments.map((d) => [d.id, { ...d, agents: [] as Agent[] }]))
    const unassigned: Group = { id: UNASSIGNED, name: 'Unassigned', emoji: '📥', order: Number.MAX_SAFE_INTEGER, agents: [] }
    for (const agent of agents) {
      const deptId = assignments[agent.id]
      const group = deptId ? byId.get(deptId) : undefined
      ;(group ?? unassigned).agents.push(agent)
    }
    const result = [...byId.values()].filter((g) => g.agents.length > 0)
    if (unassigned.agents.length > 0) result.push(unassigned)
    return result.sort((a, b) => a.order - b.order)
  }, [agents, config])

  const activeAgents = useMemo(() => {
    const set = new Set<string>()
    for (const s of sessions) if (s.hasActiveRun) set.add(s.agentId)
    return set
  }, [sessions])

  return (
    <>
      <SideHead>Agents</SideHead>
      <nav className="agent-list">
        {groups.length === 0 && <p className="sidebar-empty">{connected ? 'No agents found on this gateway.' : 'Not connected.'}</p>}

        {groups.map((group) => (
          <section key={group.id} className={`dept${collapsed.has(group.id) ? ' is-collapsed' : ''}`}>
            <h2 className="dept-title">
              <button
                className="dept-toggle"
                onClick={() => toggleGroup(group.id)}
                aria-expanded={!collapsed.has(group.id)}
                title={collapsed.has(group.id) ? 'Expand' : 'Collapse'}
              >
                <span className="dept-emoji">{group.emoji}</span>
                {group.name}
                <span className="dept-count">{group.agents.length}</span>
                <span className="dept-chevron" aria-hidden="true">
                  ›
                </span>
              </button>
            </h2>
            {!collapsed.has(group.id) &&
              group.agents.map((agent) => {
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
      </nav>
    </>
  )
}
