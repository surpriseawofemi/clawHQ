import { useCallback, useEffect, useState } from 'react'
import { api } from '../../api'

/** One scheduled job, as cron.list reports it. Only the fields shown are typed. */
type CronJob = {
  id: string
  name?: string
  enabled?: boolean
  agentId?: string
  schedule?: { kind?: string; everyMs?: number; expr?: string; cron?: string; at?: string; tz?: string }
  sessionTarget?: string
  wakeMode?: string
  state?: {
    lastRunStatus?: string
    lastStatus?: string
    lastError?: string
    lastRunAtMs?: number
    nextRunAtMs?: number
    consecutiveErrors?: number
  }
  payload?: { kind?: string; message?: string; text?: string }
}

const describeSchedule = (s: CronJob['schedule']): string => {
  if (!s) return 'no schedule'
  if (s.kind === 'every' && s.everyMs) {
    const mins = Math.round(s.everyMs / 60000)
    if (mins >= 60 && mins % 60 === 0) return `every ${mins / 60}h`
    return `every ${mins}m`
  }
  if (s.expr || s.cron) return `cron ${s.expr ?? s.cron}${s.tz ? ` (${s.tz})` : ''}`
  if (s.at) return `once at ${s.at}`
  return s.kind ?? 'schedule'
}

const when = (ms?: number): string => {
  if (!ms) return 'never'
  const diff = ms - Date.now()
  const abs = Math.abs(diff)
  const unit = abs < 3600_000 ? `${Math.round(abs / 60000)}m` : abs < 86_400_000 ? `${Math.round(abs / 3600_000)}h` : `${Math.round(abs / 86_400_000)}d`
  return diff > 0 ? `in ${unit}` : `${unit} ago`
}

/**
 * The gateway's scheduled jobs: heartbeats, reminders, recurring agent runs.
 * List, pause, resume and run now. Creating jobs stays with the agents and the CLI
 * for now; the shape of a new job depends on the agent it targets.
 */
export function AutomationsPage({ connected }: { connected: boolean }): React.JSX.Element {
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!connected) return
    setLoading(true)
    try {
      const res = await api.rpc.request<{ jobs?: CronJob[] }>('cron.list', { limit: 200 })
      setJobs((res?.jobs ?? []).slice().sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '')))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [connected])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const act = async (label: string, fn: () => Promise<unknown>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      await fn()
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>Scheduled jobs</h3>
        <p className="field-hint">
          Everything the gateway runs on a timer. Pausing keeps the job; running now
          starts it immediately, whatever the schedule says.
        </p>
        {!connected && <p className="field-hint">Connect to a gateway to see its jobs.</p>}
        {loading && connected && <p className="field-hint">Loading…</p>}
        {!loading && connected && jobs.length === 0 && <p className="field-hint">No scheduled jobs.</p>}

        <div className="gw-list">
          {jobs.map((job) => {
            const status = job.state?.lastRunStatus ?? job.state?.lastStatus
            const failing = (job.state?.consecutiveErrors ?? 0) > 0 || status === 'failed'
            return (
              <div key={job.id} className="gw-row">
                <i className={`dot ${job.enabled === false ? 'dot-off' : failing ? 'dot-warn' : 'dot-ok'}`} />
                <span className="gw-meta">
                  <span className="gw-name">
                    {job.name || job.id.slice(0, 8)}
                    {job.agentId && <span className="plugin-desc"> · {job.agentId}</span>}
                  </span>
                  <span className="gw-url mono">
                    {describeSchedule(job.schedule)} · next {when(job.state?.nextRunAtMs)} · last{' '}
                    {status ?? 'never'}
                    {job.state?.lastError ? ` (${job.state.lastError})` : ''}
                  </span>
                </span>
                <button
                  className="btn btn-sm"
                  disabled={busy !== null}
                  onClick={() =>
                    void act(`toggle-${job.id}`, () =>
                      api.rpc.request('cron.update', { id: job.id, patch: { enabled: job.enabled === false } })
                    )
                  }
                >
                  {job.enabled === false ? 'Resume' : 'Pause'}
                </button>
                <button
                  className="btn btn-sm"
                  disabled={busy !== null}
                  onClick={() => void act(`run-${job.id}`, () => api.rpc.request('cron.run', { id: job.id }))}
                >
                  {busy === `run-${job.id}` ? 'Starting…' : 'Run now'}
                </button>
              </div>
            )
          })}
        </div>
        {error && <p className="error-text">{error}</p>}
      </section>
    </div>
  )
}
