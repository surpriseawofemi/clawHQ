import { useEffect, useMemo, useRef, useState } from 'react'
import type { Agent, ChatMessage, SessionInfo } from '../types'
import { agentEmoji, agentLabel, messageText } from '../types'

type Props = {
  agents: Agent[]
  sessions: SessionInfo[]
  /** Threads already loaded, by session key; their text is searched too. */
  messages: Record<string, ChatMessage[]>
  onPick: (agentId: string, sessionKey: string) => void
  onClose: () => void
}

type Hit = {
  key: string
  sessionKey: string
  agentId: string
  title: string
  snippet: string
  kind: 'agent' | 'session' | 'message'
  score: number
}

const sessionTitle = (s: SessionInfo): string => s.label?.trim() || s.key.replace(/^agent:[^:]+:/, '')

/** A window of text around the first match, with the match itself wrapped in <mark>. */
const excerpt = (text: string, q: string): React.ReactNode => {
  const i = text.toLowerCase().indexOf(q)
  if (i < 0) return text.slice(0, 120)
  const start = Math.max(0, i - 40)
  const end = Math.min(text.length, i + q.length + 80)
  return (
    <>
      {start > 0 ? '…' : ''}
      {text.slice(start, i)}
      <mark>{text.slice(i, i + q.length)}</mark>
      {text.slice(i + q.length, end)}
      {end < text.length ? '…' : ''}
    </>
  )
}

/**
 * Cmd-K: find an agent, a thread by its label, or a line someone said in a thread
 * that is already loaded. Enter opens it. The gateway has no message search, so
 * text matches cover what this window has fetched; opening a thread loads it.
 */
export function SearchPalette({ agents, sessions, messages, onPick, onClose }: Props): React.JSX.Element {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement | null>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const hits = useMemo<Hit[]>(() => {
    const q = query.trim().toLowerCase()
    const byId = new Map(agents.map((a) => [a.id, a]))
    const recent = [...sessions].sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0))
    if (!q) {
      return recent.slice(0, 12).map((s) => ({
        key: `s-${s.key}`,
        sessionKey: s.key,
        agentId: s.agentId,
        title: sessionTitle(s),
        snippet: byId.get(s.agentId) ? agentLabel(byId.get(s.agentId)!) : s.agentId,
        kind: 'session' as const,
        score: 0
      }))
    }
    const out: Hit[] = []
    for (const a of agents) {
      const name = agentLabel(a).toLowerCase()
      if (name.includes(q) || a.id.toLowerCase().includes(q)) {
        out.push({
          key: `a-${a.id}`,
          sessionKey: `agent:${a.id}:main`,
          agentId: a.id,
          title: agentLabel(a),
          snippet: a.id,
          kind: 'agent',
          score: name.startsWith(q) ? 3 : 2
        })
      }
    }
    for (const s of recent) {
      const title = sessionTitle(s)
      if (title.toLowerCase().includes(q)) {
        out.push({
          key: `s-${s.key}`,
          sessionKey: s.key,
          agentId: s.agentId,
          title,
          snippet: byId.get(s.agentId) ? agentLabel(byId.get(s.agentId)!) : s.agentId,
          kind: 'session',
          score: 1.5
        })
      }
    }
    for (const [key, list] of Object.entries(messages)) {
      const s = sessions.find((x) => x.key === key)
      const agentId = s?.agentId ?? key.split(':')[1] ?? ''
      let found = 0
      for (let i = list.length - 1; i >= 0 && found < 3; i--) {
        const text = messageText(list[i])
        if (text && text.toLowerCase().includes(q)) {
          found++
          out.push({
            key: `m-${key}-${i}`,
            sessionKey: key,
            agentId,
            title: s ? sessionTitle(s) : key,
            snippet: text.replace(/\s+/g, ' '),
            kind: 'message',
            score: 1
          })
        }
      }
    }
    return out.sort((a, b) => b.score - a.score).slice(0, 40)
  }, [query, agents, sessions, messages])

  useEffect(() => {
    setActive(0)
  }, [query])

  const pick = (h: Hit): void => {
    if (!h.agentId) return
    onPick(h.agentId, h.sessionKey)
  }

  const onKey = (e: React.KeyboardEvent): void => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => Math.min(hits.length - 1, i + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(0, i - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (hits[active]) pick(hits[active])
    } else if (e.key === 'Escape') {
      onClose()
    }
  }

  const q = query.trim().toLowerCase()
  const byId = new Map(agents.map((a) => [a.id, a]))

  return (
    <div className="modal-backdrop palette-backdrop" onClick={onClose}>
      <div className="modal palette" onClick={(e) => e.stopPropagation()} role="dialog" aria-label="Search">
        <input
          ref={inputRef}
          className="palette-input"
          placeholder="Search agents, threads and messages…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKey}
        />
        <div className="palette-list">
          {hits.length === 0 && <div className="palette-empty">Nothing matches. Threads not opened yet are searched by title only.</div>}
          {hits.map((h, i) => {
            const a = byId.get(h.agentId)
            return (
              <button
                key={h.key}
                className={`palette-row${i === active ? ' is-active' : ''}`}
                onMouseEnter={() => setActive(i)}
                onClick={() => pick(h)}
              >
                <span aria-hidden="true">{a ? agentEmoji(a) : '💬'}</span>
                <span className="palette-main">
                  <span className="palette-title">{h.title}</span>
                  <span className="palette-snippet">{h.kind === 'message' && q ? excerpt(h.snippet, q) : h.snippet}</span>
                </span>
                <span className="palette-kind">{h.kind === 'agent' ? 'agent' : h.kind === 'session' ? 'thread' : 'message'}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
