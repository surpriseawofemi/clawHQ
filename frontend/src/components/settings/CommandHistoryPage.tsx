import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../../api'
import type { ExecRecord } from '../../types'

const when = (ms: number): string => {
  const d = new Date(ms)
  const sameDay = d.toDateString() === new Date().toDateString()
  return sameDay
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** How a decision reads on the page. */
const DECISION: Record<string, { label: string; tone: 'ok' | 'warn' | 'bad' | 'off' }> = {
  allowlisted: { label: 'allowlisted', tone: 'ok' },
  allowed: { label: 'you allowed', tone: 'ok' },
  always: { label: 'you allowed, always', tone: 'ok' },
  'trusted-agent': { label: 'trusted agent', tone: 'ok' },
  'approved-by-gateway': { label: 'approved on the gateway', tone: 'ok' },
  'run-without-asking': { label: 'run without asking', tone: 'warn' },
  denied: { label: 'you denied', tone: 'bad' },
  'timed-out': { label: 'nobody answered', tone: 'bad' },
  off: { label: 'commands off', tone: 'off' },
  refused: { label: 'refused', tone: 'bad' }
}

/**
 * Every command an agent asked this machine to run: which agent, when, what was
 * decided, and what came back. Kept on this Mac (the last 500).
 */
export function CommandHistoryPage(): React.JSX.Element {
  const [items, setItems] = useState<ExecRecord[]>([])
  const [agent, setAgent] = useState('')
  const [open, setOpen] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setItems(await api.execLog.list())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    void refresh()
    // A node status change follows every command, so it doubles as a refresh cue.
    return api.onNodeStatus(() => void refresh())
  }, [refresh])

  const agents = useMemo(() => [...new Set(items.map((i) => i.agentId).filter(Boolean))] as string[], [items])
  const visible = agent ? items.filter((i) => i.agentId === agent) : items

  const clear = async (): Promise<void> => {
    try {
      setItems(await api.execLog.clear())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>Command history</h3>
        <p className="field-hint">
          What agents ran on this machine through the node role, and what you decided.
          Commands that run on the gateway host are not listed here.
        </p>
        <div className="settings-toolbar">
          <select value={agent} onChange={(e) => setAgent(e.target.value)}>
            <option value="">All agents</option>
            {agents.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
          {items.length > 0 && (
            <button className="btn btn-sm btn-ghost" onClick={() => void clear()}>
              Clear history
            </button>
          )}
        </div>
        {visible.length === 0 && <p className="field-hint">Nothing yet.</p>}
        <div className="inbox-list">
          {visible.map((r) => {
            const d = DECISION[r.decision] ?? { label: r.decision, tone: 'off' as const }
            const isOpen = open === r.id
            return (
              <article key={r.id} className="inbox-item">
                <span className={`hist-mark is-${r.ran ? (r.success ? 'ok' : 'bad') : 'off'}`} aria-hidden="true">
                  {r.ran ? (r.success ? '✓' : '✕') : '–'}
                </span>
                <div className="inbox-body">
                  <div className="inbox-head">
                    <strong>{r.agentId || 'An agent'}</strong>
                    <span className={`hist-badge is-${d.tone}`}>{d.label}</span>
                    <span className="inbox-time">{when(r.atMs)}</span>
                  </div>
                  <code className="approval-cmd">{r.command}</code>
                  <span className="plugin-desc">
                    {r.cwd ? `in ${r.cwd}` : ''}
                    {r.ran
                      ? ` · exit ${r.exitCode ?? '?'}${r.timedOut ? ' (timed out)' : ''}${r.durationMs ? ` · ${(r.durationMs / 1000).toFixed(1)}s` : ''}`
                      : ' · did not run'}
                  </span>
                  {isOpen && (r.output || r.error) && (
                    <pre className="md-source hist-output">{r.error ? `${r.error}\n` : ''}{r.output ?? ''}</pre>
                  )}
                </div>
                <div className="inbox-actions">
                  {(r.output || r.error) && (
                    <button className="btn btn-sm" onClick={() => setOpen(isOpen ? null : r.id)}>
                      {isOpen ? 'Hide output' : 'Output'}
                    </button>
                  )}
                </div>
              </article>
            )
          })}
        </div>
        {error && <p className="error-text">{error}</p>}
      </section>
    </div>
  )
}
