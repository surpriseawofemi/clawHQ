import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../../api'
import { gatewayConfig } from '../../state/gatewayConfig'
import { SchemaForm } from '../SchemaForm'

/** One row of plugins.list. */
type PluginRow = {
  id: string
  name?: string
  description?: string
  version?: string
  origin?: string
  packageName?: string
  installed?: boolean
  enabled?: boolean
  removable?: boolean
  state?: 'enabled' | 'disabled' | 'not-installed' | string
  categories?: string[]
  kind?: string[]
  /** Ready-made params for plugins.install, present on plugins that are not installed. */
  install?: Record<string, unknown>
}

type Inspect = {
  declared?: {
    channels?: string[]
    providers?: string[]
    tools?: string[]
    mcpServers?: string[]
    cliCommands?: string[]
    skills?: string[]
  }
}

type SearchHit = {
  score?: number
  package: {
    name: string
    displayName?: string
    summary?: string
    isOfficial?: boolean
    latestVersion?: string
    runtimeId?: string
  }
}

type Filter = 'enabled' | 'disabled' | 'available' | 'all'

/**
 * Plugins on the gateway: what is on, what is off, what can be installed.
 *
 * Everything here is a gateway RPC, so it works over a tunnel. Enabling is a config
 * write the gateway hot-reloads; installing pulls the package on the gateway host.
 * Each plugin's own settings are rendered from the schema the gateway publishes for
 * it, so a plugin ClawHQ has never heard of still gets a proper form.
 */
