import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../../api'
import type { Agent, ClawHQConfig } from '../../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../../types'

/** The token and cost totals sessions.usage reports at every level. */
type Totals = {
  input?: number
  output?: number
  cacheRead?: number
  cacheWrite?: number
  totalTokens?: number
  totalCost?: number
  missingCostEntries?: number
}

type SessionUsage = {
  key: string
  label?: string
  agentId?: string
  model?: string
  usage?: Totals & { lastActivity?: number; messageCounts?: { total?: number } }
}

type AgentUsage = {
  totals?: Totals
  sessions?: SessionUsage[]
  aggregates?: {
    sessionCount?: number
    byModel?: { provider?: string; model?: string; count?: number; totals?: Totals }[]
  }
}

type Window = 7 | 30 | 90

const zero = (): Required<Pick<Totals, 'totalTokens' | 'totalCost' | 'missingCostEntries' | 'input' | 'output'>> => ({
  totalTokens: 0,
  totalCost: 0,
  missingCostEntries: 0,
  input: 0,
  output: 0
})

const add = (a: ReturnType<typeof zero>, b?: Totals): ReturnType<typeof zero> => ({
  totalTokens: a.totalTokens + (b?.totalTokens ?? 0),
  totalCost: a.totalCost + (b?.totalCost ?? 0),
  missingCostEntries: a.missingCostEntries + (b?.missingCostEntries ?? 0),
  input: a.input + (b?.input ?? 0),
  output: a.output + (b?.output ?? 0)
})

