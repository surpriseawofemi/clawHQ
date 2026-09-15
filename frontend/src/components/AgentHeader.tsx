import type { ReactNode } from 'react'
import type { Agent } from '../types'
import { agentEmoji, agentLabel } from '../types'

export type AgentTab = 'chat' | 'documents' | 'activity' | 'charter'

const TABS: { id: AgentTab; label: string }[] = [
  { id: 'chat', label: 'Chat' },
  { id: 'documents', label: 'Documents' },
  { id: 'activity', label: 'Activity' },
  { id: 'charter', label: 'Charter' }
]

type Props = {
  agent: Agent
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  /** Whether the agent has a run in flight, shown as a pulsing dot. */
  working?: boolean
  /** Extra controls on the right (session picker, settings). */
  children?: ReactNode
}

/** The strip at the top of an agent's pane: identity on the left, tabs, then whatever the view adds. */
export function AgentHeader({ agent, tab, onTab, working, children }: Props): React.JSX.Element {
  return (
    <header className="chat-head">
      <span className="chat-avatar">{agentEmoji(agent)}</span>
      <div className="chat-title">
        <h1>
          {agentLabel(agent)}
          {working && <i className="dot-active" title="Working" />}
        </h1>
        <p>
          {agent.model?.primary ?? 'default model'}
          {agent.identity?.theme ? ` · ${agent.identity.theme}` : ''}
        </p>
      </div>
      {children}
      {/* Last, pinned to the right edge, so the tabs sit in the same place on every view. */}
      <nav className="agent-tabs" aria-label="Agent views">
        {TABS.map((t) => (
          <button key={t.id} className={`agent-tab${tab === t.id ? ' is-active' : ''}`} onClick={() => onTab(t.id)}>
            {t.label}
          </button>
        ))}
      </nav>
    </header>
  )
}
