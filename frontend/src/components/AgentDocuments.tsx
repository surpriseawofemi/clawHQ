import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { renderMarkdown } from '../markdown'
import { AgentHeader, type AgentTab } from './AgentHeader'
import type { Agent } from '../types'

type Entry = {
  path: string
  name: string
  kind: 'file' | 'directory' | string
  size?: number
  updatedAtMs?: number
}

type FileBody = {
  path: string
  name: string
  content: string
  mimeType?: string
  updatedAtMs?: number
}

/** The charter and housekeeping files that are not work product. */
const NOT_DOCUMENTS = new Set(['AGENTS.md', 'SOUL.md', 'IDENTITY.md', 'USER.md', 'DREAMS.md', 'BOOTSTRAP.md'])

/** The cron job ClawHQ creates for an agent's daily update. */
type CronJob = {
  id: string
  name?: string
  agentId?: string
  enabled?: boolean
  schedule?: { kind?: string; expr?: string; tz?: string }
  state?: { lastRunAtMs?: number; lastRunStatus?: string; nextRunAtMs?: number }
}

const dailyJobName = (agentId: string): string => `clawhq-daily-update-${agentId}`

const DAILY_PROMPT = [
  'Daily update for the person who runs this org. Do exactly this and no other work:',
  '',
  '1. Write your update to DAILY-UPDATE.md at the root of your workspace, replacing the file.',
  '   Keep it under 40 lines with these headings: Yesterday, Today, Blocked, Needs a decision.',
  '   Base it on your memory notes and the files you changed; say plainly when there is nothing new.',
  '2. Append the same text under a dated heading to updates/YYYY-MM-DD.md (create the folder if needed).',
  '3. Do not start new tasks, and do not message anyone.'
].join('\n')

const HOURS = Array.from({ length: 24 }, (_, h) => h)

const hourLabel = (h: number): string => `${String(h).padStart(2, '0')}:00`

/** Hour of a "0 H * * *" expression, or null when the schedule is something else. */
const hourOf = (job: CronJob | null): number | null => {
  const m = job?.schedule?.expr?.match(/^0 (\d{1,2}) \* \* \*$/)
  return m ? Number(m[1]) : null
}

