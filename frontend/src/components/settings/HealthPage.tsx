import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../../api'

/** The gateway's health snapshot (`health` RPC, also pushed as the `health` event). */
type Health = {
  ok?: boolean
  ts?: number
  durationMs?: number
  heartbeatSeconds?: number
  configReload?: { hotReloadStatus?: string }
  eventLoop?: {
    degraded?: boolean
    reasons?: string[]
    utilization?: number
    delayP99Ms?: number
    delayMaxMs?: number
    cpuCoreRatio?: number
  }
  plugins?: { loaded?: string[]; errors?: unknown[]; unavailable?: string[] }
  sessions?: { count?: number; path?: string }
  agents?: {
    agentId: string
    name?: string
    isDefault?: boolean
    heartbeat?: { enabled?: boolean; every?: string }
    sessions?: { count?: number; recent?: { key: string; updatedAt?: number }[] }
  }[]
  channelOrder?: string[]
  channelLabels?: Record<string, string>
  channels?: Record<string, { configured?: boolean; running?: boolean; connected?: boolean; lastError?: string }>
}

type LogsTail = { file?: string; cursor: number; size?: number; lines?: string[]; truncated?: boolean; reset?: boolean }

/** A log line, parsed when the gateway writes JSON, otherwise the raw text. */
type LogLine = { id: number; raw: string; time?: string; level?: string; subsystem?: string; text: string }

const LOG_CAP = 2000
const FOLLOW_MS = 3000

const levelOf = (v: unknown): string | undefined => {
  if (typeof v === 'string') return v.toLowerCase()
  if (typeof v === 'number') return v >= 50 ? 'error' : v >= 40 ? 'warn' : v >= 30 ? 'info' : 'debug'
  return undefined
}

const parseLine = (raw: string, id: number): LogLine => {
  const t = raw.trim()
  if (t.startsWith('{')) {
    try {
      const o = JSON.parse(t) as Record<string, unknown>
      const time = o.time ?? o.timestamp ?? o.ts ?? o['@timestamp']
      const text = o.msg ?? o.message ?? o.event
      const subsystem = o.subsystem ?? o.module ?? o.name ?? o.scope
      return {
        id,
        raw,
        time: typeof time === 'number' ? new Date(time).toLocaleTimeString() : typeof time === 'string' ? time.replace(/^\d{4}-\d{2}-\d{2}T/, '').replace(/\.\d+Z?$/, '') : undefined,
        level: levelOf(o.level ?? o.severity),
        subsystem: typeof subsystem === 'string' ? subsystem : undefined,
        text: typeof text === 'string' ? text : t
      }
    } catch {
      /* not JSON after all */
    }
  }
  const m = /^(\S+)\s+(?:\[?(error|warn|warning|info|debug|trace)\]?:?)\s+/i.exec(t)
  return { id, raw, time: m?.[1], level: m?.[2]?.toLowerCase().replace('warning', 'warn'), text: m ? t.slice(m[0].length) : t }
}

