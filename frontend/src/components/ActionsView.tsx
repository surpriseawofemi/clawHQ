import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { ServerAction } from '../types'

const TEMPLATES: Omit<ServerAction, 'id'>[] = [
  { name: 'Git status', command: 'git status --short --branch', confirm: false },
  { name: 'Git pull', command: 'git pull --ff-only', confirm: true },
  { name: 'Disk usage', command: 'df -h / && du -sh . 2>/dev/null', confirm: false },
  { name: 'Recent syslog', command: 'sudo journalctl -n 100 --no-pager 2>/dev/null || tail -n 100 /var/log/syslog', confirm: false },
  { name: 'Running services', command: 'systemctl list-units --type=service --state=running --no-pager | head -40', confirm: false },
  { name: 'Docker containers', command: 'docker ps --format "table {{.Names}}\\t{{.Status}}\\t{{.Ports}}"', confirm: false },
  { name: 'PM2 list', command: 'pm2 list', confirm: false },
  { name: 'Restart nginx', command: 'sudo systemctl restart nginx && sudo systemctl status nginx --no-pager | head -5', confirm: true }
]

type Run = { runId: string; name: string; command: string; out: string; code: number | null; error?: string; startedAt: number }

const dec = (b64: string): string => {
  try {
    const bin = atob(b64)
    const bytes = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
    return new TextDecoder().decode(bytes)
  } catch {
    return ''
  }
}
// eslint-disable-next-line no-control-regex
const stripAnsi = (s: string): string => s.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '').replace(/\r(?!\n)/g, '\n')

/**
 * Quick actions: saved commands per server, run from a button with the output
 * streamed into the page. Templates cover the usual pull, restart, tail-a-log.
 * Commands run through the login shell in the project folder.
 */
