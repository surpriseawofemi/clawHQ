import { useEffect, useState } from 'react'
import { api } from '../api'
import { ContentHead, Shell, SideHead, type ShellProps } from './layout/Shell'
import type { Agent, SessionInfo } from '../types'

export type Page = 'menu' | 'chat' | 'team' | 'issues' | 'activity' | 'office' | 'tasks' | 'digest' | 'servers' | 'settings'

type Props = {
  shell: ShellProps
  agents: Agent[]
  sessions: SessionInfo[]
  onOpen: (page: Page) => void
}

type Entry = {
  page: Page | null
  title: string
  blurb: string
  icon: React.ReactNode
  soon?: boolean
}

const ENTRIES: Entry[] = [
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
    page: 'team',
    title: 'Team Chat',
    blurb: 'One room for you and every agent. @name asks that agent, @all asks everyone, no @ just tells the room.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M4 5h11a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H9l-4 3v-3H4a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z" />
        <path d="M17 9h3a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-1v3l-3-3h-3" />
      </svg>
    )
  },
  {
    page: 'issues',
    title: 'Issues',
    blurb: 'What agents need from you, one at a time: questions, approvals, problems. Plus tasks you hand out.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 8v5M12 16h.01" />
      </svg>
    )
  },
  {
    page: 'office',
    title: 'Office',
    blurb: 'The floor plan: who is busy, who is idle, who needs you, who is delegating to whom.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3" y="7" width="18" height="14" rx="2" />
        <path d="M8 7V4h8v3M3 13h18" />
      </svg>
    )
  },
  {
    page: 'tasks',
    title: 'Tasks',
    blurb: 'A board every agent can see. Assign, run, and read the result on the card.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="5" height="16" rx="1" />
        <rect x="10" y="4" width="5" height="10" rx="1" />
        <rect x="17" y="4" width="4" height="7" rx="1" />
      </svg>
    )
  },
  {
    page: 'servers',
    title: 'Servers',
    blurb: 'Your machines over SSH: health, Claude Code installed and logged in, and a terminal. No OpenClaw needed.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="18" height="7" rx="2" />
        <rect x="3" y="13" width="18" height="7" rx="2" />
        <path d="M7 7.5h.01M7 16.5h.01" />
      </svg>
    )
  },
  {
    page: 'digest',
    title: 'Digest',
    blurb: 'One day rolled up: every run, task, ask, command, automation and its cost.',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M6 3h9l4 4v14H6z" />
        <path d="M9 12h7M9 16h7M9 8h3" />
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

/**
 * The front door: the menu in the side panel, a glance at the org in the content.
 * Every other page comes back here with the top bar's back button.
 */
export function MainMenu({ shell, agents, sessions, onOpen }: Props): React.JSX.Element {
  const [pendingCommands, setPendingCommands] = useState(0)
  useEffect(() => {
    api.node
      .status()
      .then((s) => setPendingCommands(s.pendingExec?.length ?? 0))
      .catch(() => undefined)
    return api.onNodeStatus((s) => setPendingCommands(s.pendingExec?.length ?? 0))
  }, [])

  const connected = shell.status.phase === 'connected'
  const gatewayName = shell.config?.gateways.find((g) => g.id === shell.status.gatewayId)?.name ?? 'gateway'
  const working = sessions.filter((s) => s.hasActiveRun).length
  const needsYou = shell.unreadNotices + pendingCommands

  const side = (
    <>
      <SideHead>Menu</SideHead>
      <nav className="menu-list" aria-label="Main menu">
        {ENTRIES.map((e) => (
          <button key={e.title} className={`menu-item${e.soon ? ' is-soon' : ''}`} disabled={e.soon} onClick={() => e.page && onOpen(e.page)}>
            <span className="menu-icon">{e.icon}</span>
            <span className="menu-label">
              {e.title}
              {e.soon && <span className="menu-soon">soon</span>}
              {e.page === 'activity' && needsYou > 0 && <span className="badge">{needsYou}</span>}
            </span>
          </button>
        ))}
      </nav>
    </>
  )

  return (
    <Shell shell={shell} title={null} side={side}>
      <ContentHead title="Your agent org" subtitle={connected ? `${agents.length} agents on ${gatewayName}` : 'Connect to a gateway to see your agents'} />
      <div className="content-body">
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
            <strong>{shell.config?.departments.length ?? 0}</strong>
            <span>departments</span>
          </button>
        </div>
        <div className="menu-cards">
          {ENTRIES.map((e) => (
            <button key={e.title} className={`menu-card${e.soon ? ' is-soon' : ''}`} disabled={e.soon} onClick={() => e.page && onOpen(e.page)}>
              <span className="menu-icon">{e.icon}</span>
              <span className="menu-card-title">
                {e.title}
                {e.soon && <span className="menu-soon">soon</span>}
              </span>
              <span className="menu-card-blurb">{e.blurb}</span>
            </button>
          ))}
        </div>
      </div>
    </Shell>
  )
}
