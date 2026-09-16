import { useEffect, useState } from 'react'
import { api } from '../api'
import { Logo } from './Logo'
import type { Agent, ClawHQConfig, ConnectionStatus, NodeStatus, SessionInfo } from '../types'

export type Page = 'menu' | 'chat' | 'activity' | 'settings'

type Props = {
  status: ConnectionStatus
  config: ClawHQConfig | null
  agents: Agent[]
  sessions: SessionInfo[]
  unreadNotices: number
  onOpen: (page: Page) => void
  onOpenSwitcher: () => void
}

type Entry = {
  page: Page | null
  title: string
  blurb: string
  icon: React.ReactNode
  soon?: boolean
}

/**
 * The front door. Every part of ClawHQ is a page you open from here and come back
 * to with the back button; the sidebar on the left is the menu, and the right side
 * is a glance at the org before you pick one.
 */
export function MainMenu({ status, config, agents, sessions, unreadNotices, onOpen, onOpenSwitcher }: Props): React.JSX.Element {
  const [node, setNode] = useState<NodeStatus | null>(null)
  const [pendingCommands, setPendingCommands] = useState(0)

  useEffect(() => {
    api.node
      .status()
      .then((s) => {
        setNode(s)
        setPendingCommands(s.pendingExec?.length ?? 0)
      })
      .catch(() => undefined)
    return api.onNodeStatus((s) => {
      setNode(s)
      setPendingCommands(s.pendingExec?.length ?? 0)
    })
  }, [])

  const connected = status.phase === 'connected'
  const gatewayName = config?.gateways.find((g) => g.id === status.gatewayId)?.name ?? 'gateway'
  const working = sessions.filter((s) => s.hasActiveRun).length
  const needsYou = unreadNotices + pendingCommands

  const entries: Entry[] = [
    {
      page: 'chat',
      title: 'Chat',
      blurb: 'Talk to any agent, browse threads, documents and charters.',
      icon: (
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 12a8 8 0 0 1-8 8H7l-4 3v-6a8 8 0 0 1 2-5.3A8 8 0 0 1 13 4a8 8 0 0 1 8 8z" />
        </svg>
      )
    },
    {
      page: 'activity',
      title: 'Activity',
      blurb: 'What every agent did, and what needs you, in one timeline.',
      icon: (
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M3 12h4l3-8 4 16 3-8h4" />
        </svg>
      )
    },
    {
      page: null,
      title: 'Office',
      blurb: 'See the org at work: who is busy, who is idle, who talks to whom.',
      soon: true,
      icon: (
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="3" y="7" width="18" height="14" rx="2" />
          <path d="M8 7V4h8v3M3 13h18" />
        </svg>
      )
    },
    {
      page: 'settings',
      title: 'Settings',
      blurb: 'Gateways, this machine, departments, plugins, channels, updates.',
      icon: (
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
        </svg>
      )
    }
  ]

  return (
    <div className="menu-page">
      <aside className="menu-side">
        <header className="sidebar-head">
          <span className="brand">
            <Logo size={22} className="brand-mark" />
            <span className="brand-name">ClawHQ</span>
          </span>
        </header>
        <nav className="menu-list" aria-label="Main menu">
          {entries.map((e) => (
            <button
              key={e.title}
              className={`menu-item${e.soon ? ' is-soon' : ''}`}
              disabled={e.soon}
              onClick={() => e.page && onOpen(e.page)}
            >
              <span className="menu-icon">{e.icon}</span>
              <span className="menu-label">
                {e.title}
                {e.soon && <span className="menu-soon">soon</span>}
                {e.page === 'activity' && needsYou > 0 && <span className="badge">{needsYou}</span>}
              </span>
            </button>
          ))}
        </nav>
        <footer className="sidebar-foot">
          <button className="conn-pill" onClick={onOpenSwitcher} title="Switch or rename gateway">
            <i className={`dot ${connected ? 'dot-ok' : status.phase === 'connecting' ? 'dot-warn' : 'dot-off'}`} />
            <span className="conn-lines">
              <span>{connected ? gatewayName : status.phase === 'connecting' ? 'Connecting…' : 'Not connected'}</span>
              {node?.enabled && <span className="conn-sub">{node.connected ? 'this machine is a node' : 'node connecting…'}</span>}
            </span>
          </button>
        </footer>
      </aside>

      <main className="menu-main">
        <div className="menu-hero">
          <h1>Your agent org</h1>
          <p>
            {connected
              ? `${agents.length} agents on ${gatewayName}.`
              : 'Connect to a gateway to see your agents.'}
          </p>
        </div>
        <div className="menu-stats">
          <button className="menu-stat" onClick={() => onOpen('activity')}>
            <strong>{needsYou}</strong>
            <span>{needsYou === 1 ? 'thing needs you' : 'things need you'}</span>
          </button>
          <button className="menu-stat" onClick={() => onOpen('activity')}>
            <strong>{working}</strong>
            <span>{working === 1 ? 'agent working now' : 'agents working now'}</span>
          </button>
          <button className="menu-stat" onClick={() => onOpen('chat')}>
            <strong>{agents.length}</strong>
            <span>agents</span>
          </button>
          <button className="menu-stat" onClick={() => onOpen('settings')}>
            <strong>{config?.departments.length ?? 0}</strong>
            <span>departments</span>
          </button>
        </div>
        <div className="menu-cards">
          {entries.map((e) => (
            <button
              key={e.title}
              className={`menu-card${e.soon ? ' is-soon' : ''}`}
              disabled={e.soon}
              onClick={() => e.page && onOpen(e.page)}
            >
              <span className="menu-icon">{e.icon}</span>
              <span className="menu-card-title">
                {e.title}
                {e.soon && <span className="menu-soon">soon</span>}
              </span>
              <span className="menu-card-blurb">{e.blurb}</span>
            </button>
          ))}
        </div>
      </main>
    </div>
  )
}
