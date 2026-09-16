import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { Agent, ClawHQConfig, ExecRecord, NodeNotification, SessionInfo } from '../types'
import { UNASSIGNED, agentEmoji, agentLabel } from '../types'

type Props = {
  agents: Agent[]
  sessions: SessionInfo[]
  config: ClawHQConfig | null
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
  onOpenNotifications: () => void
  onOpenCommands: () => void
  onBack: () => void
}

/** One agent run, as the gateway plugin records it on agent_end. */
type Activity = {
  id: string
  atMs: number
  agentId?: string
  sessionKey?: string
  success: boolean
  durationMs?: number
  error?: string
  summary?: string
  channel?: string
}

/** Everything on the feed, from three sources, in one shape. */
type Item = {
  id: string
  atMs: number
  agentId: string
  sessionKey?: string
  kind: 'run' | 'notice' | 'command' | 'thread'
  title: string
  detail: string
  tone: 'ok' | 'bad' | 'ask' | 'plain'
  needsYou: boolean
}

const ago = (ms: number): string => {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86_400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86_400)}d ago`
}

const dayLabel = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date()
  const y = new Date()
  y.setDate(today.getDate() - 1)
  if (d.toDateString() === today.toDateString()) return 'Today'
  if (d.toDateString() === y.toDateString()) return 'Yesterday'
  return d.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' })
}

const agentOfSession = (key: string): string => key.split(':')[1] ?? ''

/**
 * The landing page: what every agent has done, whether anything needs you, and
 * what is running right now, without opening agents one at a time.
 *
 * Runs come from the gateway plugin's activity feed when it is installed. Notices
 * and command history come from this machine's own records either way, and the
 * session list says what is live. Everything is one timeline, filterable by
 * department and agent; a row opens that agent's thread.
 */
export function HomePage({ agents, sessions, config, connected, onOpenAgent, onOpenNotifications, onOpenCommands, onBack }: Props): React.JSX.Element {
  const [runs, setRuns] = useState<Activity[]>([])
  const [notices, setNotices] = useState<NodeNotification[]>([])
  const [commands, setCommands] = useState<ExecRecord[]>([])
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [dept, setDept] = useState('')
  const [agentFilter, setAgentFilter] = useState('')
  const [onlyNeedsYou, setOnlyNeedsYou] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const [inbox, log, status] = await Promise.all([api.inbox.list(), api.execLog.list(), api.plugin.status()])
      setNotices(inbox)
      setCommands(log)
      setPluginPresent(status.present)
      if (connected && status.present) {
        const res = await api.rpc.request<{ activity?: Activity[] }>('clawhq.activity.list', { limit: 300 })
        setRuns(res?.activity ?? [])
      } else {
        setRuns([])
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  useEffect(() => {
    void refresh()
    const tick = setInterval(() => void refresh(), 30_000)
    const offEvents = api.onGatewayEvent(({ event }) => {
      if (event === 'clawhq.activity' || event === 'clawhq.notice' || event === 'sessions.changed') void refresh()
    })
    const offNotice = api.onNodeNotification(() => void refresh())
    const offNode = api.onNodeStatus(() => void refresh())
    const offPlugin = api.onPluginStatus(() => void refresh())
    return () => {
      clearInterval(tick)
      offEvents()
      offNotice()
      offNode()
      offPlugin()
    }
  }, [refresh])

  const byId = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents])
  const name = (id: string): string => (byId.get(id) ? agentLabel(byId.get(id)!) : id || 'An agent')
  const emoji = (id: string): string => (byId.get(id) ? agentEmoji(byId.get(id)!) : '🤖')
  const deptOf = (id: string): string => config?.assignments?.[id] ?? UNASSIGNED

  const items = useMemo<Item[]>(() => {
    const out: Item[] = []
    for (const r of runs) {
      out.push({
        id: `run-${r.id}`,
        atMs: r.atMs,
        agentId: r.agentId ?? (r.sessionKey ? agentOfSession(r.sessionKey) : ''),
        sessionKey: r.sessionKey,
        kind: 'run',
        title: r.success ? 'Finished a turn' : 'Turn failed',
        detail: r.summary || r.error || '',
        tone: r.success ? 'ok' : 'bad',
        needsYou: !r.success
      })
    }
    for (const n of notices) {
      out.push({
        id: `notice-${n.id}`,
        atMs: n.atMs,
        agentId: n.agentId ?? '',
        sessionKey: n.sessionKey,
        kind: 'notice',
        title: n.title || 'Asked for you',
        detail: n.body,
        tone: 'ask',
        needsYou: !n.read
      })
    }
    for (const c of commands) {
      out.push({
        id: `cmd-${c.id}`,
        atMs: c.atMs,
        agentId: c.agentId ?? '',
        sessionKey: c.sessionKey,
        kind: 'command',
        title: c.ran ? (c.success ? 'Ran a command' : 'Command failed') : 'Command not run',
        detail: c.command,
        tone: c.ran ? (c.success ? 'ok' : 'bad') : 'plain',
        needsYou: false
      })
    }
    // Without the plugin there are no run records; thread updates stand in.
    if (pluginPresent === false) {
      for (const s of sessions) {
        const at = s.updatedAt ?? s.lastActivityAt
        if (!at || s.kind === 'cron') continue
        out.push({
          id: `thread-${s.key}`,
          atMs: at,
          agentId: s.agentId,
          sessionKey: s.key,
          kind: 'thread',
          title: s.hasActiveRun ? 'Working' : 'Thread updated',
          detail: s.label || s.key.replace(/^agent:[^:]+:/, ''),
          tone: 'plain',
          needsYou: false
        })
      }
    }
    return out
      .filter((i) => !dept || deptOf(i.agentId) === dept)
      .filter((i) => !agentFilter || i.agentId === agentFilter)
      .filter((i) => !onlyNeedsYou || i.needsYou)
      .sort((a, b) => b.atMs - a.atMs)
      .slice(0, 400)
    // deptOf reads config, which is a dep.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs, notices, commands, sessions, pluginPresent, dept, agentFilter, onlyNeedsYou, config])

  const needsYou = items.filter((i) => i.needsYou)
  const running = sessions.filter((s) => s.hasActiveRun)
  const departments = [...(config?.departments ?? [])].sort((a, b) => a.order - b.order)

  // Group rows by day for scanning.
  const groups: { label: string; rows: Item[] }[] = []
  for (const it of items) {
    const label = dayLabel(it.atMs)
    const last = groups[groups.length - 1]
    if (last && last.label === label) last.rows.push(it)
    else groups.push({ label, rows: [it] })
  }

  const open = (it: Item): void => {
    if (!it.agentId) return
    onOpenAgent(it.agentId, it.sessionKey)
  }

  return (
    <main className="home">
      <header className="home-head">
        <div>
          <button className="back-btn" onClick={onBack} title="Back to the main menu">
            ‹ Menu
          </button>
          <h1>Activity</h1>
          <p>
            {!connected
              ? 'Not connected to a gateway.'
              : `${agents.length} agents · ${running.length} working now · ${needsYou.length} ${needsYou.length === 1 ? 'thing needs' : 'things need'} you`}
          </p>
        </div>
        <div className="settings-toolbar">
          <select value={dept} onChange={(e) => setDept(e.target.value)} aria-label="Department">
            <option value="">All departments</option>
            {departments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.emoji} {d.name}
              </option>
            ))}
            <option value={UNASSIGNED}>Unassigned</option>
          </select>
          <select value={agentFilter} onChange={(e) => setAgentFilter(e.target.value)} aria-label="Agent">
            <option value="">All agents</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {agentLabel(a)}
              </option>
            ))}
          </select>
          <label className="check-row">
            <input type="checkbox" checked={onlyNeedsYou} onChange={(e) => setOnlyNeedsYou(e.target.checked)} />
            <span>Needs me</span>
          </label>
        </div>
      </header>

      <div className="home-body">
        {needsYou.length > 0 && !onlyNeedsYou && (
          <section className="home-section home-needs">
            <h2>Needs you</h2>
            {needsYou.slice(0, 6).map((it) => (
              <button key={it.id} className="home-row" onClick={() => open(it)}>
                <span className="home-avatar">{emoji(it.agentId)}</span>
                <span className="home-main">
                  <span className="home-title">
                    <strong>{name(it.agentId)}</strong> · {it.title}
                  </span>
                  <span className="home-detail">{it.detail}</span>
                </span>
                <span className="home-time">{ago(it.atMs)}</span>
              </button>
            ))}
            <div className="btn-row">
              <button className="btn btn-sm btn-ghost" onClick={onOpenNotifications}>
                All notifications
              </button>
              <button className="btn btn-sm btn-ghost" onClick={onOpenCommands}>
                Command history
              </button>
            </div>
          </section>
        )}

        {running.length > 0 && (
          <section className="home-section">
            <h2>Working now</h2>
            {running.map((s) => (
              <button key={s.key} className="home-row" onClick={() => onOpenAgent(s.agentId, s.key)}>
                <span className="home-avatar">{emoji(s.agentId)}</span>
                <span className="home-main">
                  <span className="home-title">
                    <strong>{name(s.agentId)}</strong> · {s.label || 'main thread'}
                  </span>
                  <span className="home-detail">
                    <i className="dot-active" /> running
                  </span>
                </span>
              </button>
            ))}
          </section>
        )}

        {connected && pluginPresent === false && (
          <p className="field-hint">
            Install the ClawHQ plugin on the gateway (Settings → Plugins) to see what each agent did on every
            turn. Until then this shows thread updates, notifications and commands.
          </p>
        )}

        {groups.length === 0 && <p className="thread-empty">Nothing yet.</p>}
        {groups.map((g) => (
          <section key={g.label} className="home-section">
            <h2>{g.label}</h2>
            {g.rows.map((it) => (
              <button key={it.id} className={`home-row is-${it.tone}`} onClick={() => open(it)}>
                <span className="home-avatar">{emoji(it.agentId)}</span>
                <span className="home-main">
                  <span className="home-title">
                    <strong>{name(it.agentId)}</strong> · {it.title}
                    {it.needsYou && <span className="hist-badge is-warn">needs you</span>}
                  </span>
                  {it.detail && <span className={`home-detail${it.kind === 'command' ? ' mono' : ''}`}>{it.detail}</span>}
                </span>
                <span className="home-time">{ago(it.atMs)}</span>
              </button>
            ))}
          </section>
        ))}
        {error && <p className="error-text">{error}</p>}
      </div>
    </main>
  )
}