export function ActionsView({
  serverId,
  projectId = '',
  projectName = '',
  dir = '',
  actions,
  suggested = [],
  onActions
}: {
  serverId: string
  projectId?: string
  projectName?: string
  dir?: string
  actions: ServerAction[]
  suggested?: ServerAction[]
  onActions: (a: ServerAction[]) => void
}): React.JSX.Element {
  const [editing, setEditing] = useState<ServerAction | null>(null)
  const [adhoc, setAdhoc] = useState('')
  const [runs, setRuns] = useState<Run[]>([])
  const [confirmFor, setConfirmFor] = useState<ServerAction | null>(null)
  const [error, setError] = useState<string | null>(null)
  const outRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    const offOut = api.onActionOut((e) => {
      if (e.serverId !== serverId) return
      setRuns((prev) => prev.map((r) => (r.runId === e.runId ? { ...r, out: r.out + dec(e.data) } : r)))
    })
    const offExit = api.onActionExit((e) => {
      if (e.serverId !== serverId) return
      setRuns((prev) => prev.map((r) => (r.runId === e.runId ? { ...r, code: e.code, error: e.error || undefined } : r)))
    })
    return () => {
      offOut()
      offExit()
    }
  }, [serverId])

  useEffect(() => {
    const el = outRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [runs[0]?.out])

  const run = async (name: string, command: string): Promise<void> => {
    setError(null)
    try {
      const runId = await api.servers.runIn(serverId, command, dir)
      setRuns((prev) => [{ runId, name, command, out: '', code: null, startedAt: Date.now() }, ...prev].slice(0, 8))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const start = (a: ServerAction): void => {
    if (a.confirm) setConfirmFor(a)
    else void run(a.name, a.command)
  }

  const save = async (): Promise<void> => {
    if (!editing) return
    try {
      onActions(await api.servers.saveAction(serverId, editing))
      setEditing(null)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const current = runs[0] ?? null
  const mine = actions.filter((a) => a.projectId === projectId && projectId)
  const shared = actions.filter((a) => !a.projectId)
  const card = (a: ServerAction): React.JSX.Element => (
    <div key={a.id} className="act-card">
      <button className="act-run" onClick={() => start(a)} title={a.command}>
        <span className="act-name">{a.confirm ? '⚠️ ' : '▶ '}{a.name}</span>
        <span className="act-cmd">{a.command}</span>
      </button>
      <span className="act-tools">
        <button className="icon-btn" title="Edit" onClick={() => setEditing({ ...a })}>✎</button>
        <button className="icon-btn" title="Remove" onClick={() => void api.servers.removeAction(serverId, a.id).then(onActions)}>🗑</button>
      </span>
    </div>
  )
  const notYetAdded = suggested.filter((sg) => !actions.some((a) => a.command === sg.command))

  return (
    <div className="acts">
      {projectId && (
        <>
          <div className="acts-label">{projectName || 'This project'}</div>
          <div className="acts-grid">
            {mine.map(card)}
            <button className="act-card act-add" onClick={() => setEditing({ id: '', name: '', command: '', confirm: false, projectId })}>
              ＋ Action for {projectName || 'this project'}
            </button>
          </div>
          {notYetAdded.length > 0 && (
            <div className="acts-templates">
              <span className="plugin-desc">Found in the folder:</span>
              {notYetAdded.map((sg) => (
                <button key={sg.command} className="btn btn-sm" title={sg.command} onClick={() => void api.servers.saveAction(serverId, { ...sg, id: '', projectId }).then(onActions)}>
                  ＋ {sg.name}
                </button>
              ))}
            </div>
          )}
          <div className="acts-label">Whole server</div>
        </>
      )}
      <div className="acts-grid">
        {shared.map(card)}
        <button className="act-card act-add" onClick={() => setEditing({ id: '', name: '', command: '', confirm: false })}>
          ＋ Server-wide action
        </button>
      </div>
      {shared.length === 0 && (
        <div className="acts-templates">
          <span className="plugin-desc">Start from a template:</span>
          {TEMPLATES.map((t) => (
            <button key={t.name} className="btn btn-sm" onClick={() => void api.servers.saveAction(serverId, { id: '', ...t }).then(onActions)}>
              {t.name}
            </button>
          ))}
        </div>
      )}
      {editing && (
        <form
          className="act-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <div className="issue-form-row">
            <div className="field">
              <span>Name</span>
              <input value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} placeholder="Restart the app" autoFocus />
            </div>
            <label className="check-row act-confirm">
              <input type="checkbox" checked={editing.confirm} onChange={(e) => setEditing({ ...editing, confirm: e.target.checked })} />
              <span>Ask before running</span>
            </label>
            {projectId && (
              <label className="check-row act-confirm">
                <input type="checkbox" checked={editing.projectId === projectId} onChange={(e) => setEditing({ ...editing, projectId: e.target.checked ? projectId : '' })} />
                <span>Only for {projectName || 'this project'}</span>
              </label>
            )}
          </div>
          <div className="field">
            <span>Command (runs in {dir || 'the home folder'} through the login shell)</span>
            <textarea rows={3} value={editing.command} onChange={(e) => setEditing({ ...editing, command: e.target.value })} placeholder="git pull --ff-only && pm2 restart app" spellCheck={false} />
          </div>
          <div className="btn-row">
            <button type="submit" className="btn btn-primary" disabled={!editing.name.trim() || !editing.command.trim()}>Save</button>
            <button type="button" className="btn" onClick={() => setEditing(null)}>Cancel</button>
          </div>
        </form>
      )}
      <form
        className="act-adhoc"
        onSubmit={(e) => {
          e.preventDefault()
          if (adhoc.trim()) {
            void run(adhoc.trim().slice(0, 40), adhoc.trim())
            setAdhoc('')
          }
        }}
      >
        <input value={adhoc} onChange={(e) => setAdhoc(e.target.value)} placeholder="Run a one-off command…" spellCheck={false} />
        <button type="submit" className="btn btn-sm" disabled={!adhoc.trim()}>Run</button>
      </form>
      {error && <p className="error-text">{error}</p>}
      {current && (
        <div className="act-out">
          <div className="act-out-bar">
            <span className="act-out-name">{current.name}</span>
            <code className="act-out-cmd">{current.command}</code>
            {current.code === null ? (
              <>
                <span className="plugin-desc">running…</span>
                <button className="btn btn-sm btn-stop" onClick={() => void api.servers.stop(current.runId)}>■ Stop</button>
              </>
            ) : (
              <span className={`act-exit${current.code === 0 ? ' is-ok' : ' is-bad'}`}>{current.code === 0 ? '✅ exit 0' : `❌ exit ${current.code}${current.error ? ` · ${current.error}` : ''}`}</span>
            )}
            {runs.length > 1 && (
              <select value={current.runId} onChange={(e) => setRuns((prev) => { const r = prev.find((x) => x.runId === e.target.value); return r ? [r, ...prev.filter((x) => x.runId !== r.runId)] : prev })} aria-label="Earlier runs">
                {runs.map((r) => (
                  <option key={r.runId} value={r.runId}>{r.name} · {new Date(r.startedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</option>
                ))}
              </select>
            )}
          </div>
          <pre ref={outRef} className="act-out-body">{stripAnsi(current.out) || (current.code === null ? '' : '(no output)')}</pre>
        </div>
      )}
      {confirmFor && (
        <div className="lightbox files-ask" role="dialog" onClick={() => setConfirmFor(null)}>
          <div className="files-ask-card" onClick={(e) => e.stopPropagation()}>
            <h3>Run "{confirmFor.name}"?</h3>
            <code className="act-out-cmd">{confirmFor.command}</code>
            <div className="btn-row">
              <button className="btn btn-primary" autoFocus onClick={() => { const a = confirmFor; setConfirmFor(null); void run(a.name, a.command) }}>Run</button>
              <button className="btn" onClick={() => setConfirmFor(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
