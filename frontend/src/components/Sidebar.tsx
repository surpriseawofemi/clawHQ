import { useMemo, useState } from 'react'
import type { Agent, ClawHQConfig, SessionInfo } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'
import { SideHead } from './layout/Shell'
import { getPrefs, setPref } from '../prefs'

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

const ago = (ms: number): string => {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return 'now'
  if (s < 3600) return `${Math.round(s / 60)}m`
  if (s < 86_400) return `${Math.round(s / 3600)}h`
  return `${Math.round(s / 86_400)}d`
}

/**
 * The Chat page's side panel: agents, by default ordered by who spoke last and
 * grouped under their departments (both switchable and remembered per machine),
 * with a pulsing dot on the ones with a run in flight. Everything global (search,
 * bell, desktops, gateway, settings) lives in the top bar, not here.
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

  const [sort, setSort] = useState(getPrefs().sidebarSort)
  const [grouped, setGrouped] = useState(getPrefs().sidebarGroup)

  /** When each agent last said or heard anything: the newest of its sessions, automations aside. */
  const lastAt = useMemo(() => {
    const m = new Map<string, number>()
    for (const s of sessions) {
      if (!s.agentId || s.key.includes(':cron:')) continue
      const at = s.updatedAt ?? 0
      if (at > (m.get(s.agentId) ?? 0)) m.set(s.agentId, at)
    }
    return m
  }, [sessions])

  const rosterIndex = useMemo(() => new Map(agents.map((a, i) => [a.id, i])), [agents])
  const orderAgents = (list: Agent[]): Agent[] =>
    [...list].sort((a, b) => {
      if (sort === 'recent') return (lastAt.get(b.id) ?? 0) - (lastAt.get(a.id) ?? 0) || agentLabel(a).localeCompare(agentLabel(b))
      if (sort === 'name') return agentLabel(a).localeCompare(agentLabel(b))
      return (rosterIndex.get(a.id) ?? 0) - (rosterIndex.get(b.id) ?? 0)
    })

  /** Agents grouped by department, with an "Unassigned" bucket that hides when empty. */
  const groups = useMemo<Group[]>(() => {
    if (!grouped) return agents.length ? [{ id: 'all', name: 'Agents', emoji: '', order: 0, agents: orderAgents(agents) }] : []
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
    for (const g of result) g.agents = orderAgents(g.agents)
    // With "recent", the department that spoke last floats up as well.
    if (sort === 'recent') {
      const newest = (g: Group): number => Math.max(0, ...g.agents.map((a) => lastAt.get(a.id) ?? 0))
      return result.sort((a, b) => newest(b) - newest(a) || a.order - b.order)
    }
    return result.sort((a, b) => a.order - b.order)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sort, grouped, lastAt, rosterIndex, agents, config])

  const activeAgents = useMemo(() => {
    const set = new Set<string>()
    for (const s of sessions) if (s.hasActiveRun) set.add(s.agentId)
    return set
  }, [sessions])

  return (
    <>
      <SideHead>
        <span className="side-title">Agents</span>
        <span className="sidebar-tools">
          <select
            value={sort}
            onChange={(e) => {
              const v = e.target.value as typeof sort
              setSort(v)
              setPref('sidebarSort', v)
            }}
            aria-label="Sort agents"
            title="Order of agents"
          >
            <option value="recent">Last message</option>
            <option value="name">Name</option>
            <option value="department">Department order</option>
          </select>
          <button
            className={`icon-btn group-toggle${grouped ? ' is-on' : ''}`}
            onClick={() => {
              setGrouped(!grouped)
              setPref('sidebarGroup', !grouped)
            }}
            title={grouped ? 'Grouped by department: click for one flat list' : 'Flat list: click to group by department'}
            aria-pressed={grouped}
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M4 6h16M4 12h10M4 18h16" />
            </svg>
          </button>
        </span>
      </SideHead>
      <nav className="agent-list">
        {groups.length === 0 && <p className="sidebar-empty">{connected ? 'No agents found on this gateway.' : 'Not connected.'}</p>}

        {groups.map((group) => (
          <section key={group.id} className={`dept${collapsed.has(group.id) ? ' is-collapsed' : ''}${group.id === 'all' ? ' is-flat' : ''}`}>
            {group.id !== 'all' && (
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
            )}
            {(group.id === 'all' || !collapsed.has(group.id)) &&
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
                      <span className="agent-sub">
                        {agent.id}
                        {lastAt.get(agent.id) ? <span className="agent-when"> · {ago(lastAt.get(agent.id) as number)}</span> : null}
                      </span>
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
