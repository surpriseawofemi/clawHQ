import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { ServerProfile, ServerRun, SessionUsage } from '../types'
import { useLiveRefresh } from '../state/useLiveRefresh'
import { plugin } from '../state/plugin'
import { ContentHead, Shell, type ShellProps } from './layout/Shell'
import type { ActivityRecord, Agent, ExecRecord, NodeNotification, Task } from '../types'
import { agentEmoji, agentLabel } from '../types'

type Props = {
  shell: ShellProps
  agents: Agent[]
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
}

type CronRun = { ts: number; jobId: string; jobName?: string; status?: string; completionStatus?: string; error?: string; durationMs?: number }
type Usage = { totalTokens: number; totalCost: number }

const isoDay = (d: Date): string => {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}
const startOf = (day: string): number => new Date(`${day}T00:00:00`).getTime()
const time = (ms: number): string => new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
const tokens = (n: number): string => (n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(0)}k` : String(n))

/**
 * One day, rolled up: what every agent did, which automations ran, what was asked
 * of you, what ran on this machine, and what it all cost. A morning review or an
 * end-of-day summary, and it copies out as Markdown for a channel or a note.
 */
export function Digest({ shell, agents, connected, onOpenAgent }: Props): React.JSX.Element {
  const [day, setDay] = useState(isoDay(new Date()))
  const [runs, setRuns] = useState<ActivityRecord[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [notices, setNotices] = useState<NodeNotification[]>([])
  const [commands, setCommands] = useState<ExecRecord[]>([])
  const [cron, setCron] = useState<CronRun[]>([])
  const [usage, setUsage] = useState<Record<string, Usage>>({})
  const [serverRuns, setServerRuns] = useState<ServerRun[]>([])
  const [serverUsage, setServerUsage] = useState<SessionUsage[]>([])
  const [serverNames, setServerNames] = useState<Record<string, string>>({})
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [loading, setLoading] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    const from = startOf(day)
    const to = from + 86_400_000
    const inDay = (ms: number) => ms >= from && ms < to
    try {
      // Servers: the local run log plus token counts read from Claude Code's own
      // session files, so terminal sessions count. Nothing here calls a model.
      void (async () => {
        try {
          const [srvs, rl] = await Promise.all([api.servers.list(), api.claude.runsSince(from)])
          setServerNames(Object.fromEntries(srvs.map((x: ServerProfile) => [x.id, x.name])))
          setServerRuns(rl.filter((r) => inDay(r.atMs)))
          const us = await Promise.all(srvs.map((x: ServerProfile) => api.claude.usageSince(x.id, from).catch(() => [] as SessionUsage[])))
          setServerUsage(us.flat())
        } catch {
          /* no servers */
        }
      })()
      const [status, inbox, log] = await Promise.all([api.plugin.status(), api.inbox.list(), api.execLog.list()])
      setPluginPresent(status.present)
      setNotices(inbox.filter((n) => inDay(n.atMs)))
      setCommands(log.filter((c) => inDay(c.atMs)))
      if (connected && status.present) {
        const [a, t] = await Promise.all([plugin.activity({ sinceMs: from - 1, limit: 2000 }), plugin.tasks.list()])
        setRuns(a.filter((r) => inDay(r.atMs)))
        setTasks(t.filter((x) => (x.finishedAtMs && inDay(x.finishedAtMs)) || inDay(x.updatedAtMs)))
      } else {
        setRuns([])
        setTasks([])
      }
      if (connected) {
        // Automations: every job's recent runs, kept to the day.
        const jobs = await api.rpc.request<{ jobs?: { id: string; name?: string }[] }>('cron.list', { limit: 200 })
        const all: CronRun[] = []
        await Promise.all(
          (jobs?.jobs ?? []).map(async (j) => {
            try {
              const res = await api.rpc.request<{ entries?: CronRun[] }>('cron.runs', { id: j.id, limit: 50 })
              for (const e of res?.entries ?? []) if (inDay(e.ts)) all.push({ ...e, jobName: e.jobName ?? j.name })
            } catch {
              /* a job without history */
            }
          })
        )
        setCron(all.sort((a, b) => b.ts - a.ts))
        // Cost per agent for the day: one sessions.usage per agent.
        const u: Record<string, Usage> = {}
        await Promise.all(
          agents.map(async (ag) => {
            try {
              const res = await api.rpc.request<{ totals?: Usage }>('sessions.usage', { agentId: ag.id, startDate: day, endDate: day, limit: 500 })
              u[ag.id] = { totalTokens: res?.totals?.totalTokens ?? 0, totalCost: res?.totals?.totalCost ?? 0 }
            } catch {
              /* no usage */
            }
          })
        )
        setUsage(u)
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [connected, day, agents])

  const live = useLiveRefresh(refresh, 30000)
  useEffect(() => {
    void refresh()
  }, [refresh])

  const byAgent = useMemo(() => {
    return agents
      .map((a) => ({
        agent: a,
        runs: runs.filter((r) => r.agentId === a.id).sort((x, y) => x.atMs - y.atMs),
        tasks: tasks.filter((t) => t.agentId === a.id),
        notices: notices.filter((n) => n.agentId === a.id),
        commands: commands.filter((c) => c.agentId === a.id),
        usage: usage[a.id]
      }))
      .filter((x) => x.runs.length || x.tasks.length || x.notices.length || x.commands.length || (x.usage?.totalTokens ?? 0) > 0)
      .sort((x, y) => y.runs.length - x.runs.length)
  }, [agents, runs, tasks, notices, commands, usage])

  const totalTokens = Object.values(usage).reduce((n, u) => n + u.totalTokens, 0)
  const totalCost = Object.values(usage).reduce((n, u) => n + u.totalCost, 0)
  const failedRuns = runs.filter((r) => !r.success).length

  const shift = (days: number): void => {
    const d = new Date(startOf(day) + days * 86_400_000)
    setDay(isoDay(d))
  }

  const markdown = (): string => {
    const lines: string[] = [`# ClawHQ digest for ${day}`, '', `- ${runs.length} runs (${failedRuns} failed) · ${tasks.filter((t) => t.status === 'done').length} tasks done · ${notices.length} asks · ${commands.length} commands · ${tokens(totalTokens)} tokens${totalCost > 0 ? ` · $${totalCost.toFixed(2)}` : ''}`, '']
    for (const x of byAgent) {
      lines.push(`## ${agentLabel(x.agent)}`)
      if (x.usage) lines.push(`${tokens(x.usage.totalTokens)} tokens${x.usage.totalCost > 0 ? `, $${x.usage.totalCost.toFixed(2)}` : ''}`)
      for (const r of x.runs) lines.push(`- ${time(r.atMs)} ${r.success ? '' : '❌ '}${r.summary || r.error || 'run'}`)
      for (const t of x.tasks) lines.push(`- task ${t.status}: ${t.title}${t.result ? ` — ${t.result}` : ''}`)
      for (const n of x.notices) lines.push(`- asked: ${n.title ? `${n.title}: ` : ''}${n.body}`)
      for (const c of x.commands) lines.push(`- ran \`${c.command}\` (${c.decision})`)
      lines.push('')
    }
    if (cron.length) {
      lines.push('## Automations')
      for (const c of cron) lines.push(`- ${time(c.ts)} ${c.jobName ?? c.jobId}: ${c.completionStatus ?? c.status}${c.error ? ` — ${c.error}` : ''}`)
      lines.push('')
    }
    if (serverRuns.length || serverUsage.length) {
      lines.push('## Servers')
      const cost = serverRuns.reduce((n, r) => n + (r.costUsd || 0), 0)
      lines.push(`${serverRuns.length} runs from ClawHQ${cost > 0 ? `, $${cost.toFixed(2)}` : ''}`)
      for (const r of serverRuns) lines.push(`- ${time(r.atMs)} ${r.ok ? '' : '❌ '}${r.serverName} / ${r.project} (${r.agent}${r.source === 'task' ? ', task from an agent' : ''}): ${r.summary || 'run'}${r.costUsd ? ` — $${r.costUsd.toFixed(2)}` : ''}`)
      for (const u of serverUsage) lines.push(`- ${serverNames[u.serverId] ?? u.serverId} / ${u.project}: ${u.sessions} Claude Code sessions, ${u.messages} replies, ${tokens(u.inputTokens + u.outputTokens)} tokens (+${tokens(u.cacheRead)} cached)`)
    }
    return lines.join('\n')
  }

  const copy = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(markdown())
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Shell shell={shell} title="Digest">
      <ContentHead onRefresh={live.now} refreshing={live.busy}
        title="Daily digest"
        subtitle={
          loading
            ? 'Loading…'
            : `${runs.length} runs · ${failedRuns} failed · ${tasks.filter((t) => t.status === 'done').length} tasks done · ${notices.length} asks · ${commands.length} commands · ${tokens(totalTokens)} tokens${totalCost > 0 ? ` · $${totalCost.toFixed(2)}` : ''}`
        }
      >
        <button className="btn btn-sm" onClick={() => shift(-1)}>
          ‹
        </button>
        <input type="date" value={day} onChange={(e) => e.target.value && setDay(e.target.value)} />
        <button className="btn btn-sm" onClick={() => shift(1)} disabled={day >= isoDay(new Date())}>
          ›
        </button>
        <button className="btn btn-sm" onClick={() => void copy()}>
          {copied ? 'Copied' : 'Copy as Markdown'}
        </button>
      </ContentHead>

      <div className="content-body">
        {pluginPresent === false && <p className="field-hint">Run summaries and tasks need the ClawHQ plugin on the gateway; asks, commands, automations and cost show anyway.</p>}
        {byAgent.length === 0 && !loading && <p className="thread-empty">Nothing recorded for this day.</p>}
        {byAgent.map((x) => (
          <section key={x.agent.id} className="digest-agent">
            <h2>
              <span>{agentEmoji(x.agent)}</span> {agentLabel(x.agent)}
              <span className="plugin-desc">
                {x.runs.length} runs{x.usage ? ` · ${tokens(x.usage.totalTokens)} tokens${x.usage.totalCost > 0 ? ` · $${x.usage.totalCost.toFixed(2)}` : ''}` : ''}
              </span>
            </h2>
            {x.runs.map((r) => (
              <button key={r.id} className={`home-row is-${r.success ? 'ok' : 'bad'}`} onClick={() => onOpenAgent(x.agent.id, r.sessionKey)}>
                <span className="home-time">{time(r.atMs)}</span>
                <span className="home-main">
                  <span className="home-detail">{r.summary || r.error || (r.success ? 'Finished a turn' : 'Turn failed')}</span>
                </span>
              </button>
            ))}
            {x.tasks.map((t) => (
              <div key={t.id} className={`home-row is-${t.status === 'failed' ? 'bad' : t.status === 'done' ? 'ok' : 'plain'}`}>
                <span className="home-time">task</span>
                <span className="home-main">
                  <span className="home-title">{t.title}</span>
                  {(t.result || t.error) && <span className="home-detail">{t.error || t.result}</span>}
                </span>
              </div>
            ))}
            {x.notices.map((n) => (
              <div key={n.id} className="home-row is-ask">
                <span className="home-time">{time(n.atMs)}</span>
                <span className="home-main">
                  <span className="home-title">Asked you{n.title ? `: ${n.title}` : ''}</span>
                  <span className="home-detail">{n.body}</span>
                </span>
              </div>
            ))}
            {x.commands.map((c) => (
              <div key={c.id} className={`home-row is-${c.ran ? (c.success ? 'ok' : 'bad') : 'plain'}`}>
                <span className="home-time">{time(c.atMs)}</span>
                <span className="home-main">
                  <span className="home-detail mono">{c.command}</span>
                </span>
              </div>
            ))}
          </section>
        ))}
        {cron.length > 0 && (
          <section className="digest-agent">
            <h2>
              <span>⏱</span> Automations <span className="plugin-desc">{cron.length} runs</span>
            </h2>
            {cron.map((c, i) => (
              <div key={`${c.jobId}-${c.ts}-${i}`} className={`home-row is-${c.completionStatus === 'failed' || c.status === 'error' ? 'bad' : c.status === 'skipped' ? 'plain' : 'ok'}`}>
                <span className="home-time">{time(c.ts)}</span>
                <span className="home-main">
                  <span className="home-title">{c.jobName ?? c.jobId}</span>
                  <span className="home-detail">
                    {c.completionStatus ?? c.status}
                    {c.durationMs ? ` · ${(c.durationMs / 1000).toFixed(1)}s` : ''}
                    {c.error ? ` · ${c.error}` : ''}
                  </span>
                </span>
              </div>
            ))}
          </section>
        )}
        {(serverRuns.length > 0 || serverUsage.length > 0) && (
          <section className="digest-agent">
            <h2>
              <span>🖥️</span> Servers{' '}
              <span className="plugin-desc">
                {serverRuns.length} runs from ClawHQ
                {serverRuns.some((r) => r.costUsd) ? ` · $${serverRuns.reduce((n, r) => n + (r.costUsd || 0), 0).toFixed(2)}` : ''}
              </span>
            </h2>
            {serverRuns.map((r, i) => (
              <div key={`${r.atMs}-${i}`} className={`home-row is-${r.ok ? 'ok' : 'bad'}`}>
                <span className="home-time">{time(r.atMs)}</span>
                <span className="home-main">
                  <span className="home-title">
                    {r.serverName} / {r.project} <span className="plugin-desc">{r.agent}{r.source === 'task' ? ' · task from an agent' : ''}</span>
                  </span>
                  <span className="home-detail">
                    {r.summary || 'run'}
                    {r.durationMs ? ` · ${Math.round(r.durationMs / 1000)}s` : ''}
                    {r.costUsd ? ` · $${r.costUsd.toFixed(2)}` : ''}
                  </span>
                </span>
              </div>
            ))}
            {serverUsage.map((u) => (
              <div key={`${u.serverId}-${u.project}`} className="home-row is-plain">
                <span className="home-time">all day</span>
                <span className="home-main">
                  <span className="home-title">
                    {serverNames[u.serverId] ?? u.serverId} / {u.project} <span className="plugin-desc">Claude Code sessions, terminal included</span>
                  </span>
                  <span className="home-detail">
                    {u.sessions} sessions · {u.messages} replies · {tokens(u.inputTokens + u.outputTokens)} tokens, {tokens(u.cacheRead)} from cache
                  </span>
                </span>
              </div>
            ))}
          </section>
        )}
        {error && <p className="error-text">{error}</p>}
      </div>
    </Shell>
  )
}
