import { useCallback, useEffect, useState } from 'react'
import { gatewayConfig, enumOptions } from '../../state/gatewayConfig'
import { SchemaForm } from '../SchemaForm'

type ServerRow = {
  name: string
  config: Record<string, unknown>
}

const TRANSPORTS = ['stdio', 'sse', 'streamable-http'] as const
type Transport = (typeof TRANSPORTS)[number]

/**
 * MCP servers the gateway's agents can call, kept under `mcp.servers` in the
 * gateway config. Adding one is a config write; the gateway's runtime picks it up
 * on the next agent turn.
 */
export function McpServersPage({ connected }: { connected: boolean }): React.JSX.Element {
  const [servers, setServers] = useState<ServerRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [showAdvanced, setShowAdvanced] = useState(false)

  const [draft, setDraft] = useState<{ name: string; transport: Transport; command: string; url: string }>({
    name: '',
    transport: 'stdio',
    command: '',
    url: ''
  })
  const [adding, setAdding] = useState(false)

  const refresh = useCallback(async () => {
    if (!connected) return
    setLoading(true)
    try {
      await gatewayConfig.load()
      const raw = gatewayConfig.get(['mcp', 'servers'])
      const rows = raw && typeof raw === 'object' ? Object.entries(raw as Record<string, Record<string, unknown>>) : []
      setServers(rows.map(([name, config]) => ({ name, config: config ?? {} })).sort((a, b) => a.name.localeCompare(b.name)))
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

  const write = async (path: string[], value: unknown): Promise<void> => {
    await gatewayConfig.set(path, value)
    await refresh()
  }

  const add = async (): Promise<void> => {
    const name = draft.name.trim().replace(/[^A-Za-z0-9_.-]+/g, '-')
    if (!name) return
    setAdding(true)
    setError(null)
    try {
      const body: Record<string, unknown> = { transport: draft.transport, enabled: true }
      if (draft.transport === 'stdio') {
        const parts = draft.command.trim().split(/\s+/).filter(Boolean)
        if (parts.length === 0) throw new Error('a command is required for a stdio server')
        body.command = parts[0]
        if (parts.length > 1) body.args = parts.slice(1)
      } else {
        if (!draft.url.trim()) throw new Error('a URL is required for this transport')
        body.url = draft.url.trim()
      }
      await write(['mcp', 'servers', name], body)
      setDraft({ name: '', transport: 'stdio', command: '', url: '' })
      setOpen(name)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setAdding(false)
    }
  }

  const transportOptions = (enumOptions(gatewayConfig.lookup(['mcp', 'servers', 'x', 'transport'])) as string[] | null) ?? [
    ...TRANSPORTS
  ]

  return (
    <div className="settings-stack">
      <section className="panel">
        <h3>MCP servers</h3>
        <p className="field-hint">
          Tool servers every agent on the gateway can call. Stored in the gateway&apos;s own
          config under <code>mcp.servers</code>; a server runs on the gateway host, not on
          this machine.
        </p>
        {!connected && <p className="field-hint">Connect to a gateway to manage MCP servers.</p>}
        {loading && connected && <p className="field-hint">Loading…</p>}

        {servers.length > 0 && (
          <div className="gw-list">
            {servers.map((s) => {
              const enabled = s.config.enabled !== false
              const summary =
                typeof s.config.url === 'string'
                  ? s.config.url
                  : [s.config.command, ...((s.config.args as string[] | undefined) ?? [])].filter(Boolean).join(' ')
              return (
                <div key={s.name} className="settings-row-block">
                  <div className={`gw-row${open === s.name ? ' is-active' : ''}`}>
                    <i className={`dot ${enabled ? 'dot-ok' : 'dot-off'}`} />
                    <span className="gw-meta">
                      <span className="gw-name">{s.name}</span>
                      <span className="gw-url mono">
                        {String(s.config.transport ?? 'stdio')} · {summary || 'not configured'}
                      </span>
                    </span>
                    <label className="check-row" title={enabled ? 'Enabled' : 'Disabled'}>
                      <input
                        type="checkbox"
                        checked={enabled}
                        onChange={(e) => void write(['mcp', 'servers', s.name, 'enabled'], e.target.checked)}
                      />
                    </label>
                    <button className="btn btn-sm" onClick={() => setOpen(open === s.name ? null : s.name)}>
                      {open === s.name ? 'Close' : 'Configure'}
                    </button>
                    <button
                      className="icon-btn"
                      title="Remove server"
                      onClick={() => void write(['mcp', 'servers', s.name], null)}
                    >
                      🗑
                    </button>
                  </div>
                  {open === s.name && (
                    <div className="settings-drawer">
                      <label className="check-row sf-advanced">
                        <input type="checkbox" checked={showAdvanced} onChange={(e) => setShowAdvanced(e.target.checked)} />
                        <span>Show advanced fields</span>
                      </label>
                      <SchemaForm
                        path={['mcp', 'servers', s.name]}
                        value={s.config}
                        onChange={write}
                        showAdvanced={showAdvanced}
                      />
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
        {!loading && connected && servers.length === 0 && (
          <p className="field-hint">No MCP servers configured yet.</p>
        )}
      </section>

      <section className="panel">
        <h3>Add a server</h3>
        <div className="field-row">
          <label className="field">
            <span>Name</span>
            <input
              className="mono"
              placeholder="github"
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </label>
          <label className="field field-narrow">
            <span>Transport</span>
            <select
              value={draft.transport}
              onChange={(e) => setDraft({ ...draft, transport: e.target.value as Transport })}
            >
              {transportOptions.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
        </div>
        {draft.transport === 'stdio' ? (
          <label className="field">
            <span>Command</span>
            <input
              className="mono"
              placeholder="npx -y @modelcontextprotocol/server-github"
              value={draft.command}
              onChange={(e) => setDraft({ ...draft, command: e.target.value })}
            />
            <p className="field-hint">Runs on the gateway host. Environment variables and the rest can be set after adding.</p>
          </label>
        ) : (
          <label className="field">
            <span>URL</span>
            <input
              className="mono"
              placeholder="https://mcp.example.com/sse"
              value={draft.url}
              onChange={(e) => setDraft({ ...draft, url: e.target.value })}
            />
          </label>
        )}
        <div className="btn-row">
          <button
            className="btn btn-primary"
            disabled={!connected || adding || !draft.name.trim() || (draft.transport === 'stdio' ? !draft.command.trim() : !draft.url.trim())}
            onClick={() => void add()}
          >
            {adding ? 'Adding…' : 'Add server'}
          </button>
        </div>
        {error && <p className="error-text">{error}</p>}
      </section>
    </div>
  )
}