export function PluginsPage({ connected }: { connected: boolean }): React.JSX.Element {
  const [rows, setRows] = useState<PluginRow[]>([])
  const [mutationAllowed, setMutationAllowed] = useState(true)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<Filter>('enabled')
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [inspect, setInspect] = useState<Record<string, Inspect>>({})
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null)

  const [hubQuery, setHubQuery] = useState('')
  const [hubHits, setHubHits] = useState<SearchHit[] | null>(null)
  const [hubBusy, setHubBusy] = useState(false)

  const refresh = useCallback(async () => {
    if (!connected) return
    setLoading(true)
    try {
      const [list] = await Promise.all([
        api.rpc.request<{ plugins?: PluginRow[]; mutationAllowed?: boolean }>('plugins.list'),
        gatewayConfig.load()
      ])
      setRows(list?.plugins ?? [])
      setMutationAllowed(list?.mutationAllowed !== false)
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

  const setEnabled = (row: PluginRow, enabled: boolean): Promise<void> =>
    act(`enable-${row.id}`, () => api.rpc.request('plugins.setEnabled', { pluginId: row.id, enabled }))

  const install = (row: PluginRow): Promise<void> =>
    act(`install-${row.id}`, () =>
      api.rpc.request('plugins.install', row.install ?? { source: 'official', pluginId: row.id })
    )

  const uninstall = (row: PluginRow): Promise<void> => {
    setConfirmRemove(null)
    return act(`remove-${row.id}`, () => api.rpc.request('plugins.uninstall', { pluginId: row.id }))
  }

  const toggleOpen = async (row: PluginRow): Promise<void> => {
    if (open === row.id) {
      setOpen(null)
      return
    }
    setOpen(row.id)
    if (!inspect[row.id]) {
      try {
        const res = await api.rpc.request<Inspect>('plugins.inspect', { pluginId: row.id })
        setInspect((prev) => ({ ...prev, [row.id]: res }))
      } catch {
        // The form still works without the declared-capabilities summary.
      }
    }
  }

  const write = async (path: string[], value: unknown): Promise<void> => {
    await gatewayConfig.set(path, value)
  }

  const searchHub = async (): Promise<void> => {
    const q = hubQuery.trim()
    if (!q) return
    setHubBusy(true)
    setError(null)
    try {
      const res = await api.rpc.request<{ results?: SearchHit[] }>('plugins.search', { query: q, limit: 12 })
      setHubHits(res?.results ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setHubBusy(false)
    }
  }

  const installFromHub = (hit: SearchHit): Promise<void> =>
    act(`hub-${hit.package.name}`, () =>
      api.rpc.request('plugins.install', { source: 'clawhub', packageName: hit.package.name })
    )

  const counts = useMemo(
    () => ({
      enabled: rows.filter((r) => r.state === 'enabled').length,
      disabled: rows.filter((r) => r.state === 'disabled').length,
      available: rows.filter((r) => r.installed === false).length,
      all: rows.length
    }),
    [rows]
  )

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return rows
      .filter((r) => {
        switch (filter) {
          case 'enabled':
            return r.state === 'enabled'
          case 'disabled':
            return r.state === 'disabled'
          case 'available':
            return r.installed === false
          default:
            return true
        }
      })
      .filter((r) =>
        q
          ? `${r.id} ${r.name ?? ''} ${r.description ?? ''} ${(r.categories ?? []).join(' ')}`
              .toLowerCase()
              .includes(q)
          : true
      )
      .sort((a, b) => (a.name ?? a.id).localeCompare(b.name ?? b.id))
  }, [rows, filter, query])

  const filters: { id: Filter; label: string }[] = [
    { id: 'enabled', label: `On (${counts.enabled})` },
    { id: 'disabled', label: `Off (${counts.disabled})` },
    { id: 'available', label: `Available (${counts.available})` },
    { id: 'all', label: `All (${counts.all})` }
  ]

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>Plugins</h3>
        <p className="field-hint">
          Providers, channels, tools and memory for every agent on this gateway. Switching
          one on hot-reloads it for new agent turns; installing pulls it onto the gateway
          host.
        </p>
        {!mutationAllowed && (
          <p className="note-text">This gateway does not allow plugin changes from clients.</p>
        )}
        {!connected && <p className="field-hint">Connect to a gateway to manage its plugins.</p>}

        <div className="settings-toolbar">
          <div className="tabs tabs-inline">
            {filters.map((f) => (
              <button
                key={f.id}
                className={`tab${filter === f.id ? ' is-active' : ''}`}
                onClick={() => setFilter(f.id)}
              >
                {f.label}
              </button>
            ))}
          </div>
          <input
            type="search"
            placeholder="Filter by name or category"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        {loading && connected && <p className="field-hint">Loading…</p>}
        {!loading && connected && visible.length === 0 && <p className="field-hint">Nothing matches.</p>}

        <div className="gw-list">
          {visible.map((row) => {
            const installed = row.installed !== false
            const on = row.state === 'enabled'
            const isOpen = open === row.id
            const info = inspect[row.id]?.declared
            const configSchema = gatewayConfig.lookup(['plugins', 'entries', row.id, 'config'])
            const hasConfig = !!configSchema?.properties && Object.keys(configSchema.properties).length > 0
            return (
              <div key={row.id} className="settings-row-block">
                <div className={`gw-row${isOpen ? ' is-active' : ''}`}>
                  <i className={`dot ${on ? 'dot-ok' : installed ? 'dot-off' : 'dot-off'}`} />
                  <span className="gw-meta">
                    <span className="gw-name">
                      {row.name ?? row.id}
                      <span className="plugin-desc">
                        {' '}
                        · {row.version ?? ''} {row.origin ?? ''}
                        {(row.categories ?? []).length > 0 ? ` · ${(row.categories ?? []).join(', ')}` : ''}
                      </span>
                    </span>
                    <span className="gw-url plugin-desc">{row.description ?? row.packageName ?? ''}</span>
                  </span>
                  {installed ? (
                    <>
                      <label className="check-row" title={on ? 'Enabled' : 'Disabled'}>
                        <input
                          type="checkbox"
                          checked={on}
                          disabled={!mutationAllowed || busy !== null}
                          onChange={(e) => void setEnabled(row, e.target.checked)}
                        />
                      </label>
                      <button className="btn btn-sm" onClick={() => void toggleOpen(row)}>
                        {isOpen ? 'Close' : hasConfig ? 'Configure' : 'Details'}
                      </button>
                      {row.removable !== false &&
                        (confirmRemove === row.id ? (
                          <button
                            className="btn btn-sm btn-danger"
                            disabled={busy !== null}
                            onClick={() => void uninstall(row)}
                          >
                            Really remove
                          </button>
                        ) : (
                          <button
                            className="icon-btn"
                            title="Uninstall"
                            disabled={!mutationAllowed || busy !== null}
                            onClick={() => setConfirmRemove(row.id)}
                          >
                            🗑
                          </button>
                        ))}
                    </>
                  ) : (
                    <button
                      className="btn btn-sm btn-primary"
                      disabled={!mutationAllowed || busy !== null}
                      onClick={() => void install(row)}
                    >
                      {busy === `install-${row.id}` ? 'Installing…' : 'Install'}
                    </button>
                  )}
                </div>
                {isOpen && (
                  <div className="settings-drawer">
                    {info && (
                      <p className="field-hint">
                        {[
                          info.providers?.length ? `providers: ${info.providers.join(', ')}` : '',
                          info.channels?.length ? `channels: ${info.channels.join(', ')}` : '',
                          info.tools?.length ? `tools: ${info.tools.join(', ')}` : '',
                          info.mcpServers?.length ? `MCP servers: ${info.mcpServers.join(', ')}` : '',
                          info.skills?.length ? `skills: ${info.skills.join(', ')}` : ''
                        ]
                          .filter(Boolean)
                          .join(' · ') || 'No declared providers, channels or tools.'}
                      </p>
                    )}
                    {hasConfig ? (
                      <>
                        <label className="check-row sf-advanced">
                          <input
                            type="checkbox"
                            checked={showAdvanced}
                            onChange={(e) => setShowAdvanced(e.target.checked)}
                          />
                          <span>Show advanced fields</span>
                        </label>
                        <SchemaForm
                          path={['plugins', 'entries', row.id, 'config']}
                          value={gatewayConfig.get(['plugins', 'entries', row.id, 'config'])}
                          onChange={write}
                          showAdvanced={showAdvanced}
                        />
                      </>
                    ) : (
                      <p className="field-hint">This plugin has no settings of its own.</p>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
        {error && <p className="error-text">{error}</p>}
      </section>

      <section className="panel">
        <h3>Find more on ClawHub</h3>
        <p className="field-hint">
          Community and official packages. Installing runs on the gateway host and needs
          network access there.
        </p>
        <div className="dept-edit-row">
          <input
            type="search"
            placeholder="whatsapp, notion, jira…"
            value={hubQuery}
            onChange={(e) => setHubQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void searchHub()
            }}
          />
          <button className="btn" disabled={!connected || hubBusy || !hubQuery.trim()} onClick={() => void searchHub()}>
            {hubBusy ? 'Searching…' : 'Search'}
          </button>
        </div>
        {hubHits && hubHits.length === 0 && <p className="field-hint">No packages match.</p>}
        {hubHits && hubHits.length > 0 && (
          <div className="gw-list">
            {hubHits.map((hit) => {
              const already = rows.find((r) => r.packageName === hit.package.name)
              return (
                <div key={hit.package.name} className="gw-row">
                  <span className="gw-meta">
                    <span className="gw-name">
                      {hit.package.displayName ?? hit.package.name}
                      <span className="plugin-desc">
                        {' '}
                        · {hit.package.isOfficial ? 'official' : 'community'} {hit.package.latestVersion ?? ''}
                      </span>
                    </span>
                    <span className="gw-url plugin-desc">{hit.package.summary ?? hit.package.name}</span>
                  </span>
                  {already?.installed ? (
                    <span className="field-hint">installed</span>
                  ) : (
                    <button
                      className="btn btn-sm btn-primary"
                      disabled={!mutationAllowed || busy !== null}
                      onClick={() => void (already ? install(already) : installFromHub(hit))}
                    >
                      {busy === `hub-${hit.package.name}` || busy === `install-${already?.id}` ? 'Installing…' : 'Install'}
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}
