import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { ContentHead, Shell, SideHead, type ShellProps } from './layout/Shell'
import type { AgentStatus, HelperStatus, ProjectInfo, ServerHealth, ServerProfile, ServerProject } from '../types'
import { AutopilotView } from './AutopilotView'
import { serverNav, liveSessionName } from '../state/serverNav'
import { TerminalTabs } from './TerminalView'
import { terminals } from '../state/terminals'
import { ActionsView } from './ActionsView'
import { FilesView } from './FilesView'
import { ClaudeChat } from './ClaudeChat'

type Props = { shell: ShellProps }

const pageMemory: { servers: ServerProfile[]; selectedId: string | null; tab: Tab; health: Record<string, ServerHealth>; projInfo: Record<string, ProjectInfo>; helper: Record<string, HelperStatus> } = {
  servers: [],
  selectedId: null,
  tab: 'health',
  health: {},
  projInfo: {},
  helper: {}
}
/** Text the chat tab should start with (from Discuss / Fix on an issue). */
let chatPrefill = ''
export const takeChatPrefill = (): string => {
  const v = chatPrefill
  chatPrefill = ''
  return v
}
type Tab = 'health' | 'chat' | 'files' | 'actions' | 'terminal' | 'autopilot'

const empty = (): ServerProfile => ({ id: '', name: '', host: '', port: 22, user: 'root', auth: 'agent', keyPath: '', password: '', dir: '', addedAtMs: 0, monitor: true, tmux: true })

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
  // Leaving the page and coming back keeps everything: the server list, which one
  // was open, its tab, health results and project cards live outside React.
  const [servers, setServers] = useState<ServerProfile[]>(pageMemory.servers)
  const [selectedId, setSelectedId] = useState<string | null>(pageMemory.selectedId)
  const [editing, setEditing] = useState<ServerProfile | null>(null)
  const [tab, setTab] = useState<Tab>(pageMemory.tab)
  const [health, setHealth] = useState<Record<string, ServerHealth>>(pageMemory.health)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [installLog, setInstallLog] = useState<string | null>(null)
  const [helper, setHelper] = useState<Record<string, HelperStatus>>(pageMemory.helper)
  const loadHelper = async (id: string): Promise<void> => {
    try {
      const st = await api.helper.status(id)
      setHelper((prev) => ({ ...prev, [id]: st }))
    } catch {
      /* unreachable */
    }
  }
  const [projForm, setProjForm] = useState<{ id: string; name: string; dir: string; agent: string } | null>(null)
  // What the active project's folder holds, read once per project.
  const [projInfo, setProjInfo] = useState<Record<string, ProjectInfo>>(pageMemory.projInfo)
  useEffect(() => {
    Object.assign(pageMemory, { servers, selectedId, tab, health, projInfo, helper })
  }, [servers, selectedId, tab, health, projInfo, helper])
  // A request from another page (Issues, the bell): open this server here.
  useEffect(() => {
    const nav = serverNav.take()
    if (!nav) return
    setSelectedId(nav.serverId)
    setEditing(null)
    if (nav.prefill) chatPrefill = nav.prefill
    if (nav.projectId) void projectAction(() => api.servers.selectProject(nav.serverId, nav.projectId as string))
    if (nav.tab) setTab(nav.tab)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const infoKey = (s: ServerProfile, p: ServerProject): string => `${s.id}:${p.id}:${p.dir}`
  const loadInfo = async (s: ServerProfile, p: ServerProject): Promise<void> => {
    try {
      const info = await api.servers.projectInfo(s.id, p.dir)
      setProjInfo((prev) => ({ ...prev, [infoKey(s, p)]: info }))
    } catch {
      /* unreachable; the health row says so */
    }
  }

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
      if (res.ok) void loadHelper(id)
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
  useEffect(() => {
    const p = selected ? activeProject(selected) : undefined
    if (selected && p && p.dir && !projInfo[infoKey(selected, p)]) void loadInfo(selected, p)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id, selected?.activeProjectId])

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

  const install = async (id: string, agent: AgentStatus['id'] = 'claude'): Promise<void> => {
    setBusy(`install:${id}:${agent}`)
    setInstallLog(`Installing… this takes a minute.`)
    try {
      const out = await api.servers.installAgent(id, agent)
      setInstallLog(out)
      await check(id)
    } catch (err) {
      setInstallLog(`${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setBusy(null)
    }
  }

  const terminalsFor = (serverId: string, projectId: string): number => terminals.list(serverId, projectId).length
  const activeProject = (s: ServerProfile): ServerProject | undefined => (s.projects ?? []).find((p) => p.id === s.activeProjectId) ?? s.projects?.[0]
  const projectAction = async (fn: () => Promise<ServerProfile[]>): Promise<void> => {
    setBusy('project')
    setError(null)
    try {
      const list = await fn()
      setServers(list)
      setProjForm(null)
      const sv = list.find((x) => x.id === selectedId)
      const ap = sv ? activeProject(sv) : undefined
      if (sv && ap && ap.dir) void loadInfo(sv, ap)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
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
            <div key={s.id} className="srv-block">
              <button className={`srv-row${selectedId === s.id && !editing ? ' is-selected' : ''}`} onClick={() => { setSelectedId(s.id); setEditing(null) }}>
                <i className={`desk-dot ${dot}`} />
                <span className="srv-row-meta">
                  <span className="srv-row-name">{s.name}</span>
                  <span className="srv-row-sub">
                    {s.user}@{s.host}
                    {s.port !== 22 ? `:${s.port}` : ''} · {hh ? (hh.ok ? (hh.claude.installed ? `Claude ${hh.claude.version || ''}` : 'no Claude Code') : 'unreachable') : `checked ${ago(s.lastOkAtMs)}`}
                  </span>
                </span>
              </button>
              {selectedId === s.id && (
                <div className="proj-list">
                  {(s.projects ?? []).map((p) => (
                    <div key={p.id} className={`proj-row${p.id === s.activeProjectId && !projForm ? ' is-active' : ''}`}>
                      <button className="proj-main" onClick={() => { setEditing(null); setProjForm(null); void projectAction(() => api.servers.selectProject(s.id, p.id)) }} title={p.dir || 'home folder'}>
                        <span className="proj-icon">📁</span>
                        <span className="proj-meta">
                          <span className="proj-name">{p.name}</span>
                          <span className="proj-sub">{p.dir || '~'} · {p.agent ?? 'claude'}{terminalsFor(s.id, p.id) ? ` · ${terminalsFor(s.id, p.id)} term` : ''}</span>
                        </span>
                      </button>
                      <span className="proj-tools">
                        <button className="icon-btn" title="Edit project" onClick={() => setProjForm({ id: p.id, name: p.name, dir: p.dir, agent: p.agent ?? 'claude' })}>✎</button>
                        {(s.projects?.length ?? 0) > 1 && (
                          <button className="icon-btn" title="Remove from the list (files stay on the server)" onClick={() => void projectAction(() => api.servers.removeProject(s.id, p.id))}>🗑</button>
                        )}
                      </span>
                    </div>
                  ))}
                  <button className="proj-add" onClick={() => { setEditing(null); setProjForm({ id: '', name: '', dir: '', agent: 'claude' }) }}>＋ Project</button>
                </div>
              )}
            </div>
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
            <label className="check-row">
              <input type="checkbox" checked={editing.monitor !== false} onChange={(e) => setEditing({ ...editing, monitor: e.target.checked })} />
              <span>Watch health every 10 minutes and warn in the bell (not answering, disk over 90%, high load)</span>
            </label>
            <label className="check-row">
              <input type="checkbox" checked={editing.tmux !== false} onChange={(e) => setEditing({ ...editing, tmux: e.target.checked })} />
              <span>Run terminals in tmux so they survive closing ClawHQ (needs tmux on the server)</span>
            </label>
            <div className="field">
              <span>{editing.id ? 'Active project folder' : 'Project folder (optional)'}</span>
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
            subtitle={`${selected.user}@${selected.host}${selected.port !== 22 ? `:${selected.port}` : ''}${activeProject(selected) ? ` · ${activeProject(selected)?.name}${activeProject(selected)?.dir ? ` (${activeProject(selected)?.dir})` : ''}` : ''}`}
            onRefresh={tab === 'health' ? () => void check(selected.id) : undefined}
            refreshing={busy === `health:${selected.id}`}
          >
            <div className="seg">
              <button className={tab === 'health' ? 'is-active' : ''} onClick={() => setTab('health')}>
                Health
              </button>
              <button className={tab === 'chat' ? 'is-active' : ''} onClick={() => setTab('chat')}>
                Chat
              </button>
              <button className={tab === 'autopilot' ? 'is-active' : ''} onClick={() => setTab('autopilot')} title="Mission, live session, issues and log for this project">
                Autopilot{(helper[selected.id]?.projects.find((p) => p.projectId === selected.activeProjectId)?.openIssues ?? 0) > 0 ? ` · ${helper[selected.id]?.projects.find((p) => p.projectId === selected.activeProjectId)?.openIssues}` : ''}
              </button>
              <button className={tab === 'files' ? 'is-active' : ''} onClick={() => setTab('files')}>
                Files
              </button>
              <button className={tab === 'actions' ? 'is-active' : ''} onClick={() => setTab('actions')}>
                Actions
              </button>
              <button className={tab === 'terminal' ? 'is-active' : ''} onClick={() => setTab('terminal')}>
                Terminal
              </button>
            </div>
            {health[selected.id]?.ok ? (
              <button
                className="btn btn-sm btn-ghost"
                title="Close every SSH connection to this server (terminals, files, chat). tmux sessions keep running."
                disabled={busy !== null}
                onClick={() => {
                  setBusy('disconnect')
                  terminals.detachAll(selected.id)
                  void api.servers
                    .disconnect(selected.id)
                    .then(() => setHealth((prev) => ({ ...prev, [selected.id]: { ...(prev[selected.id] as ServerHealth), ok: false, error: 'Disconnected. Press Reconnect to check the server again.' } })))
                    .catch((err) => setError(String(err)))
                    .finally(() => { setBusy(null); setTab('health') })
                }}
              >
                Disconnect
              </button>
            ) : (
              <button className="btn btn-sm btn-primary" disabled={busy !== null} onClick={() => void check(selected.id)}>
                {busy === `health:${selected.id}` ? 'Connecting…' : 'Reconnect'}
              </button>
            )}
            <button className="btn btn-sm btn-ghost" onClick={() => setEditing({ ...selected, password: '' })}>
              Edit
            </button>
            <button className="btn btn-sm btn-ghost" onClick={() => void remove(selected)} disabled={busy !== null}>
              Forget
            </button>
          </ContentHead>
          {projForm && (
            <form
              className="proj-form"
              onSubmit={(e) => {
                e.preventDefault()
                void projectAction(() => (projForm.id ? api.servers.updateProject(selected.id, projForm.id, projForm.name, projForm.dir) : api.servers.addProject(selected.id, projForm.name, projForm.dir, projForm.agent)))
              }}
            >
              <div className="field">
                <span>Name</span>
                <input value={projForm.name} onChange={(e) => setProjForm({ ...projForm, name: e.target.value })} placeholder="emailmanager.pro" autoFocus />
              </div>
              <div className="field">
                <span>Folder on the server</span>
                <input value={projForm.dir} onChange={(e) => setProjForm({ ...projForm, dir: e.target.value })} placeholder="/var/www/emailmanager" spellCheck={false} />
              </div>
              {!projForm.id && (
                <div className="field">
                  <span>Agent</span>
                  <select value={projForm.agent} onChange={(e) => setProjForm({ ...projForm, agent: e.target.value })}>
                    <option value="claude">Claude Code</option>
                    <option value="codex">Codex</option>
                    <option value="gemini">Gemini CLI</option>
                    <option value="grok">Grok CLI</option>
                  </select>
                </div>
              )}
              <div className="btn-row">
                <button type="submit" className="btn btn-primary" disabled={busy === 'project' || (!projForm.name.trim() && !projForm.dir.trim())}>{projForm.id ? 'Save' : 'Add'}</button>
                <button type="button" className="btn" onClick={() => setProjForm(null)}>Cancel</button>
              </div>
            </form>
          )}
          {tab === 'autopilot' && activeProject(selected) ? (
            <div className="content-body">
              <AutopilotView
                server={selected}
                project={activeProject(selected) as ServerProject}
                helper={helper[selected.id] ?? null}
                onHelper={(st) => setHelper((prev) => ({ ...prev, [selected.id]: st }))}
                onDiscuss={(text) => {
                  chatPrefill = text
                  setTab('chat')
                }}
              />
            </div>
          ) : tab === 'chat' ? (
            <div className="content-body cc-body">
              {(() => {
                const agentId = activeProject(selected)?.agent ?? 'claude'
                const st = h?.agents?.find((a) => a.id === agentId)
                if (h && h.ok && st && !st.installed) return <p className="field-hint">{st.label} is not installed on this server yet. Install it from the Health tab.</p>
                if (h && h.ok && st && !st.loggedIn) return <p className="field-hint">{st.label} is not logged in on this server. Open the terminal and {st.loginHint}.</p>
                const ap = activeProject(selected)
                return (
                  <ClaudeChat
                    key={`${selected.id}:${selected.activeProjectId ?? ''}`}
                    serverId={selected.id}
                    serverName={selected.name}
                    live={ap ? { projectId: ap.id, projectName: ap.name, dir: ap.dir, session: liveSessionName(ap.name), helperWired: helper[selected.id]?.projects.find((p) => p.projectId === ap.id)?.hooks ?? false } : undefined}
                  />
                )
              })()}
            </div>
          ) : tab === 'actions' ? (
            <div className="content-body acts-body">
              <ActionsView
                serverId={selected.id}
                projectId={activeProject(selected)?.id ?? ''}
                projectName={activeProject(selected)?.name ?? ''}
                dir={activeProject(selected)?.dir ?? ''}
                actions={selected.actions ?? []}
                suggested={activeProject(selected) ? (projInfo[infoKey(selected, activeProject(selected) as ServerProject)]?.suggested ?? []) : []}
                onActions={(a) => setServers((prev) => prev.map((s) => (s.id === selected.id ? { ...s, actions: a } : s)))}
              />
            </div>
          ) : tab === 'files' ? (
            <div className="content-body files-body">
              <FilesView key={`${selected.id}:${selected.activeProjectId ?? ''}`} serverId={selected.id} startDir={activeProject(selected)?.dir || undefined} memoryKey={`${selected.id}:${selected.activeProjectId ?? ''}`} />
            </div>
          ) : tab === 'terminal' ? (
            <div className="content-body term-body">
              <TerminalTabs serverId={selected.id} projectId={activeProject(selected)?.id ?? ''} dir={activeProject(selected)?.dir ?? ''} tmux={selected.tmux !== false && (h?.tmux?.installed ?? false)} />
              <p className="field-hint">
                {selected.tmux !== false && h?.tmux?.installed
                  ? 'Each tab is a tmux session on the server: it survives closing ClawHQ and comes back here. × ends it, ⇣ detaches and keeps it running. Drag to select (tmux copies it to your Mac), hold ⌥ to select in the page, right-click to paste.'
                  : 'Login shells on the server; they stay open while ClawHQ runs. Install tmux on Health to keep them across restarts.'}{' '}
                Run <code>claude</code> here for the full Claude Code.
              </p>
            </div>
          ) : (
            <div className="content-body issue-detail">
              {error && <p className="error-text">{error}</p>}
              {!h && <p className="field-hint">Checking…</p>}
              {h && !h.ok && h.error?.startsWith('Disconnected') && (
                <div className="srv-unreachable">
                  <p className="field-hint">{h.error}</p>
                </div>
              )}
              {h && !h.ok && !h.error?.startsWith('Disconnected') && (
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
                        {!c.ok && c.hint && (
                          <span className="srv-check-hint">
                            {c.hint}{' '}
                            {c.id === 'tmux' && (
                              <button className="btn btn-sm" disabled={busy !== null} onClick={() => { setBusy('tmux'); setInstallLog('Installing tmux…'); void api.servers.installTmux(selected.id).then((out) => setInstallLog(out)).catch((err) => setInstallLog(String(err))).finally(() => { setBusy(null); void check(selected.id) }) }}>
                                {busy === 'tmux' ? 'Installing…' : 'Install tmux'}
                              </button>
                            )}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                  {activeProject(selected) && (() => {
                    const p = activeProject(selected) as ServerProject
                    const info = projInfo[infoKey(selected, p)]
                    return (
                      <>
                        <h4 className="srv-h4">Project · {p.name}</h4>
                        <div className="proj-card">
                          {!p.dir && <span className="plugin-desc">No folder set; edit the project to point it at one.</span>}
                          {p.dir && !info && <span className="plugin-desc">Reading {p.dir}…</span>}
                          {info && !info.exists && <span className="error-text">{info.dir} does not exist on the server.</span>}
                          {info && info.exists && (
                            <>
                              <span className="proj-fact mono">{info.dir}</span>
                              {info.gitBranch && <span className="proj-fact">🌿 {info.gitBranch}{info.gitDirty ? ` · ${info.gitDirty} changed` : ' · clean'}</span>}
                              {info.gitRemote && <span className="proj-fact mono" title={info.gitRemote}>{info.gitRemote.replace(/^.*[:/]([^/]+\/[^/]+?)(\.git)?$/, '$1')}</span>}
                              {info.package && <span className="proj-fact">📦 {info.package}{info.scripts.length ? ` · ${info.scripts.length} scripts` : ''}</span>}
                              {info.goModule && <span className="proj-fact">🐹 {info.goModule}</span>}
                              {info.composer && <span className="proj-fact">🐘 {info.composer}</span>}
                              {info.python && <span className="proj-fact">🐍 python</span>}
                              {info.docker && <span className="proj-fact">🐳 docker</span>}
                              {info.pm2 && <span className="proj-fact">⚙️ pm2: {info.pm2}</span>}
                              {info.hasClaudeMd && <span className="proj-fact">📝 CLAUDE.md</span>}
                              {info.hasEnv && <span className="proj-fact">🔑 .env</span>}
                              <span className="proj-fact plugin-desc">{info.files} entries</span>
                              <button className="icon-btn" title="Read the folder again" onClick={() => void loadInfo(selected, p)}>↻</button>
                            </>
                          )}
                        </div>
                      </>
                    )
                  })()}
                  <h4 className="srv-h4">ClawHQ helper</h4>
                  <div className="srv-checks">
                    {(() => {
                      const hs = helper[selected.id]
                      const w = hs?.projects.find((x) => x.projectId === selected.activeProjectId)
                      return (
                        <>
                          <div className={`srv-check srv-agent${hs?.installed ? (hs.current ? ' is-ok' : ' is-warn') : ''}`}>
                            <span className="srv-check-icon">{hs?.installed ? (hs.current ? '✅' : '🟡') : '⬜'}</span>
                            <span className="srv-check-label">Helper</span>
                            <span className="srv-check-value">{hs ? (hs.installed ? `${hs.version}${hs.current ? '' : ' · ClawHQ has a newer one'}` : 'not installed') : 'checking…'}</span>
                            <span className="srv-agent-actions">
                              {hs && (!hs.installed || !hs.current) && (
                                <button className="btn btn-sm" disabled={busy !== null} onClick={() => { setBusy('helper'); void api.helper.install(selected.id).then((st) => setHelper((prev) => ({ ...prev, [selected.id]: st }))).catch((err) => setError(String(err))).finally(() => setBusy(null)) }}>
                                  {busy === 'helper' ? 'Installing…' : hs.installed ? 'Update' : 'Install'}
                                </button>
                              )}
                            </span>
                          </div>
                          {hs?.installed && activeProject(selected) && (
                            <div className={`srv-check srv-agent${w?.wired ? ' is-ok' : ''}`}>
                              <span className="srv-check-icon">{w?.wired ? '✅' : '⬜'}</span>
                              <span className="srv-check-label">{activeProject(selected)?.name}</span>
                              <span className="srv-check-value">{w?.wired ? `wired · ${w.openIssues} open issue${w.openIssues === 1 ? '' : 's'}` : 'not wired: no hooks or tools in this project yet'}</span>
                              <span className="srv-agent-actions">
                                {!w?.wired ? (
                                  <button className="btn btn-sm" disabled={busy !== null} onClick={() => { setBusy('wire'); void api.helper.wire(selected.id, (activeProject(selected) as ServerProject).id).then((st) => setHelper((prev) => ({ ...prev, [selected.id]: st }))).catch((err) => setError(String(err))).finally(() => setBusy(null)) }}>
                                    {busy === 'wire' ? 'Wiring…' : 'Wire project'}
                                  </button>
                                ) : (
                                  <>
                                    <button className="btn btn-sm" onClick={() => setTab('autopilot')}>Autopilot</button>
                                    <button className="btn btn-sm btn-ghost" disabled={busy !== null} title="Remove the hooks and tools from this project" onClick={() => { setBusy('unwire'); void api.helper.unwire(selected.id, (activeProject(selected) as ServerProject).id).then((st) => setHelper((prev) => ({ ...prev, [selected.id]: st }))).catch((err) => setError(String(err))).finally(() => setBusy(null)) }}>
                                      Unwire
                                    </button>
                                  </>
                                )}
                              </span>
                            </div>
                          )}
                        </>
                      )
                    })()}
                  </div>
                  <h4 className="srv-h4">Coding agents</h4>
                  <div className="srv-checks">
                    {(h.agents ?? []).map((a) => (
                      <div key={a.id} className={`srv-check srv-agent${a.installed && a.loggedIn ? ' is-ok' : a.installed ? ' is-warn' : ''}`}>
                        <span className="srv-check-icon">{a.installed && a.loggedIn ? '✅' : a.installed ? '🟡' : '⬜'}</span>
                        <span className="srv-check-label">{a.label}</span>
                        <span className="srv-check-value" title={a.installed && !a.loggedIn ? `Open the terminal and ${a.loginHint}` : a.path || undefined}>
                          {a.installed ? `${a.version || 'installed'} · ${a.loggedIn ? (a.account ? `logged in as ${a.account}` : 'logged in') : 'not logged in'}` : 'not installed'}
                        </span>
                        <span className="srv-agent-actions">
                          {!a.installed && (
                            <button className="btn btn-sm" onClick={() => void install(selected.id, a.id)} disabled={busy !== null}>
                              {busy === `install:${selected.id}:${a.id}` ? 'Installing…' : 'Install'}
                            </button>
                          )}
                          {a.installed && !a.loggedIn && (
                            <button className="btn btn-sm" onClick={() => setTab('terminal')} title={`Open the terminal and ${a.loginHint}`}>Log in</button>
                          )}
                          {a.installed && a.loggedIn && (activeProject(selected)?.agent ?? 'claude') === a.id && (
                            <button className="btn btn-sm" onClick={() => setTab('chat')}>Chat</button>
                          )}
                        </span>
                      </div>
                    ))}
                  </div>
                  <div className="btn-row">
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
