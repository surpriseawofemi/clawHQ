import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { AgentHeader, type AgentTab } from './AgentHeader'
import type { Agent, SessionInfo } from '../types'

type Entry = {
  path: string
  name: string
  kind: string
  size?: number
  updatedAtMs?: number
}

const when = (ms?: number): string => {
  if (!ms) return 'never'
  const diff = Date.now() - ms
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return `${Math.round(diff / 86_400_000)}d ago`
}

const sessionTitle = (s: SessionInfo): string => {
  if (s.key.endsWith(':superboss')) return 'Super Boss Chat'
  if (s.isMain || s.key.endsWith(':main')) return 'Main thread'
  if (s.label) return s.label
  return s.key.split(':').slice(2).join(':') || s.key
}

type Props = {
  agent: Agent
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  sessions: SessionInfo[]
  connected: boolean
  onOpenSession: (key: string | null) => void
}

/**
 * What an agent has been doing: its threads by recency and the files it touched,
 * from session timestamps and workspace modification times. No extra bookkeeping
 * on the gateway; both are already there.
 */
export function AgentActivity({ agent, tab, onTab, sessions, connected, onOpenSession }: Props): React.JSX.Element {
  const [files, setFiles] = useState<Entry[]>([])
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!connected) return
    try {
      const root = await api.rpc.request<{ entries?: Entry[] }>('agents.workspace.list', { agentId: agent.id })
      const entries = (root?.entries ?? []).filter((e) => e.kind === 'file' && !e.name.startsWith('.'))
      // memory/ holds the daily notes, which are the best trace of a working day.
      try {
        const mem = await api.rpc.request<{ entries?: Entry[] }>('agents.workspace.list', {
          agentId: agent.id,
          path: 'memory'
        })
        entries.push(...(mem?.entries ?? []).filter((e) => e.kind === 'file'))
      } catch {
        /* no memory folder */
      }
      entries.sort((a, b) => (b.updatedAtMs ?? 0) - (a.updatedAtMs ?? 0))
      setFiles(entries.slice(0, 20))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [agent.id, connected])

  useEffect(() => {
    void load()
  }, [load])

  const mine = sessions
    .filter((s) => s.agentId === agent.id && !s.key.includes(':cron:'))
    .sort((a, b) => (b.lastActivityAt ?? b.updatedAt ?? 0) - (a.lastActivityAt ?? a.updatedAt ?? 0))

  return (
    <main className="chat">
      <AgentHeader agent={agent} tab={tab} onTab={onTab} working={mine.some((s) => s.hasActiveRun)} />
      <div className="docs-body">
        <div className="activity-grid">
          <section className="panel">
            <h3>Threads</h3>
            {mine.length === 0 && <p className="field-hint">No conversations yet.</p>}
            <div className="gw-list">
              {mine.map((s) => (
                <div key={s.key} className="gw-row">
                  <i className={`dot ${s.hasActiveRun ? 'dot-ok' : 'dot-off'}`} />
                  <span className="gw-meta">
                    <span className="gw-name">{sessionTitle(s)}</span>
                    <span className="gw-url mono">
                      {s.hasActiveRun ? 'working now' : `last activity ${when(s.lastActivityAt ?? s.updatedAt)}`}
                      {s.totalTokens ? ` · ${Math.round(s.totalTokens / 1000)}k tokens` : ''}
                    </span>
                  </span>
                  <button className="btn btn-sm" onClick={() => onOpenSession(s.key.endsWith(':superboss') ? null : s.key)}>
                    Open
                  </button>
                </div>
              ))}
            </div>
          </section>

          <section className="panel">
            <h3>Recently changed files</h3>
            {files.length === 0 && <p className="field-hint">Nothing changed yet.</p>}
            <div className="gw-list">
              {files.map((f) => (
                <div key={f.path} className="gw-row">
                  <span className="gw-meta">
                    <span className="gw-name">{f.path}</span>
                    <span className="gw-url mono">
                      {when(f.updatedAtMs)}
                      {f.size ? ` · ${Math.max(1, Math.round(f.size / 1024))} KB` : ''}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          </section>
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </main>
  )
}
