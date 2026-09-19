import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { useLiveRefresh } from '../state/useLiveRefresh'
import { renderMarkdown } from '../markdown'
import { plugin } from '../state/plugin'
import { bossSessionKey } from '../state/useFleet'
import { ContentHead, Shell, SideHead, type ShellProps } from './layout/Shell'
import type { Agent, Issue, IssueStatus, IssueUrgency, ServerIssue, ServerProfile } from '../types'
import { serverNav } from '../state/serverNav'
import { getPrefs, setPref } from '../prefs'
import { Toggle } from './Toggle'
import { agentEmoji, agentLabel } from '../types'

type Props = {
  shell: ShellProps
  agents: Agent[]
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
  onOpenServers?: () => void
}

const STATUS_ICON: Record<IssueStatus, string> = { open: '🔴', 'in-progress': '🟡', resolved: '✅' }
const STATUS_LABEL: Record<IssueStatus, string> = { open: 'Needs you', 'in-progress': 'In progress', resolved: 'Resolved' }
const URGENCY_RANK: Record<IssueUrgency, number> = { urgent: 0, high: 1, normal: 2, low: 3 }
const STATUS_RANK: Record<IssueStatus, number> = { open: 0, 'in-progress': 1, resolved: 2 }
const KIND_LABEL: Record<string, string> = { question: 'Question', task: 'Task', issue: 'Issue', improvement: 'Improvement' }

const when = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date().toDateString() === d.toDateString()
  return today ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/**
 * Issues: everything agents need from you, one item at a time, plus tasks you hand
 * out. The list on the left is ordered by what needs you first, then urgency, then
 * age; the item on the right shows the ask, the thread, and a box to answer. An
 * answer goes into the plugin and into the agent's Super Boss Chat as a turn, so
 * the agent acts on it and closes the item with a note.
 */
/** What the page showed last time, so coming back is instant and nothing blinks. */
const issuesMemory: { issues: Issue[]; selectedId: string | null; showResolved: boolean; serverIssues: { server: ServerProfile; projectId: string; projectName: string; issues: ServerIssue[] }[] } = { issues: [], selectedId: null, showResolved: false, serverIssues: [] }

