import { useEffect, useMemo, useState } from 'react'
import type { Agent, ClawHQConfig, ConnectionStatus, NodeStatus, RemoteNode, SessionInfo } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'
import { api } from '../api'
import { Logo } from './Logo'

type Props = {
  agents: Agent[]
  sessions: SessionInfo[]
  config: ClawHQConfig | null
  selectedAgentId: string | null
  onSelect: (agentId: string) => void
  onAgentSettings: (agentId: string) => void
  onOpenSettings: () => void
  onOpenSwitcher: () => void
  status: ConnectionStatus
  settingsOpen: boolean
  desktops: RemoteNode[]
  desktopsOpen: boolean
  onToggleDesktops: () => void
  unreadNotices: number
  onOpenNotifications: () => void
  onOpenSearch: () => void
}

/** One line for the gateway link, one for this machine's node role. */
function connectionLines(
  status: ConnectionStatus,
  node: NodeStatus | null,
  gatewayName: string | null
): { dot: string; main: string; sub: string } {
  const name = gatewayName ?? 'Gateway'
  let dot = 'dot-off'
  let main = 'Disconnected'
  if (status.phase === 'connected') {
    dot = 'dot-ok'
    main = name
  } else if (status.phase === 'connecting' && status.paired) {
    dot = 'dot-warn'
    main = `Reconnecting to ${name}…`
  } else if (status.phase === 'connecting') {
    dot = 'dot-warn'
    main = `Connecting to ${name}…`
  } else if (status.phase === 'pending') {
    dot = 'dot-warn'
    main = 'Waiting for approval'
  }

  let sub = ''
  if (node && node.enabled) {
    if (node.connected) sub = 'this Mac is a node'
    else if (node.pairing === 'awaiting-approval') sub = 'node: pairing…'
    else if (node.pairing === 'reconnecting') sub = 'node: reconnecting…'
    else if (node.pairing === 'connecting') sub = 'node: connecting…'
    else sub = 'node: waiting for gateway'
  } else if (node) {
    sub = 'node role off'
  }
  return { dot, main, sub }
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
  onOpenSwitcher,
  status,
  settingsOpen,
  desktops,
  desktopsOpen,
  onToggleDesktops,
  unreadNotices,
  onOpenNotifications,
  onOpenSearch
}: Props): React.JSX.Element {
  const desktopsOnline = desktops.filter((d) => d.connected).length
  const connected = status.phase === 'connected'
  const [node, setNode] = useState<NodeStatus | null>(null)
  useEffect(() => {
    api.node
      .status()
      .then(setNode)
      .catch(() => undefined)
    return api.onNodeStatus(setNode)
  }, [])
  const gatewayName = config?.gateways.find((g) => g.id === status.gatewayId)?.name ?? null
  const lines = connectionLines(status, node, gatewayName)

  // Collapsed groups are remembered per machine; a group id is a department id or
  // the desktops bucket.
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
        <button className="desk-btn" onClick={onOpenSearch} title="Search (⌘K)" aria-label="Search">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
        </button>
        <button
          className="desk-btn bell-btn"
          onClick={onOpenNotifications}
          title={unreadNotices > 0 ? `${unreadNotices} unread notification${unreadNotices === 1 ? '' : 's'}` : 'Notifications'}
          aria-label="Notifications"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M6 9a6 6 0 0 1 12 0c0 6 2 7 2 7H4s2-1 2-7" />
            <path d="M10 20a2 2 0 0 0 4 0" />
          </svg>
          {unreadNotices > 0 && <i className="desk-badge is-online">{unreadNotices}</i>}
        </button>
        <button
          className={`desk-btn${desktopsOpen ? ' is-active' : ''}`}
          onClick={onToggleDesktops}
          title={
            desktops.length === 0
              ? 'No desktops paired yet'
              : `${desktopsOnline} of ${desktops.length} desktop${desktops.length === 1 ? '' : 's'} online`
          }
          aria-label="Desktops"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="3" y="4" width="18" height="12" rx="2" />
            <path d="M8 20h8M12 16v4" />
          </svg>
          {desktops.length > 0 && (
            <i className={`desk-badge${desktopsOnline > 0 ? ' is-online' : ''}`}>{desktops.length}</i>
          )}
        </button>
      </header>

      <nav className="agent-list">
        {groups.length === 0 && (
          <p className="sidebar-empty">
            {connected ? 'No agents found on this gateway.' : 'Not connected.'}
          </p>
        )}

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

      <footer className="sidebar-foot">
        <button className="conn-pill" onClick={onOpenSwitcher} title="Switch or rename gateway">
          <i className={`dot ${lines.dot}`} />
          <span className="conn-lines">
            <span>{lines.main}</span>
            {lines.sub && <span className="conn-sub">{lines.sub}</span>}
          </span>
          <span className="conn-chevron">⌃</span>
        </button>
        <button
          className={`conn-gear${settingsOpen ? ' is-active' : ''}`}
          onClick={onOpenSettings}
          title="Settings"
          aria-label="Settings"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
          </svg>
        </button>
      </footer>
    </aside>
  )
}
