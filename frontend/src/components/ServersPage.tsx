import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { ContentHead, Shell, SideHead, type ShellProps } from './layout/Shell'
import type { ServerHealth, ServerProfile } from '../types'
import { TerminalView } from './TerminalView'

type Props = { shell: ShellProps }
type Tab = 'health' | 'terminal'

const empty = (): ServerProfile => ({ id: '', name: '', host: '', port: 22, user: 'root', auth: 'agent', keyPath: '', password: '', dir: '', addedAtMs: 0 })

const ago = (ms?: number): string => {
  if (!ms) return 'never'
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86_400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86_400)}d ago`
}

/**
 * Servers: machines ClawHQ reaches over SSH with nothing to do with OpenClaw.
 * Save one, and its health page says what is there (system, git, Node, Claude
 * Code and whether it is logged in), offers to install Claude Code, and opens a
 * terminal on it.
 */
export function ServersPage({ shell }: Props): React.JSX.Element {
  const [servers, setServers] = useState<ServerProfile[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [editing, setEditing] = useState<ServerProfile | null>(null)
  const [tab, setTab] = useState<Tab>('health')
  const [health, setHealth] = useState<Record<string, ServerHealth>>({})
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [installLog, setInstallLog] = useState<string | null>(null)
  const [termStatus, setTermStatus] = useState('')

  const load = useCallback(async () => {
    try {
      const list = await api.servers.list()
      setServers(list)
      if (!selectedId && list.length > 0) setSelectedId(list[0].id)
      if (list.length === 0) setEditing(empty())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [selectedId])
  useEffect(() => {
    void load()
  }, [load])

  const selected = servers.find((s) => s.id === selectedId) ?? null
  const h = selected ? health[selected.id] : undefined

  const check = async (id: string): Promise<void> => {
    setBusy(`health:${id}`)
    setError(null)
    try {
      const res = await api.servers.health(id)
      setHealth((prev) => ({ ...prev, [id]: res }))
      void load()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  useEffect(() => {
    if (selected && !health[selected.id] && busy === null) void check(selected.id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id])

  const save = async (): Promise<void> => {
    if (!editing) return
    setBusy('save')
    setError(null)
    try {
      const list = await api.servers.save(editing)
      setServers(list)
      const saved = editing.id ? list.find((s) => s.id === editing.id) : list[list.length - 1]
      setEditing(null)
      if (saved) {
        setSelectedId(saved.id)
        setHealth((prev) => {
          const next = { ...prev }
          delete next[saved.id]
          return next
        })
        setTab('health')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const remove = async (s: ServerProfile): Promise<void> => {
    setBusy(`rm:${s.id}`)
    try {
      const list = await api.servers.remove(s.id)
      setServers(list)
      if (selectedId === s.id) setSelectedId(list[0]?.id ?? null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const install = async (id: string): Promise<void> => {
    setBusy(`install:${id}`)
    setInstallLog('Running the Claude Code installer… this takes a minute.')
    try {
      const out = await api.servers.installClaude(id)
      setInstallLog(out)
      await check(id)
    } catch (err) {
      setInstallLog(`${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setBusy(null)
    }
  }

  const side = (
    <>
      <SideHead>
        <span className="side-title">Servers</span>
        <button className="btn btn-sm btn-primary" onClick={() => setEditing(empty())}>
          + Add
        </button>
      </SideHead>
      <div className="srv-side">
        {servers.length === 0 && <p className="field-hint">No servers yet. Add one with its SSH details.</p>}
        {servers.map((s) => {
          const hh = health[s.id]
          const dot = hh ? (hh.ok ? (hh.claude.installed && hh.claude.loggedIn ? 'is-online' : 'is-working') : 'is-bad') : s.lastOkAtMs ? 'is-idle' : 'is-idle'
          return (
            <button key={s.id} className={`srv-row${selectedId === s.id && !editing ? ' is-selected' : ''}`} onClick={() => { setSelectedId(s.id); setEditing(null) }}>
              <i className={`desk-dot ${dot}`} />
              <span className="srv-row-meta">
                <span className="srv-row-name">{s.name}</span>
                <span className="srv-row-sub">
                  {s.user}@{s.host}
                  {s.port !== 22 ? `:${s.port}` : ''} · {hh ? (hh.ok ? (hh.claude.installed ? `Claude ${hh.claude.version || ''}` : 'no Claude Code') : 'unreachable') : `checked ${ago(s.lastOkAtMs)}`}
                </span>
              </span>
            </button>
          )
        })}
      </div>
    </>
  )

  return (
    <Shell shell={shell} title="Servers" side={side}>
      {editing ? (
        <>
          <ContentHead title={editing.id ? `Edit ${editing.name || editing.host}` : 'Add a server'} subtitle="SSH details stay on this machine. Use your SSH agent or a key when you can; a password is stored in ClawHQ's config file." />
          <div className="content-body issue-detail">
            <div className="issue-form-row">
              <div className="field">
                <span>Name</span>
                <input value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} placeholder="Production" autoFocus />
              </div>
              <div className="field">
                <span>Host</span>
                <input value={editing.host} onChange={(e) => setEditing({ ...editing, host: e.target.value })} placeholder="server.example.com or 100.x.y.z" spellCheck={false} />
              </div>
              <div className="field srv-port">
                <span>Port</span>
                <input type="number" value={editing.port} onChange={(e) => setEditing({ ...editing, port: Number(e.target.value) || 22 })} />
              </div>
            </div>
            <div className="issue-form-row">
              <div className="field">
                <span>User</span>
                <input value={editing.user} onChange={(e) => setEditing({ ...editing, user: e.target.value })} placeholder="root" spellCheck={false} />
              </div>
              <div className="field">
                <span>Authentication</span>
                <select value={editing.auth} onChange={(e) => setEditing({ ...editing, auth: e.target.value as ServerProfile['auth'] })}>
                  <option value="agent">SSH agent / keys in ~/.ssh</option>
                  <option value="key">Key file</option>
                  <option value="password">Password</option>
                </select>
              </div>
            </div>
            {editing.auth === 'key' && (
              <div className="field">
                <span>Key file</span>
                <input value={editing.keyPath ?? ''} onChange={(e) => setEditing({ ...editing, keyPath: e.target.value })} placeholder="~/.ssh/id_ed25519" spellCheck={false} />
              </div>
            )}
            {(editing.auth === 'password' || editing.auth === 'key') && (
              <div className="field">
                <span>{editing.auth === 'key' ? 'Key passphrase (if any)' : 'Password'}</span>
                <input type="password" value={editing.password ?? ''} onChange={(e) => setEditing({ ...editing, password: e.target.value })} placeholder={editing.hasPassword ? 'unchanged' : ''} />
              </div>
            )}
            <div className="field">
              <span>Project folder (optional)</span>
              <input value={editing.dir ?? ''} onChange={(e) => setEditing({ ...editing, dir: e.target.value })} placeholder="/var/www/emailmanager" spellCheck={false} />
              <p className="field-hint">The terminal opens here, and Claude Code will work in it.</p>
            </div>
            <div className="btn-row">
              <button className="btn btn-primary" onClick={() => void save()} disabled={busy === 'save' || !editing.host.trim() || !editing.user.trim()}>
                {busy === 'save' ? 'Saving…' : 'Save and check'}
              </button>
              {servers.length > 0 && (
                <button className="btn" onClick={() => setEditing(null)}>
                  Cancel
                </button>
              )}
            </div>
            {error && <p className="error-text">{error}</p>}
          </div>
        </>
      ) : selected ? (
        <>
          <ContentHead
            title={selected.name}
            subtitle={`${selected.user}@${selected.host}${selected.port !== 22 ? `:${selected.port}` : ''}${selected.dir ? ` · ${selected.dir}` : ''}`}
            onRefresh={tab === 'health' ? () => void check(selected.id) : undefined}
            refreshing={busy === `health:${selected.id}`}
          >
            <div className="seg">
              <button className={tab === 'health' ? 'is-active' : ''} onClick={() => setTab('health')}>
                Health
              </button>
              <button className={tab === 'terminal' ? 'is-active' : ''} onClick={() => setTab('terminal')}>
                Terminal{termStatus === 'connected' ? ' ·' : ''}
              </button>
            </div>
            <button className="btn btn-sm btn-ghost" onClick={() => setEditing({ ...selected, password: '' })}>
              Edit
            </button>
            <button className="btn btn-sm btn-ghost" onClick={() => void remove(selected)} disabled={busy !== null}>
              Forget
            </button>
          </ContentHead>
          {tab === 'terminal' ? (
            <div className="content-body term-body">
              <TerminalView serverId={selected.id} onStatus={setTermStatus} />
              <p className="field-hint">
                A login shell on the server. Run <code>claude</code> here for the full Claude Code, or <code>claude</code> then <code>/login</code> once to sign in.
              </p>
            </div>
          ) : (
            <div className="content-body issue-detail">
              {error && <p className="error-text">{error}</p>}
              {!h && <p className="field-hint">Checking…</p>}
              {h && !h.ok && (
                <div className="srv-unreachable">
                  <p className="error-text">Could not check this server: {h.error}</p>
                  <p className="field-hint">Host, user, key or password wrong? Edit the server. Host key changed on purpose? Forget it and add it again.</p>
                </div>
              )}
              {h && h.ok && (
                <>
                  <div className="srv-facts">
                    <span>⏱ {h.uptime || '—'}</span>
                    <span>📈 load {h.load || '—'}</span>
                  </div>
                  <div className="srv-checks">
                    {h.checks.map((c) => (
                      <div key={c.id} className={`srv-check${c.ok ? ' is-ok' : ' is-bad'}`}>
                        <span className="srv-check-icon">{c.ok ? '✅' : '❌'}</span>
                        <span className="srv-check-label">{c.label}</span>
                        <span className="srv-check-value">{c.value}</span>
                        {!c.ok && c.hint && <span className="srv-check-hint">{c.hint}</span>}
                      </div>
                    ))}
                  </div>
                  <div className="btn-row">
                    {!h.claude.installed && (
                      <button className="btn btn-primary" onClick={() => void install(selected.id)} disabled={busy !== null}>
                        {busy === `install:${selected.id}` ? 'Installing…' : 'Install Claude Code'}
                      </button>
                    )}
                    {h.claude.installed && !h.claude.loggedIn && (
                      <button className="btn btn-primary" onClick={() => setTab('terminal')}>
                        Open terminal to log in
                      </button>
                    )}
                    <button className="btn" onClick={() => setTab('terminal')}>
                      Open terminal
                    </button>
                  </div>
                  {installLog && <pre className="md-source join-cmd srv-log">{installLog}</pre>}
                  <p className="field-hint">Checked {ago(h.checkedAtMs)}. Coming next: a chat with Claude Code on this server, and other coding agents.</p>
                </>
              )}
            </div>
          )}
        </>
      ) : (
        <>
          <ContentHead title="Servers" subtitle="Machines you reach over SSH." />
          <div className="content-body">
            <p className="field-hint">Add a server to see its health and open a terminal on it.</p>
          </div>
        </>
      )}
    </Shell>
  )
}