const pct = (n?: number): string => (n === undefined ? '–' : `${Math.round(n * 100)}%`)
const ago = (ms?: number): string => {
  if (!ms) return 'never'
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.round(s / 60)}m ago` : `${Math.round(s / 3600)}h ago`
}

/**
 * The gateway's health snapshot and a live tail of its log. The snapshot refreshes
 * whenever the gateway pushes a `health` event; the log follows with `logs.tail`
 * cursors, so only new lines cross the wire.
 */
export function HealthPage({ connected }: { connected: boolean }): React.JSX.Element {
  const [health, setHealth] = useState<Health | null>(null)
  const [healthError, setHealthError] = useState<string | null>(null)
  const [lines, setLines] = useState<LogLine[]>([])
  const [file, setFile] = useState('')
  const [follow, setFollow] = useState(true)
  const [filter, setFilter] = useState('')
  const [level, setLevel] = useState('')
  const [logError, setLogError] = useState<string | null>(null)
  const cursor = useRef<number | null>(null)
  const seq = useRef(0)
  const endRef = useRef<HTMLDivElement | null>(null)

  const loadHealth = useCallback(async () => {
    if (!connected) return
    try {
      setHealth(await api.rpc.request<Health>('health', {}))
      setHealthError(null)
    } catch (err) {
      setHealthError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  const tail = useCallback(async () => {
    if (!connected) return
    try {
      const params: Record<string, number> = cursor.current === null ? { limit: 300, maxBytes: 256 * 1024 } : { cursor: cursor.current, maxBytes: 256 * 1024 }
      const res = await api.rpc.request<LogsTail>('logs.tail', params)
      if (!res) return
      if (res.file) setFile(res.file)
      const fresh = (res.lines ?? []).map((raw) => parseLine(raw, ++seq.current))
      cursor.current = res.cursor
      if (res.reset) {
        setLines(fresh)
      } else if (fresh.length) {
        setLines((prev) => [...prev, ...fresh].slice(-LOG_CAP))
      }
      setLogError(null)
    } catch (err) {
      setLogError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  useEffect(() => {
    void loadHealth()
    return api.onGatewayEvent(({ event, payload }) => {
      if (event === 'health' && payload) setHealth(payload as Health)
    })
  }, [loadHealth])

  useEffect(() => {
    cursor.current = null
    seq.current = 0
    setLines([])
    void tail()
  }, [tail])

  useEffect(() => {
    if (!follow || !connected) return
    const t = setInterval(() => void tail(), FOLLOW_MS)
    return () => clearInterval(t)
  }, [follow, connected, tail])

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return lines.filter((l) => (!level || l.level === level || (level === 'warn' && l.level === 'error')) && (!q || l.raw.toLowerCase().includes(q)))
  }, [lines, filter, level])

  useEffect(() => {
    if (follow) endRef.current?.scrollIntoView({ block: 'end' })
  }, [visible, follow])

  const loop = health?.eventLoop
  const degraded = health ? !health.ok || loop?.degraded : false
  const channels = health?.channelOrder?.length ? health.channelOrder : Object.keys(health?.channels ?? {})

  return (
    <div className="settings-stack">
      <section className="panel">
        <div className="settings-toolbar">
          <h3 style={{ flex: 1 }}>Gateway health</h3>
          <button className="btn btn-sm" disabled={!connected} onClick={() => void loadHealth()}>
            Refresh
          </button>
        </div>
        {!connected && <p className="field-hint">Connect to a gateway to see its health.</p>}
        {connected && !health && !healthError && <p className="field-hint">Loading…</p>}
        {health && (
          <>
            <p className="health-line">
              <i className={`dot ${degraded ? 'dot-warn' : 'dot-ok'}`} />
              {degraded ? 'Degraded' : 'Healthy'}
              {loop?.reasons?.length ? <span className="plugin-desc"> · {loop.reasons.join(', ').replace(/_/g, ' ')}</span> : null}
              <span className="plugin-desc"> · checked {ago(health.ts)}</span>
            </p>
            <dl className="kv health-kv">
              <dt>Event loop</dt>
              <dd>
                {pct(loop?.utilization)} busy · p99 delay {Math.round(loop?.delayP99Ms ?? 0)} ms · CPU {pct(loop?.cpuCoreRatio)} of a core
              </dd>
              <dt>Config</dt>
              <dd>hot reload {health.configReload?.hotReloadStatus ?? 'unknown'}</dd>
              <dt>Heartbeat</dt>
              <dd>{health.heartbeatSeconds ? `every ${Math.round(health.heartbeatSeconds / 60)} min` : 'off'}</dd>
              <dt>Sessions</dt>
              <dd>{health.sessions?.count ?? 0} on the default agent</dd>
              <dt>Plugins</dt>
              <dd className="health-wrap">
                {(health.plugins?.loaded ?? []).length} loaded
                {health.plugins?.unavailable?.length ? ` · unavailable: ${health.plugins.unavailable.join(', ')}` : ''}
                {health.plugins?.errors?.length ? ` · ${health.plugins.errors.length} with errors` : ''}
              </dd>
              {channels.length > 0 && (
                <>
                  <dt>Channels</dt>
                  <dd className="health-wrap">
                    {channels.map((id) => {
                      const c = health.channels?.[id]
                      const on = c?.connected ?? c?.running
                      return (
                        <span key={id} className="health-chan">
                          <i className={`dot ${on ? 'dot-ok' : c?.configured ? 'dot-warn' : 'dot-off'}`} />
                          {health.channelLabels?.[id] ?? id}
                          {c?.lastError ? <span className="plugin-desc"> {c.lastError}</span> : null}
                        </span>
                      )
                    })}
                  </dd>
                </>
              )}
            </dl>
            {health.agents?.length ? (
              <div className="gw-list health-agents">
                {health.agents.map((a) => {
                  const last = a.sessions?.recent?.[0]?.updatedAt
                  return (
                    <div key={a.agentId} className="gw-row">
                      <i className={`dot ${a.heartbeat?.enabled ? 'dot-ok' : 'dot-off'}`} />
                      <span className="gw-meta">
                        <span className="gw-name">
                          {a.name || a.agentId}
                          {a.isDefault && <span className="plugin-desc"> · default</span>}
                        </span>
                        <span className="gw-url mono">
                          {a.sessions?.count ?? 0} sessions · last active {ago(last)} · heartbeat{' '}
                          {a.heartbeat?.enabled ? a.heartbeat.every ?? 'on' : 'off'}
                        </span>
                      </span>
                    </div>
                  )
                })}
              </div>
            ) : null}
          </>
        )}
        {healthError && <p className="error-text">{healthError}</p>}
      </section>

      <section className="panel">
        <div className="settings-toolbar">
          <h3 style={{ flex: 1 }}>Gateway log</h3>
          <input
            type="search"
            placeholder="Filter lines"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            <option value="">All levels</option>
            <option value="warn">Warnings and errors</option>
            <option value="error">Errors only</option>
          </select>
          <label className="check-row check-inline">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
          </label>
          <button className="btn btn-sm" disabled={!connected} onClick={() => void tail()}>
            Fetch
          </button>
        </div>
        <p className="field-hint">
          {file ? `${file} on the gateway host` : 'The gateway’s own log file'}
          {lines.length ? ` · ${visible.length} of ${lines.length} lines` : ''}
        </p>
        {!connected && <p className="field-hint">Connect to a gateway to tail its log.</p>}
        <div className="log-view">
          {visible.map((l) => (
            <div key={l.id} className={`log-line is-${l.level ?? 'info'}`}>
              {l.time && <span className="log-time">{l.time}</span>}
              {l.level && <span className="log-level">{l.level}</span>}
              {l.subsystem && <span className="log-sub">{l.subsystem}</span>}
              <span className="log-text">{l.text}</span>
            </div>
          ))}
          {connected && lines.length === 0 && !logError && <div className="log-line">Nothing yet.</div>}
          <div ref={endRef} />
        </div>
        {logError && <p className="error-text">{logError}</p>}
      </section>
    </div>
  )
}
