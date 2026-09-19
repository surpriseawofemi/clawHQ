import { useCallback, useEffect, useMemo, useState } from 'react'
import { TrashIcon } from './icons'
import { api } from '../api'
import { useLiveRefresh } from '../state/useLiveRefresh'
import { plugin, taskPrompt } from '../state/plugin'
import { ContentHead, Shell, type ShellProps } from './layout/Shell'
import type { Agent, Task, TaskStatus } from '../types'
import { agentEmoji, agentLabel } from '../types'

type Props = {
  shell: ShellProps
  agents: Agent[]
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
}

const COLUMNS: { id: TaskStatus; label: string }[] = [
  { id: 'todo', label: 'To do' },
  { id: 'doing', label: 'Doing' },
  { id: 'done', label: 'Done' },
  { id: 'failed', label: 'Failed' }
]

const ago = (ms: number): string => {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86_400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86_400)}d ago`
}

/**
 * The task board, kept by the gateway plugin so every ClawHQ and every agent sees
 * the same cards. Run sends the task into a fresh thread of the assigned agent and
 * moves the card to Doing; the agent moves it to Done with its result (or the run's
 * end does, if the agent forgets).
 */
export function TasksBoard({ shell, agents, connected, onOpenAgent }: Props): React.JSX.Element {
  const [tasks, setTasks] = useState<Task[]>([])
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [title, setTitle] = useState('')
  const [details, setDetails] = useState('')
  const [agentId, setAgentId] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showDone, setShowDone] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const status = await api.plugin.status()
      setPluginPresent(status.present)
      setTasks(connected && status.present ? await plugin.tasks.list() : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  const live = useLiveRefresh(refresh, 8000)
  useEffect(() => {
    void refresh()
    const off = api.onGatewayEvent(({ event }) => {
      if (event === 'clawhq.tasks.changed') void refresh()
    })
    const tick = setInterval(() => void refresh(), 30_000)
    return () => {
      off()
      clearInterval(tick)
    }
  }, [refresh])

  const byId = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents])

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

  const create = (): Promise<void> =>
    act('create', async () => {
      if (!title.trim()) return
      await plugin.tasks.create({ title, details, agentId: agentId || undefined })
      setTitle('')
      setDetails('')
    })

  /** A fresh thread for the run, so the card's result is easy to find later. */
  const run = (task: Task): Promise<void> =>
    act(`run-${task.id}`, async () => {
      if (!task.agentId) throw new Error('assign an agent first')
      const created = await api.rpc.request<{ key?: string }>('sessions.create', {
        agentId: task.agentId,
        label: `Task: ${task.title}`.slice(0, 80)
      })
      const key = created?.key ?? `agent:${task.agentId}:main`
      const reply = await api.rpc.sendChat(key, taskPrompt(task))
      await plugin.tasks.update(task.id, { status: 'doing', sessionKey: key, runId: reply.runId })
    })

  const columns = COLUMNS.map((c) => ({
    ...c,
    tasks: tasks
      .filter((t) => t.status === c.id)
      .filter((t) => c.id !== 'done' || showDone || Date.now() - t.updatedAtMs < 86_400_000)
      .sort((a, b) => b.updatedAtMs - a.updatedAtMs)
  }))

  return (
    <Shell shell={shell} title="Tasks">
      <ContentHead onRefresh={live.now} refreshing={live.busy} title="Tasks" subtitle={pluginPresent === false ? 'The task board needs the ClawHQ plugin on the gateway (Settings → Plugins).' : `${tasks.filter((t) => t.status !== 'done').length} open`}>
        <label className="check-row">
          <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />
          <span>Show all done</span>
        </label>
      </ContentHead>

      <div className="content-body">
        <form
          className="task-new"
          onSubmit={(e) => {
            e.preventDefault()
            void create()
          }}
        >
          <input placeholder="New task" value={title} onChange={(e) => setTitle(e.target.value)} disabled={!pluginPresent} />
          <input placeholder="Details (optional)" value={details} onChange={(e) => setDetails(e.target.value)} disabled={!pluginPresent} />
          <select value={agentId} onChange={(e) => setAgentId(e.target.value)} disabled={!pluginPresent} aria-label="Assign to">
            <option value="">Unassigned</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {agentEmoji(a)} {agentLabel(a)}
              </option>
            ))}
          </select>
          <button className="btn btn-primary" type="submit" disabled={!pluginPresent || !title.trim() || busy !== null}>
            Add
          </button>
        </form>

        <div className="kanban">
          {columns.map((col) => (
            <section key={col.id} className={`kanban-col is-${col.id}`}>
              <h2>
                {col.label} <span className="plugin-desc">{col.tasks.length}</span>
              </h2>
              {col.tasks.map((t) => {
                const a = t.agentId ? byId.get(t.agentId) : undefined
                return (
                  <article key={t.id} className="task-card">
                    <div className="task-title">{t.title}</div>
                    {t.details && <div className="task-details">{t.details}</div>}
                    <div className="task-meta">
                      <select
                        value={t.agentId ?? ''}
                        onChange={(e) => void act(`assign-${t.id}`, () => plugin.tasks.update(t.id, { agentId: e.target.value }))}
                        aria-label="Assignee"
                        disabled={busy !== null}
                      >
                        <option value="">Unassigned</option>
                        {agents.map((x) => (
                          <option key={x.id} value={x.id}>
                            {agentEmoji(x)} {agentLabel(x)}
                          </option>
                        ))}
                      </select>
                      <span className="plugin-desc">
                        {a ? '' : ''}
                        {t.createdBy !== 'human' ? `by ${t.createdBy} · ` : ''}
                        {ago(t.updatedAtMs)}
                      </span>
                    </div>
                    {(t.result || t.error) && <pre className={`task-result${t.error ? ' is-bad' : ''}`}>{t.error || t.result}</pre>}
                    <div className="btn-row">
                      {t.status === 'todo' && (
                        <button className="btn btn-sm btn-primary" disabled={busy !== null || !t.agentId} onClick={() => void run(t)} title={t.agentId ? 'Send it to the agent' : 'Assign an agent first'}>
                          {busy === `run-${t.id}` ? 'Starting…' : 'Run'}
                        </button>
                      )}
                      {t.sessionKey && t.agentId && (
                        <button className="btn btn-sm" onClick={() => onOpenAgent(t.agentId!, t.sessionKey)}>
                          Thread
                        </button>
                      )}
                      {t.status !== 'done' && (
                        <button className="btn btn-sm btn-ghost" disabled={busy !== null} onClick={() => void act(`done-${t.id}`, () => plugin.tasks.update(t.id, { status: 'done' }))}>
                          Done
                        </button>
                      )}
                      {t.status !== 'todo' && (
                        <button className="btn btn-sm btn-ghost" disabled={busy !== null} onClick={() => void act(`todo-${t.id}`, () => plugin.tasks.update(t.id, { status: 'todo' }))}>
                          Reopen
                        </button>
                      )}
                      <button className="btn btn-sm btn-ghost" disabled={busy !== null} onClick={() => void act(`del-${t.id}`, () => plugin.tasks.remove(t.id))} title="Delete"><TrashIcon /></button>
                    </div>
                  </article>
                )
              })}
              {col.tasks.length === 0 && <p className="field-hint">Nothing here.</p>}
            </section>
          ))}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </Shell>
  )
}
