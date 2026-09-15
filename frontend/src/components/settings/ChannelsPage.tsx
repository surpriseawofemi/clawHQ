import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../../api'
import { gatewayConfig, humanize } from '../../state/gatewayConfig'
import { SchemaForm } from '../SchemaForm'

/** One account of a channel, as channels.status reports it. */
type Account = {
  accountId: string
  name?: string
  enabled?: boolean
  configured?: boolean
  linked?: boolean
  running?: boolean
  connected?: boolean
  lastError?: string
  lastConnectedAt?: number
  lastInboundAt?: number
  lastOutboundAt?: number
  mode?: string
  dmPolicy?: string
}

type ChannelsStatus = {
  ts?: number
  channelOrder?: string[]
  channelLabels?: Record<string, string>
  channelDetailLabels?: Record<string, string>
  channels?: Record<string, Partial<Account> & { accounts?: Account[] }>
  channelAccounts?: Record<string, Account[]>
  channelDefaultAccountId?: Record<string, string>
}

/** Keys under `channels` in the schema that are not channels themselves. */
const NOT_A_CHANNEL = new Set(['defaults', 'modelByChannel'])

const ago = (ms?: number): string => {
  if (!ms) return 'never'
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.round(s / 60)}m ago` : s < 86_400 ? `${Math.round(s / 3600)}h ago` : `${Math.round(s / 86_400)}d ago`
}

/**
 * Channels on the gateway: Telegram, Discord, Slack and the rest.
 *
 * The catalogue is the `channels` object of the gateway's config schema, so a channel
 * the gateway learns about tomorrow appears here without a release. Status comes
 * from `channels.status`; settings are the same schema-driven form as plugins, and
 * every edit is one `config.patch` the gateway hot-reloads.
 */
export function ChannelsPage({ connected }: { connected: boolean }): React.JSX.Element {
  const [ids, setIds] = useState<string[]>([])
  const [status, setStatus] = useState<ChannelsStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState<string | null>(null)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showAvailable, setShowAvailable] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  const refresh = useCallback(async () => {
    if (!connected) return
    setLoading(true)
    try {
      await gatewayConfig.load()
      const catalogue = gatewayConfig.lookup(['channels'])?.properties ?? {}
      setIds(Object.keys(catalogue).filter((k) => !NOT_A_CHANNEL.has(k)).sort())
      try {
        setStatus(await api.rpc.request<ChannelsStatus>('channels.status', { probe: false }))
      } catch {
        // Status is a bonus; the config form still works without it.
        setStatus(null)
      }
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

  // Live status ticks: the gateway pushes a health event with channel summaries.
  useEffect(() => {
    if (!connected) return
    return api.onGatewayEvent(({ event }) => {
      if (event === 'health') setTick((n) => n + 1)
    })
  }, [connected])

  useEffect(() => {
    if (!connected || tick === 0) return
    api.rpc
      .request<ChannelsStatus>('channels.status', { probe: false })
      .then(setStatus)
      .catch(() => undefined)
  }, [tick, connected])

  const write = async (path: string[], value: unknown): Promise<void> => {
    await gatewayConfig.set(path, value)
    setTick((n) => n + 1)
  }

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

  const describe = (id: string) => {
    const cfg = gatewayConfig.get(['channels', id]) as Record<string, unknown> | undefined
    const configured = !!cfg && Object.keys(cfg).some((k) => k !== 'enabled')
    const enabled = configured && cfg?.enabled !== false
    const accounts = status?.channelAccounts?.[id] ?? status?.channels?.[id]?.accounts ?? []
    const summary = status?.channels?.[id]
    const primary: Partial<Account> | undefined = accounts[0] ?? summary
    const live = !!(primary?.connected ?? primary?.running)
    const label = status?.channelLabels?.[id] ?? gatewayConfig.hint(['channels', id])?.label ?? humanize(id)
    const help = gatewayConfig.hint(['channels', id])?.help ?? gatewayConfig.lookup(['channels', id])?.description ?? ''
    let line = ''
    if (!configured) line = 'not set up'
    else if (!enabled) line = 'disabled'
    else if (primary?.lastError) line = `error: ${primary.lastError}`
    else if (live) line = `connected · last message ${ago(primary?.lastInboundAt)}`
    else if (primary?.configured === false) line = 'configured here, but the gateway says it is missing something'
    else line = 'set up, not running'
    if (accounts.length > 1) line += ` · ${accounts.length} accounts`
    return { configured, enabled, live, label, help, line, hasError: !!primary?.lastError }
  }

  const rows = useMemo(
    () =>
      ids
        .map((id) => ({ id, ...describe(id) }))
        .filter((r) => {
          const q = query.trim().toLowerCase()
          return !q || r.id.includes(q) || r.label.toLowerCase().includes(q)
        }),
    // describe reads module state refreshed by tick/status; both are deps on purpose.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ids, query, status, tick]
  )
  const setUp = rows.filter((r) => r.configured)
  const available = rows.filter((r) => !r.configured)

  const renderRow = (r: (typeof rows)[number]): React.JSX.Element => {
    const isOpen = open === r.id
    const dot = r.hasError ? 'dot-warn' : r.live ? 'dot-ok' : r.enabled ? 'dot-warn' : 'dot-off'
    return (
      <div key={r.id} className="settings-row-block">
        <div className={`gw-row${isOpen ? ' is-active' : ''}`}>
          <i className={`dot ${dot}`} />
          <span className="gw-meta">
            <span className="gw-name">
              {r.label}
              <span className="plugin-desc"> · {r.line}</span>
            </span>
            {r.help && <span className="gw-url plugin-desc">{r.help}</span>}
          </span>
          {r.configured && (
            <button
              className="btn btn-sm"
              disabled={busy !== null}
              onClick={() => void act(`toggle-${r.id}`, () => gatewayConfig.set(['channels', r.id, 'enabled'], !r.enabled))}
            >
              {r.enabled ? 'Disable' : 'Enable'}
            </button>
          )}
          {r.configured && (
            <button
              className="btn btn-sm btn-ghost"
              title="Drop the gateway's stored session for this channel"
              disabled={busy !== null}
              onClick={() => void act(`logout-${r.id}`, () => api.rpc.request('channels.logout', { channel: r.id }))}
            >
              Log out
            </button>
          )}
          <button className="btn btn-sm" onClick={() => setOpen(isOpen ? null : r.id)}>
            {isOpen ? 'Close' : r.configured ? 'Settings' : 'Set up'}
          </button>
        </div>
        {isOpen && (
          <div className="settings-drawer">
            <label className="check-row sf-advanced">
              <input type="checkbox" checked={showAdvanced} onChange={(e) => setShowAdvanced(e.target.checked)} />
              <span>Show advanced fields</span>
            </label>
            <SchemaForm
              path={['channels', r.id]}
              value={gatewayConfig.get(['channels', r.id])}
              onChange={write}
              showAdvanced={showAdvanced}
            />
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>Channels</h3>
        <p className="field-hint">
          Where your agents talk to people: Telegram, Discord, Slack and the rest. The list
          is whatever this gateway supports; settings are its own, edited in place.
        </p>
        <div className="settings-toolbar">
          <input type="search" placeholder="Find a channel" value={query} onChange={(e) => setQuery(e.target.value)} />
          <button className="btn btn-sm" disabled={!connected || busy !== null} onClick={() => void refresh()}>
            Refresh
          </button>
        </div>
        {!connected && <p className="field-hint">Connect to a gateway to see its channels.</p>}
        {loading && connected && <p className="field-hint">Loading…</p>}
        {!loading && connected && setUp.length === 0 && (
          <p className="field-hint">No channel is set up yet. Pick one below.</p>
        )}
        <div className="gw-list">{setUp.map(renderRow)}</div>
        {!loading && connected && available.length > 0 && (
          <>
            <button className="btn btn-sm btn-ghost" onClick={() => setShowAvailable((v) => !v)}>
              {showAvailable ? 'Hide' : 'Show'} {available.length} more {available.length === 1 ? 'channel' : 'channels'}
            </button>
            {showAvailable && <div className="gw-list">{available.map(renderRow)}</div>}
          </>
        )}
        {error && <p className="error-text">{error}</p>}
      </section>

      {connected && !loading && gatewayConfig.lookup(['channels', 'defaults']) && (
        <section className="panel">
          <h3>Channel defaults</h3>
          <p className="field-hint">
            {gatewayConfig.lookup(['channels', 'defaults'])?.description ?? 'Baseline behaviour every channel inherits unless it says otherwise.'}
          </p>
          <SchemaForm
            path={['channels', 'defaults']}
            value={gatewayConfig.get(['channels', 'defaults'])}
            onChange={write}
            showAdvanced={showAdvanced}
          />
        </section>
      )}
    </div>
  )
}
