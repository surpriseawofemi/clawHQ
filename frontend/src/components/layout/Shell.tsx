import { useEffect, useState } from 'react'
import { api } from '../../api'
import { effectiveTheme, onTheme, toggleTheme } from '../../theme'
import { Logo } from '../Logo'
import type { ClawHQConfig, ConnectionStatus, NodeStatus, RemoteNode } from '../../types'

/**
 * The page system. Every page is the same frame: one top bar across the window
 * (brand, back to the menu, page name, and the global actions), then either a side
 * panel plus content or content alone. Pages choose the side panel and fill the
 * content; the frame is the same everywhere.
 */

/** What the top bar needs from the app; the same object is handed to every page. */
export type ShellProps = {
  status: ConnectionStatus
  config: ClawHQConfig | null
  desktops: RemoteNode[]
  unreadNotices: number
  onBack: () => void
  onOpenSearch: () => void
  onOpenNotifications: () => void
  onOpenDesktops: () => void
  onOpenSettings: () => void
  onOpenSwitcher: () => void
}

type Props = {
  shell: ShellProps
  /** Page name after the back button; the main menu passes null and gets no back button. */
  title: string | null
  /** Side panel content; omit for a full-width page. */
  side?: React.ReactNode
  children: React.ReactNode
}

/** One line for the gateway link, one for this machine's node role. */
function connectionLines(status: ConnectionStatus, node: NodeStatus | null, gatewayName: string | null): { dot: string; main: string; sub: string } {
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
    if (node.connected) sub = 'this machine is a node'
    else if (node.pairing === 'awaiting-approval') sub = 'node: pairing…'
    else if (node.pairing === 'reconnecting') sub = 'node: reconnecting…'
    else if (node.pairing === 'connecting') sub = 'node: connecting…'
    else sub = 'node: waiting for gateway'
  } else if (node) {
    sub = 'node role off'
  }
  return { dot, main, sub }
}

const Icon = {
  search: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  ),
  bell: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M6 9a6 6 0 0 1 12 0c0 6 2 7 2 7H4s2-1 2-7" />
      <path d="M10 20a2 2 0 0 0 4 0" />
    </svg>
  ),
  desktop: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </svg>
  ),
  /** Two boxes joined: this machine is a node of the gateway. */
  node: (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
      <path d="M10 6.5h4v7.5" />
    </svg>
  ),
  sun: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </svg>
  ),
  moon: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
    </svg>
  ),
  gear: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
    </svg>
  )
}

export function TopBar({ shell, title }: { shell: ShellProps; title: string | null }): React.JSX.Element {
  const { status, config, desktops, unreadNotices } = shell
  const [node, setNode] = useState<NodeStatus | null>(null)
  useEffect(() => {
    api.node
      .status()
      .then(setNode)
      .catch(() => undefined)
    return api.onNodeStatus(setNode)
  }, [])
  // Sun while dark (click for light), moon while light (click for dark).
  const [shown, setShown] = useState<'dark' | 'light'>(effectiveTheme())
  useEffect(() => onTheme((t) => setShown(effectiveTheme(t))), [])
  const gatewayName = config?.gateways.find((g) => g.id === status.gatewayId)?.name ?? null
  const lines = connectionLines(status, node, gatewayName)
  const online = desktops.filter((d) => d.connected).length

  return (
    <header className="topbar">
      <span className="brand">
        <Logo size={22} className="brand-mark" />
        <span className="brand-name">ClawHQ</span>
      </span>
      {title !== null && (
        <>
          <button className="back-btn" onClick={shell.onBack} title="Back to the main menu">
            ‹ Menu
          </button>
          <span className="topbar-title">{title}</span>
        </>
      )}
      <span className="topbar-spacer" />
      <button
        className="conn-pill"
        onClick={shell.onOpenSwitcher}
        title={`Switch or rename gateway${lines.sub ? ` · ${lines.sub}` : ''}`}
      >
        <i className={`dot ${lines.dot}`} />
        {node?.enabled && (
          <span className={`node-mark${node.connected ? ' is-on' : ''}`} aria-label={lines.sub} title={lines.sub}>
            {Icon.node}
          </span>
        )}
        <span className="conn-lines">
          <span>{lines.main}</span>
        </span>
      </button>
      <button className="desk-btn" onClick={shell.onOpenSearch} title="Search (⌘K)" aria-label="Search">
        {Icon.search}
      </button>
      <button
        className="desk-btn"
        onClick={shell.onOpenNotifications}
        title={unreadNotices > 0 ? `${unreadNotices} unread notification${unreadNotices === 1 ? '' : 's'}` : 'Notifications'}
        aria-label="Notifications"
      >
        {Icon.bell}
        {unreadNotices > 0 && <i className="desk-badge is-online">{unreadNotices}</i>}
      </button>
      <button
        className="desk-btn"
        onClick={shell.onOpenDesktops}
        title={desktops.length === 0 ? 'No desktops paired yet' : `${online} of ${desktops.length} desktop${desktops.length === 1 ? '' : 's'} online`}
        aria-label="Desktops"
      >
        {Icon.desktop}
        {online > 0 && <i className="desk-dot-mark" aria-hidden="true" />}
      </button>
      <button
        className="desk-btn"
        onClick={() => setShown(effectiveTheme(toggleTheme()))}
        title={shown === 'dark' ? 'Switch to light' : 'Switch to dark'}
        aria-label={shown === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
      >
        {shown === 'dark' ? Icon.sun : Icon.moon}
      </button>
      <button className="desk-btn" onClick={shell.onOpenSettings} title="Settings" aria-label="Settings">
        {Icon.gear}
      </button>
    </header>
  )
}

export function Shell({ shell, title, side, children }: Props): React.JSX.Element {
  return (
    <div className="shell">
      <TopBar shell={shell} title={title} />
      <div className={`shell-body${side ? ' has-side' : ''}`}>
        {side && <aside className="side-panel">{side}</aside>}
        <main className="shell-main">{children}</main>
      </div>
    </div>
  )
}

/** A section label inside a side panel. */
export function SideHead({ children }: { children: React.ReactNode }): React.JSX.Element {
  return <div className="side-head">{children}</div>
}

/** The row every page's content starts with: a title, optionally a subtitle, then controls on the right. */
export function ContentHead({
  title,
  subtitle,
  children,
  onRefresh,
  refreshing
}: {
  title: React.ReactNode
  subtitle?: React.ReactNode
  children?: React.ReactNode
  /** Shows a refresh button at the right end; pages that poll still offer it for "now". */
  onRefresh?: () => void
  refreshing?: boolean
}): React.JSX.Element {
  return (
    <header className="content-head">
      <div className="content-title">
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {(children || onRefresh) && (
        <div className="content-actions">
          {children}
          {onRefresh && (
            <button className={`desk-btn refresh-btn${refreshing ? ' is-spinning' : ''}`} onClick={onRefresh} title="Refresh now (pages also refresh on their own)" aria-label="Refresh">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M21 12a9 9 0 1 1-2.6-6.4" />
                <path d="M21 3v6h-6" />
              </svg>
            </button>
          )}
        </div>
      )}
    </header>
  )
}