const when = (ms?: number): string => {
  if (!ms) return ''
  const diff = Date.now() - ms
  if (diff < 3_600_000) return `${Math.max(1, Math.round(diff / 60_000))}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return new Date(ms).toLocaleDateString([], { month: 'short', day: 'numeric' })
}

type Props = {
  agent: Agent
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  connected: boolean
}

/**
 * An agent's deliverables: the markdown in its workspace, read through the gateway.
 *
 * Every agent writes its work product as `*.md` at the workspace root, so that is
 * the default listing; folders such as `memory/` can be opened too. Rendering is
 * read-only here: the gateway only accepts writes to the charter files.
 */
export function AgentDocuments({ agent, tab, onTab, connected }: Props): React.JSX.Element {
  const [dir, setDir] = useState('')
  const [entries, setEntries] = useState<Entry[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [file, setFile] = useState<FileBody | null>(null)
  const [showSource, setShowSource] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [daily, setDaily] = useState<CronJob | null>(null)
  const [dailyBusy, setDailyBusy] = useState(false)

  // The daily-update job for this agent, if ClawHQ has set one up.
  const loadDaily = useCallback(async () => {
    if (!connected) return
    try {
      const res = await api.rpc.request<{ jobs?: CronJob[] }>('cron.list', { limit: 500 })
      setDaily((res?.jobs ?? []).find((j) => j.name === dailyJobName(agent.id)) ?? null)
    } catch {
      setDaily(null)
    }
  }, [agent.id, connected])

  useEffect(() => {
    void loadDaily()
  }, [loadDaily])

  const setDailyHour = async (hour: number | null): Promise<void> => {
    setDailyBusy(true)
    setError(null)
    try {
      if (hour === null) {
        if (daily) await api.rpc.request('cron.remove', { id: daily.id })
      } else {
        const schedule = { kind: 'cron', expr: `0 ${hour} * * *`, tz: Intl.DateTimeFormat().resolvedOptions().timeZone }
        if (daily) {
          await api.rpc.request('cron.update', { id: daily.id, patch: { schedule, enabled: true } })
        } else {
          await api.rpc.request('cron.add', {
            name: dailyJobName(agent.id),
            agentId: agent.id,
            sessionTarget: 'isolated',
            wakeMode: 'now',
            schedule,
            payload: { kind: 'agentTurn', message: DAILY_PROMPT },
            delivery: { mode: 'none' },
            enabled: true
          })
        }
      }
      await loadDaily()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setDailyBusy(false)
    }
  }

  const runDailyNow = async (): Promise<void> => {
    if (!daily) return
    setDailyBusy(true)
    setError(null)
    try {
      await api.rpc.request('cron.run', { id: daily.id })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setDailyBusy(false)
    }
  }

  const listDir = useCallback(
    async (path: string) => {
      if (!connected) return
      setLoading(true)
      try {
        const res = await api.rpc.request<{ entries?: Entry[] }>('agents.workspace.list', {
          agentId: agent.id,
          ...(path ? { path } : {})
        })
        setEntries(res?.entries ?? [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setLoading(false)
      }
    },
    [agent.id, connected]
  )

  useEffect(() => {
    setDir('')
    setSelected(null)
    setFile(null)
  }, [agent.id])

  useEffect(() => {
    void listDir(dir)
  }, [dir, listDir])

  const visible = useMemo(() => {
    const dirs = entries.filter((e) => e.kind === 'directory' && !e.name.startsWith('.'))
    const docs = entries.filter(
      (e) => e.kind === 'file' && /\.(md|markdown|txt)$/i.test(e.name) && !(dir === '' && NOT_DOCUMENTS.has(e.name))
    )
    docs.sort((a, b) => (b.updatedAtMs ?? 0) - (a.updatedAtMs ?? 0))
    return { dirs, docs }
  }, [entries, dir])

  // Open the most recent document by default so the tab is never a blank pane.
  useEffect(() => {
    if (!selected && visible.docs.length > 0) setSelected(visible.docs[0].path)
  }, [visible.docs, selected])

  useEffect(() => {
    if (!selected || !connected) return
    let cancelled = false
    void (async () => {
      try {
        const res = await api.rpc.request<{ file?: FileBody }>('agents.workspace.get', {
          agentId: agent.id,
          path: selected
        })
        if (!cancelled) setFile(res?.file ?? null)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [selected, agent.id, connected])

  const crumbs = dir ? dir.split(/[\\/]/).filter(Boolean) : []

  return (
    <main className="chat">
      <AgentHeader agent={agent} tab={tab} onTab={onTab} />
      <div className="docs">
        <aside className="docs-list">
          {crumbs.length > 0 && (
            <div className="docs-crumbs">
              <button onClick={() => setDir('')}>workspace</button>
              {crumbs.map((c, i) => (
                <span key={i}>
                  {' / '}
                  <button onClick={() => setDir(crumbs.slice(0, i + 1).join('/'))}>{c}</button>
                </span>
              ))}
            </div>
          )}
          {visible.dirs.map((d) => (
            <button key={d.path} className="docs-item is-dir" onClick={() => setDir(d.path)}>
              📁 {d.name}
            </button>
          ))}
          {visible.docs.map((d) => (
            <button
              key={d.path}
              className={`docs-item${selected === d.path ? ' is-active' : ''}`}
              onClick={() => setSelected(d.path)}
            >
              <span>{d.name.replace(/\.(md|markdown|txt)$/i, '')}</span>
              <small>
                {when(d.updatedAtMs)}
                {d.size ? ` · ${Math.max(1, Math.round(d.size / 1024))} KB` : ''}
              </small>
            </button>
          ))}
          {!loading && visible.docs.length === 0 && visible.dirs.length === 0 && (
            <p className="field-hint">No documents here yet.</p>
          )}
          {loading && <p className="field-hint">Loading…</p>}
        </aside>
        <section className="docs-body">
          <div className="daily-card">
            <span>📝 Daily update</span>
            <select
              value={hourOf(daily) ?? ''}
              disabled={dailyBusy || !connected}
              onChange={(e) => void setDailyHour(e.target.value === '' ? null : Number(e.target.value))}
              title="When the agent writes DAILY-UPDATE.md each day"
            >
              <option value="">off</option>
              {HOURS.map((h) => (
                <option key={h} value={h}>
                  every day at {hourLabel(h)}
                </option>
              ))}
            </select>
            {daily && (
              <>
                <span>
                  {daily.state?.lastRunAtMs
                    ? `last ${when(daily.state.lastRunAtMs)}${daily.state.lastRunStatus ? ` (${daily.state.lastRunStatus})` : ''}`
                    : 'not run yet'}
                </span>
                <button className="btn btn-sm" disabled={dailyBusy} onClick={() => void runDailyNow()}>
                  Write one now
                </button>
              </>
            )}
            {!daily && <span>The agent writes DAILY-UPDATE.md here at that hour.</span>}
          </div>
          {error && <p className="error-text">{error}</p>}
          {file ? (
            <>
              <div className="docs-toolbar">
                <span className="mono">{file.path}</span>
                {file.updatedAtMs && <span>· updated {when(file.updatedAtMs)}</span>}
                <div className="btn-row">
                  <button className="btn btn-sm" onClick={() => setShowSource((s) => !s)}>
                    {showSource ? 'Rendered' : 'Source'}
                  </button>
                </div>
              </div>
              {showSource ? (
                <pre className="md-source">{file.content}</pre>
              ) : (
                <article className="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(file.content) }} />
              )}
            </>
          ) : (
            !error && <p className="thread-empty">{connected ? 'Pick a document.' : 'Not connected.'}</p>
          )}
        </section>
      </div>
    </main>
  )
}