const tokens = (n: number): string =>
  n >= 1e9 ? `${(n / 1e9).toFixed(2)}B` : n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(0)}k` : String(n)

const money = (n: number): string => (n >= 100 ? `$${n.toFixed(0)}` : n >= 1 ? `$${n.toFixed(2)}` : `$${n.toFixed(3)}`)

const isoDay = (d: Date): string => d.toISOString().slice(0, 10)

/**
 * Tokens and cost per agent, rolled up by department.
 *
 * The gateway answers `sessions.usage` one agent at a time (a multi-agent gateway
 * refuses the unscoped call), so the page fans out one request per agent for the
 * chosen window and adds the totals up along ClawHQ's own org chart. Cost is what
 * the gateway prices; calls it cannot price (a CLI-backed model, say) are counted
 * and shown as unknown rather than as zero.
 */
export function UsagePage({
  connected,
  config,
  agents
}: {
  connected: boolean
  config: ClawHQConfig | null
  agents: Agent[]
}): React.JSX.Element {
  const [window, setWindow] = useState<Window>(7)
  const [byAgent, setByAgent] = useState<Record<string, AgentUsage>>({})
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!connected || agents.length === 0) return
    setLoading(true)
    setError(null)
    const end = new Date()
    const start = new Date(end.getTime() - (window - 1) * 86_400_000)
    const params = { startDate: isoDay(start), endDate: isoDay(end), limit: 500 }
    const next: Record<string, AgentUsage> = {}
    const failures: string[] = []
    await Promise.all(
      agents.map(async (a) => {
        try {
          next[a.id] = await api.rpc.request<AgentUsage>('sessions.usage', { ...params, agentId: a.id })
        } catch (err) {
          failures.push(`${a.id}: ${err instanceof Error ? err.message : String(err)}`)
        }
      })
    )
    setByAgent(next)
    if (failures.length) setError(failures.join('\n'))
    setLoading(false)
  }, [connected, agents, window])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const departments = useMemo(() => {
    const depts = [...(config?.departments ?? [])].sort((a, b) => a.order - b.order)
    const groups = [...depts.map((d) => ({ id: d.id, name: d.name, emoji: d.emoji })), { id: UNASSIGNED, name: 'Unassigned', emoji: '📥' }]
    const rows = groups
      .map((g) => {
        const members = agents
          .filter((a) => (config?.assignments?.[a.id] ?? UNASSIGNED) === g.id)
          .map((a) => {
            const u = byAgent[a.id]
            return { agent: a, totals: add(zero(), u?.totals), sessions: u?.aggregates?.sessionCount ?? u?.sessions?.length ?? 0, usage: u }
          })
          .sort((x, y) => y.totals.totalTokens - x.totals.totalTokens)
        const totals = members.reduce((acc, m) => add(acc, m.totals), zero())
        return { ...g, members, totals, sessions: members.reduce((n, m) => n + m.sessions, 0) }
      })
      .filter((g) => g.members.length > 0)
    return rows.sort((a, b) => b.totals.totalTokens - a.totals.totalTokens)
  }, [config, agents, byAgent])

  const grand = departments.reduce((acc, d) => add(acc, d.totals), zero())
  const max = Math.max(1, ...departments.flatMap((d) => d.members.map((m) => m.totals.totalTokens)))
  const priced = grand.totalCost > 0

  return (
    <div className="settings-stack">
      <section className="panel">
        <div className="settings-toolbar">
          <h3 style={{ flex: 1 }}>Usage</h3>
          <div className="tabs tabs-inline">
            {([7, 30, 90] as Window[]).map((w) => (
              <button key={w} className={`tab${window === w ? ' is-active' : ''}`} onClick={() => setWindow(w)}>
                {w} days
              </button>
            ))}
          </div>
          <button className="btn btn-sm" disabled={!connected || loading} onClick={() => void refresh()}>
            {loading ? 'Loading…' : 'Refresh'}
          </button>
        </div>
        <p className="field-hint">
          Tokens and cost per agent from the gateway's session records, added up by
          department. Cache reads count as tokens; they are what keeps a long thread cheap.
        </p>
        {!connected && <p className="field-hint">Connect to a gateway to see usage.</p>}
        {connected && (
          <dl className="kv usage-kv">
            <dt>Total</dt>
            <dd>
              {tokens(grand.totalTokens)} tokens · {priced ? money(grand.totalCost) : 'cost unknown'}
              {grand.missingCostEntries > 0 && (
                <span className="plugin-desc"> · {grand.missingCostEntries} calls without a price</span>
              )}
            </dd>
          </dl>
        )}
        <div className="gw-list usage-list">
          {departments.map((d) => (
            <div key={d.id} className="usage-dept">
              <div className="usage-dept-head">
                <span className="gw-name">
                  {d.emoji} {d.name}
                </span>
                <span className="plugin-desc">
                  {d.members.length} {d.members.length === 1 ? 'agent' : 'agents'} · {d.sessions} sessions
                </span>
                <span className="usage-num mono">{tokens(d.totals.totalTokens)}</span>
                <span className="usage-num mono">{d.totals.totalCost > 0 ? money(d.totals.totalCost) : priced ? '$0' : '–'}</span>
              </div>
              {d.members.map((m) => {
                const isOpen = open === m.agent.id
                const top = (m.usage?.sessions ?? [])
                  .slice()
                  .sort((x, y) => (y.usage?.totalTokens ?? 0) - (x.usage?.totalTokens ?? 0))
                  .slice(0, 5)
                return (
                  <div key={m.agent.id} className="settings-row-block">
                    <button className={`usage-row${isOpen ? ' is-active' : ''}`} onClick={() => setOpen(isOpen ? null : m.agent.id)}>
                      <span className="usage-agent">
                        {agentEmoji(m.agent)} {agentLabel(m.agent)}
                      </span>
                      <span className="usage-bar" aria-hidden="true">
                        <i style={{ width: `${Math.max(1, (m.totals.totalTokens / max) * 100)}%` }} />
                      </span>
                      <span className="usage-num mono">{tokens(m.totals.totalTokens)}</span>
                      <span className="usage-num mono">{m.totals.totalCost > 0 ? money(m.totals.totalCost) : priced ? '$0' : '–'}</span>
                    </button>
                    {isOpen && (
                      <div className="settings-drawer">
                        <p className="plugin-desc">
                          {m.sessions} sessions · in {tokens(m.totals.input)} · out {tokens(m.totals.output)}
                          {(m.usage?.aggregates?.byModel ?? []).length > 0 &&
                            ` · ${(m.usage?.aggregates?.byModel ?? [])
                              .map((x) => `${x.model ?? '?'} ×${x.count ?? 0}`)
                              .join(', ')}`}
                        </p>
                        {top.length === 0 && <p className="field-hint">No sessions in this window.</p>}
                        {top.map((s) => (
                          <div key={s.key} className="usage-session">
                            <span className="usage-agent">{s.label || s.key.replace(/^agent:[^:]+:/, '')}</span>
                            <span className="usage-num mono">{tokens(s.usage?.totalTokens ?? 0)}</span>
                            <span className="usage-num mono">
                              {(s.usage?.totalCost ?? 0) > 0 ? money(s.usage?.totalCost ?? 0) : priced ? '$0' : '–'}
                            </span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          ))}
        </div>
        {connected && !loading && departments.length === 0 && <p className="field-hint">No agents to report on.</p>}
        {error && <pre className="error-text">{error}</pre>}
      </section>
    </div>
  )
}
