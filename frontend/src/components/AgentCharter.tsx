import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { renderMarkdown } from '../markdown'
import { AgentHeader, type AgentTab } from './AgentHeader'
import type { Agent } from '../types'

type CharterFile = {
  name: string
  missing?: boolean
  size?: number
  updatedAtMs?: number
}

type FileBody = {
  name: string
  content: string
  hash?: string
  updatedAtMs?: number
}

/** What each charter file is for, in the words the gateway uses. */
const PURPOSE: Record<string, string> = {
  'AGENTS.md': 'Workspace conventions and standing instructions. Read at every session start.',
  'SOUL.md': 'Personality, tone and values. Who the agent is when it speaks.',
  'IDENTITY.md': 'Name, emoji and a one-line vibe. Drives the sidebar identity.',
  'USER.md': 'What the agent knows about you: preferences and directives.',
  'MEMORY.md': 'Long-term memory the agent keeps for itself. Edit with care.'
}

const ORDER = ['AGENTS.md', 'SOUL.md', 'IDENTITY.md', 'USER.md', 'MEMORY.md']

type Props = {
  agent: Agent
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  connected: boolean
  /** The agent list should refresh after IDENTITY.md changes. */
  onSaved: () => void
}

/**
 * The files that shape an agent: read and written through the gateway's
 * agents.files RPCs, which are scoped to exactly these names. A save goes straight
 * to the agent's workspace on the gateway host and applies at its next session start.
 */
export function AgentCharter({ agent, tab, onTab, connected, onSaved }: Props): React.JSX.Element {
  const [files, setFiles] = useState<CharterFile[]>([])
  const [name, setName] = useState('AGENTS.md')
  const [body, setBody] = useState<FileBody | null>(null)
  const [draft, setDraft] = useState('')
  const [preview, setPreview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)

  const refreshList = useCallback(async () => {
    if (!connected) return
    try {
      const res = await api.rpc.request<{ files?: CharterFile[] }>('agents.files.list', { agentId: agent.id })
      const known = new Map((res?.files ?? []).map((f) => [f.name, f]))
      setFiles(ORDER.map((n) => known.get(n) ?? { name: n, missing: true }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [agent.id, connected])

  const load = useCallback(
    async (target: string) => {
      if (!connected) return
      setError(null)
      setNote(null)
      try {
        const res = await api.rpc.request<{ file?: FileBody }>('agents.files.get', { agentId: agent.id, name: target })
        const f = res?.file ?? { name: target, content: '' }
        setBody(f)
        setDraft(f.content ?? '')
      } catch (err) {
        setBody({ name: target, content: '' })
        setDraft('')
        setError(err instanceof Error ? err.message : String(err))
      }
    },
    [agent.id, connected]
  )

  useEffect(() => {
    setName('AGENTS.md')
    void refreshList()
  }, [agent.id, refreshList])

  useEffect(() => {
    void load(name)
  }, [name, load])

  const dirty = body !== null && draft !== (body.content ?? '')

  const save = async (): Promise<void> => {
    setBusy(true)
    setError(null)
    setNote(null)
    try {
      await api.rpc.request('agents.files.set', { agentId: agent.id, name, content: draft })
      setNote(`Saved ${name}. The agent picks it up at its next session start.`)
      await load(name)
      await refreshList()
      if (name === 'IDENTITY.md') onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="chat">
      <AgentHeader agent={agent} tab={tab} onTab={onTab} />
      <div className="charter-grid">
        <aside className="docs-list">
          {files.map((f) => (
            <button
              key={f.name}
              className={`docs-item${name === f.name ? ' is-active' : ''}`}
              onClick={() => {
                if (dirty && !window.confirm(`Discard unsaved changes to ${name}?`)) return
                setName(f.name)
              }}
            >
              <span>{f.name.replace(/\.md$/, '')}</span>
              <small>{f.missing ? 'not created yet' : `${Math.max(1, Math.round((f.size ?? 0) / 1024))} KB`}</small>
            </button>
          ))}
          <p className="charter-hint">
            These files are the agent&apos;s standing instructions. Changes apply the next time it
            starts a session.
          </p>
        </aside>
        <section className="docs-body">
          <div className="docs-toolbar">
            <span>{PURPOSE[name] ?? ''}</span>
            <div className="btn-row">
              <button className="btn btn-sm" onClick={() => setPreview((p) => !p)}>
                {preview ? 'Edit' : 'Preview'}
              </button>
              <button
                className="btn btn-sm"
                disabled={!dirty || busy}
                onClick={() => setDraft(body?.content ?? '')}
              >
                Revert
              </button>
              <button className="btn btn-sm btn-primary" disabled={!dirty || busy || !connected} onClick={() => void save()}>
                {busy ? 'Saving…' : dirty ? 'Save' : 'Saved'}
              </button>
            </div>
          </div>
          {preview ? (
            <article className="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(draft) }} />
          ) : (
            <textarea
              className="md-source"
              value={draft}
              spellCheck={false}
              disabled={!connected}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === 's') {
                  e.preventDefault()
                  if (dirty && !busy) void save()
                }
              }}
            />
          )}
          {error && <p className="error-text">{error}</p>}
          {note && <p className="note-text">{note}</p>}
        </section>
      </div>
    </main>
  )
}
