import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { renderMarkdown } from '../markdown'
import { AgentHeader, type AgentTab } from './AgentHeader'
import type { Agent } from '../types'

type Entry = {
  path: string
  name: string
  kind: 'file' | 'directory' | string
  size?: number
  updatedAtMs?: number
}

type FileBody = {
  path: string
  name: string
  content: string
  mimeType?: string
  updatedAtMs?: number
}

/** The charter and housekeeping files that are not work product. */
const NOT_DOCUMENTS = new Set(['AGENTS.md', 'SOUL.md', 'IDENTITY.md', 'USER.md', 'DREAMS.md', 'BOOTSTRAP.md'])

const when = (ms?: number): string => {
  if (!ms) return ''
  const diff = Date.now() - ms
  if (diff < 3_600_000) return `${Math.max(1, Math.round(diff / 60_000))}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return new Date(ms).toLocaleDateString([], { month: 'short', day: 'numeric' })
}

type Props = {
  agent: Agent
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  connected: boolean
}

/**
 * An agent's deliverables: the markdown in its workspace, read through the gateway.
 *
 * Every agent writes its work product as `*.md` at the workspace root, so that is
 * the default listing; folders such as `memory/` can be opened too. Rendering is
 * read-only here: the gateway only accepts writes to the charter files.
 */
export function AgentDocuments({ agent, tab, onTab, connected }: Props): React.JSX.Element {
  const [dir, setDir] = useState('')
  const [entries, setEntries] = useState<Entry[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [file, setFile] = useState<FileBody | null>(null)
  const [showSource, setShowSource] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const listDir = useCallback(
    async (path: string) => {
      if (!connected) return
      setLoading(true)
      try {
        const res = await api.rpc.request<{ entries?: Entry[] }>('agents.workspace.list', {
          agentId: agent.id,
          ...(path ? { path } : {})
        })
        setEntries(res?.entries ?? [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setLoading(false)
      }
    },
    [agent.id, connected]
  )

  useEffect(() => {
    setDir('')
    setSelected(null)
    setFile(null)
  }, [agent.id])

  useEffect(() => {
    void listDir(dir)
  }, [dir, listDir])

  const visible = useMemo(() => {
    const dirs = entries.filter((e) => e.kind === 'directory' && !e.name.startsWith('.'))
    const docs = entries.filter(
      (e) => e.kind === 'file' && /\.(md|markdown|txt)$/i.test(e.name) && !(dir === '' && NOT_DOCUMENTS.has(e.name))
    )
    docs.sort((a, b) => (b.updatedAtMs ?? 0) - (a.updatedAtMs ?? 0))
    return { dirs, docs }
  }, [entries, dir])

  // Open the most recent document by default so the tab is never a blank pane.
  useEffect(() => {
    if (!selected && visible.docs.length > 0) setSelected(visible.docs[0].path)
  }, [visible.docs, selected])

  useEffect(() => {
    if (!selected || !connected) return
    let cancelled = false
    void (async () => {
      try {
        const res = await api.rpc.request<{ file?: FileBody }>('agents.workspace.get', {
          agentId: agent.id,
          path: selected
        })
        if (!cancelled) setFile(res?.file ?? null)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [selected, agent.id, connected])

  const crumbs = dir ? dir.split(/[\\/]/).filter(Boolean) : []

  return (
    <main className="chat">
      <AgentHeader agent={agent} tab={tab} onTab={onTab} />
      <div className="docs">
        <aside className="docs-list">
          {crumbs.length > 0 && (
            <div className="docs-crumbs">
              <button onClick={() => setDir('')}>workspace</button>
              {crumbs.map((c, i) => (
                <span key={i}>
                  {' / '}
                  <button onClick={() => setDir(crumbs.slice(0, i + 1).join('/'))}>{c}</button>
                </span>
              ))}
            </div>
          )}
          {visible.dirs.map((d) => (
            <button key={d.path} className="docs-item is-dir" onClick={() => setDir(d.path)}>
              📁 {d.name}
            </button>
          ))}
          {visible.docs.map((d) => (
            <button
              key={d.path}
              className={`docs-item${selected === d.path ? ' is-active' : ''}`}
              onClick={() => setSelected(d.path)}
            >
              <span>{d.name.replace(/\.(md|markdown|txt)$/i, '')}</span>
              <small>
                {when(d.updatedAtMs)}
                {d.size ? ` · ${Math.max(1, Math.round(d.size / 1024))} KB` : ''}
              </small>
            </button>
          ))}
          {!loading && visible.docs.length === 0 && visible.dirs.length === 0 && (
            <p className="field-hint">No documents here yet.</p>
          )}
          {loading && <p className="field-hint">Loading…</p>}
        </aside>
        <section className="docs-body">
          {error && <p className="error-text">{error}</p>}
          {file ? (
            <>
              <div className="docs-toolbar">
                <span className="mono">{file.path}</span>
                {file.updatedAtMs && <span>· updated {when(file.updatedAtMs)}</span>}
                <div className="btn-row">
                  <button className="btn btn-sm" onClick={() => setShowSource((s) => !s)}>
                    {showSource ? 'Rendered' : 'Source'}
                  </button>
                </div>
              </div>
              {showSource ? (
                <pre className="md-source">{file.content}</pre>
              ) : (
                <article className="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(file.content) }} />
              )}
            </>
          ) : (
            !error && <p className="thread-empty">{connected ? 'Pick a document.' : 'Not connected.'}</p>
          )}
        </section>
      </div>
    </main>
  )
}