export function IssuesPage({ shell, agents, connected, onOpenAgent, onOpenServers }: Props): React.JSX.Element {
  const [issues, setIssues] = useState<Issue[]>(issuesMemory.issues)
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(issuesMemory.selectedId)
  const [showResolved, setShowResolvedState] = useState(getPrefs().issuesShowResolved)
  const [sort, setSortState] = useState(getPrefs().issuesSort)
  const setShowResolved = (v: boolean): void => { setShowResolvedState(v); setPref('issuesShowResolved', v) }
  const setSort = (v: typeof sort): void => { setSortState(v); setPref('issuesSort', v) }
  const [answer, setAnswer] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [composing, setComposing] = useState(false)
  const [nt, setNt] = useState({ title: '', body: '', agentId: '', urgency: 'normal' })
  const refresh = useCallback(async () => {
    try {
      const status = await api.plugin.status()
      setPluginPresent(status.present)
      if (connected && status.present) setIssues(await plugin.issues.list())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  const live = useLiveRefresh(refresh, 6000)
  useEffect(() => {
    void refresh()
    const off = api.onGatewayEvent(({ event }) => {
      if (event === 'clawhq.issues.changed') void refresh()
    })
    return off
  }, [refresh])

  // Servers: an issue can be handed to the coding agent on a machine; its answer
  // comes back as a reply on the issue.
  const [servers, setServers] = useState<ServerProfile[]>([])
  const [serverRuns, setServerRuns] = useState<Record<string, { issueId: string; serverName: string }>>({})
  // Numbered issues the coding agents on servers opened for you (via the helper).
  const [serverIssues, setServerIssues] = useState<{ server: ServerProfile; projectId: string; projectName: string; issues: ServerIssue[] }[]>(issuesMemory.serverIssues)
  useEffect(() => {
    Object.assign(issuesMemory, { issues, selectedId, showResolved, serverIssues })
  }, [issues, selectedId, showResolved, serverIssues])
  useEffect(() => {
    let alive = true
    const loadServers = async (): Promise<void> => {
      try {
        const list = await api.servers.list()
        if (!alive) return
        setServers(list)
        const groups = await Promise.all(
          list.flatMap((sv) => (sv.projects ?? []).map(async (p) => ({ server: sv, projectId: p.id, projectName: p.name, issues: await api.helper.issues(sv.id, p.id).catch(() => [] as ServerIssue[]) })))
        )
        if (alive) setServerIssues(groups.filter((g) => g.issues.some((i) => i.status === 'open')))
      } catch {
        /* no servers */
      }
    }
    void loadServers()
    const t = setInterval(() => void loadServers(), 60_000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])
  useEffect(
    () =>
      api.onClaudeEvent((e) => {
        const link = serverRuns[e.runId]
        if (!link) return
        if (e.type === 'result' || e.type === 'error') {
          const text = (e.text || '').trim() || (e.type === 'error' ? 'The run failed without output.' : 'Done, no summary given.')
          void plugin.issues
            .reply(link.issueId, e.type === 'error' ? `❌ ${text}` : text, { by: link.serverName, byKind: 'agent' })
            .then(refresh)
            .catch((err) => setError(err instanceof Error ? err.message : String(err)))
          setServerRuns((prev) => {
            const n = { ...prev }
            delete n[e.runId]
            return n
          })
        }
      }),
    [serverRuns, refresh]
  )
  const sendToServer = async (issue: Issue, serverId: string): Promise<void> => {
    const sv = servers.find((x) => x.id === serverId)
    if (!sv) return
    setBusy('server')
    setError(null)
    try {
      const prompt = [
        `Issue ${issue.id} from ClawHQ, handed to you by the boss: ${issue.title}`,
        issue.body ? `\n${issue.body}` : '',
        issue.replies.length ? `\nThread so far:\n${issue.replies.map((r) => `- ${r.by}: ${r.text}`).join('\n')}` : '',
        '',
        'Do what it asks in this project, then reply with a short report of what changed and anything still needing a decision.'
      ].join('\n')
      const runId = await api.claude.send(sv.id, prompt)
      setServerRuns((prev) => ({ ...prev, [runId]: { issueId: issue.id, serverName: sv.name } }))
      await plugin.issues.update(issue.id, { status: 'in-progress' })
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const ordered = useMemo(
    () =>
      [...issues]
        .filter((i) => showResolved || i.status !== 'resolved')
        .sort((a, b) => {
          if (sort === 'newest') return b.createdAtMs - a.createdAtMs
          if (sort === 'oldest') return a.createdAtMs - b.createdAtMs
          if (sort === 'urgency') return URGENCY_RANK[a.urgency] - URGENCY_RANK[b.urgency] || b.updatedAtMs - a.updatedAtMs
          return STATUS_RANK[a.status] - STATUS_RANK[b.status] || URGENCY_RANK[a.urgency] - URGENCY_RANK[b.urgency] || b.updatedAtMs - a.updatedAtMs
        }),
    [issues, showResolved, sort]
  )
  const selected = issues.find((i) => i.id === selectedId) ?? null
  useEffect(() => {
    if (!selected && ordered.length > 0) setSelectedId(ordered[0].id)
  }, [selected, ordered])

  const agentOf = (id?: string): Agent | undefined => (id ? agents.find((a) => a.id === id) : undefined)
  const counterpart = (i: Issue): Agent | undefined => agentOf(i.assigneeAgentId) ?? (i.fromKind === 'agent' ? agentOf(i.from) : undefined)

  const tell = async (agentId: string, text: string): Promise<void> => {
    const key = bossSessionKey(agentId)
    try {
      await api.rpc.request('sessions.create', { key, agentId, label: 'Super Boss Chat' })
    } catch {
      /* exists */
    }
    await api.rpc.sendChat(key, text)
  }

  const sendAnswer = async (): Promise<void> => {
    if (!selected || !answer.trim()) return
    setBusy('answer')
    try {
      const text = answer.trim()
      await plugin.issues.reply(selected.id, text)
      const to = counterpart(selected)
      if (to) {
        await tell(
          to.id,
          `Answer from the boss on issue ${selected.id} "${selected.title}":\n\n${text}\n\nAct on it. Call clawhq_issue_update with status in-progress while you do and resolved with a short note when it is settled; file a new issue with clawhq_issue_create if something else needs the boss.`
        )
      }
      setAnswer('')
      setError(null)
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const setStatus = async (i: Issue, status: IssueStatus): Promise<void> => {
    setBusy(`status:${i.id}`)
    try {
      await plugin.issues.update(i.id, { status })
      setError(null)
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const createTask = async (): Promise<void> => {
    if (!nt.title.trim()) return
    setBusy('new')
    try {
      const it = await plugin.issues.create({ kind: 'task', title: nt.title.trim(), body: nt.body.trim() || undefined, assigneeAgentId: nt.agentId || undefined, urgency: nt.urgency })
      if (nt.agentId) {
        await tell(
          nt.agentId,
          `Task from the boss (issue ${it.id}): ${it.title}${it.body ? `\n\n${it.body}` : ''}\n\nCall clawhq_issue_update with status in-progress when you start and resolved with a short note when done. If anything is unclear, ask with clawhq_issue_create instead of guessing.`
        )
      }
      setNt({ title: '', body: '', agentId: '', urgency: 'normal' })
      setComposing(false)
      setSelectedId(it.id)
      setError(null)
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const remove = async (i: Issue): Promise<void> => {
    setBusy(`rm:${i.id}`)
    try {
      await plugin.issues.remove(i.id)
      if (selectedId === i.id) setSelectedId(null)
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const open = issues.filter((i) => i.status === 'open').length

  const side = (
    <>
      <SideHead>
        <span className="side-title">Issues</span>
        <span className="plugin-desc">{open > 0 ? `${open} need you` : 'nothing waiting'}</span>
      </SideHead>
      <div className="issue-side">
        <div className="issue-side-tools">
          <button className="btn btn-sm btn-primary" onClick={() => setComposing(true)} disabled={!connected || pluginPresent === false}>
            + Task for an agent
          </button>
          <span className="row-tools">
            <select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label="Sort issues" className="issue-sort">
              <option value="needs">Needs you first</option>
              <option value="urgency">Urgency</option>
              <option value="newest">Newest</option>
              <option value="oldest">Oldest</option>
            </select>
            <Toggle on={showResolved} onChange={setShowResolved} label="Resolved" />
          </span>
        </div>
        {serverIssues.map((g) => (
          <div key={`${g.server.id}:${g.projectId}`} className="issue-server-group">
            <div className="team-side-label">🖥 {g.server.name} / {g.projectName}</div>
            {g.issues.filter((i) => i.status === 'open').slice(0, 30).map((i) => (
              <button
                key={i.n}
                className={`issue-row${i.needsBoss ? ' is-open' : ''}`}
                title="Open in the server's Autopilot tab"
                onClick={() => {
                  serverNav.set({ serverId: g.server.id, projectId: g.projectId, tab: 'autopilot' })
                  onOpenServers?.()
                }}
              >
                <span className="issue-status">{i.needsBoss ? '🔴' : '🟡'}</span>
                <span className="issue-row-meta">
                  <span className="issue-row-title">#{i.n} {i.title}</span>
                  <span className="issue-row-sub">{i.needsBoss ? 'needs you' : 'note'}{i.urgency !== 'normal' ? <span className={`urgency is-${i.urgency}`}> · {i.urgency}</span> : null}</span>
                </span>
                <span className="issue-row-time">{when(i.updatedAt)}</span>
              </button>
            ))}
          </div>
        ))}
        {ordered.length === 0 && serverIssues.length === 0 && <p className="field-hint">{pluginPresent === false ? 'Needs the ClawHQ gateway plugin.' : 'No issues.'}</p>}
        {ordered.map((i) => {
          const a = counterpart(i)
          return (
            <button key={i.id} className={`issue-row${selectedId === i.id && !composing ? ' is-selected' : ''}${i.status === 'open' ? ' is-open' : ''}`} onClick={() => { setSelectedId(i.id); setComposing(false) }}>
              <span className="issue-status" title={STATUS_LABEL[i.status]}>{STATUS_ICON[i.status]}</span>
              <span className="issue-row-meta">
                <span className="issue-row-title">{i.title}</span>
                <span className="issue-row-sub">
                  {a ? `${agentEmoji(a)} ${agentLabel(a)}` : i.fromKind === 'human' ? '👤 You' : i.from} · {KIND_LABEL[i.kind] ?? i.kind}
                  {i.urgency !== 'normal' && <span className={`urgency is-${i.urgency}`}> · {i.urgency}</span>}
                </span>
              </span>
              <span className="issue-row-time">{when(i.updatedAtMs)}</span>
            </button>
          )
        })}
      </div>
    </>
  )

  return (
    <Shell shell={shell} title="Issues" side={side}>
      {composing ? (
        <>
          <ContentHead onRefresh={live.now} refreshing={live.busy} title="New task for an agent" subtitle="It lands in the agent's Super Boss Chat and on this list; the agent closes it with a note." />
          <div className="content-body issue-detail">
            <div className="field">
              <span>Title</span>
              <input value={nt.title} onChange={(e) => setNt({ ...nt, title: e.target.value })} placeholder="What needs doing" autoFocus />
            </div>
            <div className="field">
              <span>Details</span>
              <textarea rows={5} value={nt.body} onChange={(e) => setNt({ ...nt, body: e.target.value })} placeholder="Context, what done looks like" />
            </div>
            <div className="issue-form-row">
              <div className="field">
                <span>Agent</span>
                <select value={nt.agentId} onChange={(e) => setNt({ ...nt, agentId: e.target.value })}>
                  <option value="">Nobody yet</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {agentEmoji(a)} {agentLabel(a)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <span>Urgency</span>
                <select value={nt.urgency} onChange={(e) => setNt({ ...nt, urgency: e.target.value })}>
                  <option value="low">Low</option>
                  <option value="normal">Normal</option>
                  <option value="high">High</option>
                  <option value="urgent">Urgent</option>
                </select>
              </div>
            </div>
            <div className="btn-row">
              <button className="btn btn-primary" onClick={() => void createTask()} disabled={!nt.title.trim() || busy === 'new'}>
                {busy === 'new' ? 'Creating…' : nt.agentId ? 'Create and send' : 'Create'}
              </button>
              <button className="btn" onClick={() => setComposing(false)}>
                Cancel
              </button>
            </div>
            {error && <p className="error-text">{error}</p>}
          </div>
        </>
      ) : selected ? (
        (() => {
          const a = counterpart(selected)
          const from = selected.fromKind === 'agent' ? agentOf(selected.from) : undefined
          return (
            <>
              <ContentHead onRefresh={live.now} refreshing={live.busy}
                title={
                  <span className="issue-title">
                    <span title={STATUS_LABEL[selected.status]}>{STATUS_ICON[selected.status]}</span> {selected.title}
                  </span>
                }
                subtitle={`${KIND_LABEL[selected.kind] ?? selected.kind} · ${STATUS_LABEL[selected.status]} · ${selected.urgency} · ${from ? `from ${agentLabel(from)}` : 'from you'}${selected.assigneeAgentId && agentOf(selected.assigneeAgentId) ? ` · for ${agentLabel(agentOf(selected.assigneeAgentId) as Agent)}` : ''} · ${when(selected.createdAtMs)}`}
              >
                <select value={selected.urgency} onChange={(e) => void plugin.issues.update(selected.id, { urgency: e.target.value }).then(refresh)} aria-label="Urgency">
                  <option value="low">Low</option>
                  <option value="normal">Normal</option>
                  <option value="high">High</option>
                  <option value="urgent">Urgent</option>
                </select>
                {selected.status !== 'resolved' ? (
                  <button className="btn btn-sm" onClick={() => void setStatus(selected, 'resolved')} disabled={busy !== null}>
                    ✅ Mark resolved
                  </button>
                ) : (
                  <button className="btn btn-sm" onClick={() => void setStatus(selected, 'open')} disabled={busy !== null}>
                    Reopen
                  </button>
                )}
                {a && (
                  <button className="btn btn-sm btn-ghost" onClick={() => onOpenAgent(a.id, selected.sessionKey)}>
                    Open {agentLabel(a)}
                  </button>
                )}
                {servers.length > 0 && (
                  <select
                    value=""
                    onChange={(e) => {
                      if (e.target.value) void sendToServer(selected, e.target.value)
                    }}
                    disabled={busy !== null}
                    aria-label="Send to a server"
                    title="Hand this issue to the coding agent on a server; its report comes back as a reply"
                  >
                    <option value="">Send to server…</option>
                    {servers.map((sv) => (
                      <option key={sv.id} value={sv.id}>
                        🖥 {sv.name}
                      </option>
                    ))}
                  </select>
                )}
                <button className="btn btn-sm btn-ghost" onClick={() => void remove(selected)} disabled={busy !== null} title="Delete this issue">
                  Delete
                </button>
              </ContentHead>
              <div className="content-body issue-detail">
                <div className="issue-thread">
                  <div className="issue-msg">
                    <span className="team-avatar">{from ? agentEmoji(from) : '👤'}</span>
                    <div className="team-bubble">
                      <div className="team-meta">
                        <span className="team-from">{from ? agentLabel(from) : 'You'}</span>
                        <span className="team-time">{when(selected.createdAtMs)}</span>
                      </div>
                      <div className="team-md" dangerouslySetInnerHTML={{ __html: renderMarkdown(selected.body || selected.title) }} />
                    </div>
                  </div>
                  {selected.replies.map((r) => {
                    const ra = r.byKind === 'agent' ? agentOf(r.by) : undefined
                    return (
                      <div key={r.id} className={`issue-msg${r.byKind === 'human' ? ' is-mine' : ''}`}>
                        <span className="team-avatar">{r.byKind === 'human' ? '👤' : ra ? agentEmoji(ra) : '🤖'}</span>
                        <div className="team-bubble">
                          <div className="team-meta">
                            <span className="team-from">{r.byKind === 'human' ? 'You' : ra ? agentLabel(ra) : r.by}</span>
                            <span className="team-time">{when(r.atMs)}</span>
                          </div>
                          <div className="team-md" dangerouslySetInnerHTML={{ __html: renderMarkdown(r.text) }} />
                        </div>
                      </div>
                    )
                  })}
                  {selected.status === 'resolved' && (
                    <p className="issue-resolved">✅ Resolved {selected.resolvedAtMs ? when(selected.resolvedAtMs) : ''}</p>
                  )}
                </div>
                {error && <p className="error-text">{error}</p>}
                <div className="team-composer">
                  <textarea
                    rows={2}
                    value={answer}
                    placeholder={a ? `Answer ${agentLabel(a)}… (Enter sends, Shift+Enter for a new line)` : 'Add a note…'}
                    disabled={!connected}
                    onChange={(e) => setAnswer(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault()
                        void sendAnswer()
                      }
                    }}
                  />
                  <button className="btn btn-primary btn-send" onClick={() => void sendAnswer()} disabled={!connected || !answer.trim() || busy === 'answer'}>
                    {busy === 'answer' ? 'Sending…' : a ? 'Answer' : 'Note'}
                  </button>
                </div>
              </div>
            </>
          )
        })()
      ) : (
        <>
          <ContentHead onRefresh={live.now} refreshing={live.busy} title="Issues" subtitle="Questions, approvals and problems agents raise for you, and tasks you hand out, one at a time." />
          <div className="content-body">
            <p className="field-hint">{pluginPresent === false ? 'Issues need the ClawHQ gateway plugin (Settings → Plugins).' : 'Nothing waiting. Agents file items here with clawhq_issue_create; use "+ Task for an agent" to hand something out.'}</p>
          </div>
        </>
      )}
    </Shell>
  )
}
