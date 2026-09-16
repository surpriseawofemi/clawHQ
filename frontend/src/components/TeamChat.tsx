import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { plugin, teamPrompt, teamSessionKey } from '../state/plugin'
import { ContentHead, Shell, type ShellProps } from './layout/Shell'
import type { Agent, TeamPost } from '../types'
import { agentEmoji, agentLabel } from '../types'

type Props = {
  shell: ShellProps
  agents: Agent[]
  connected: boolean
  onOpenAgent: (agentId: string, sessionKey?: string) => void
}

const timeOf = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date().toDateString() === d.toDateString()
  return today
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** Renders @mentions in a post as chips. */
function PostText({ text, agents }: { text: string; agents: Agent[] }): React.JSX.Element {
  const parts = text.split(/(@[\w.-]+)/g)
  return (
    <>
      {parts.map((p, i) => {
        if (!p.startsWith('@')) return <span key={i}>{p}</span>
        const id = p.slice(1).toLowerCase()
        const a = agents.find((x) => x.id.toLowerCase() === id)
        const known = a || id === 'all' || id === 'boss'
        return (
          <span key={i} className={`mention${known ? '' : ' is-unknown'}`} title={a ? agentLabel(a) : undefined}>
            {a ? `${agentEmoji(a)} ` : ''}
            {p}
          </span>
        )
      })}
    </>
  )
}

/**
 * Team Chat: one board the human and every agent share, kept by the gateway
 * plugin. @agent runs a turn for that agent, @all for everyone; a post with no
 * mention just sits on the board and each agent sees it at the start of its next
 * turn. Agents answer through the plugin, which posts their reply here.
 */
export function TeamChat({ shell, agents, connected, onOpenAgent }: Props): React.JSX.Element {
  const [posts, setPosts] = useState<TeamPost[]>([])
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState<Record<string, number>>({})
  const scroller = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLTextAreaElement>(null)

  const refresh = useCallback(async () => {
    try {
      const status = await api.plugin.status()
      setPluginPresent(status.present)
      setPosts(connected && status.present ? await plugin.team.list(300) : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  useEffect(() => {
    void refresh()
    const off = api.onGatewayEvent(({ event, payload }) => {
      if (event === 'clawhq.team.changed') {
        void refresh()
        const p = payload as { fromKind?: string; from?: string } | undefined
        if (p?.fromKind === 'agent' && p.from) setPending((prev) => ({ ...prev, [p.from as string]: Math.max(0, (prev[p.from as string] ?? 1) - 1) }))
      }
    })
    return off
  }, [refresh])

  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [posts.length])

  // One line, grows to five, like the agent chat box.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const cs = getComputedStyle(el)
    const line = parseFloat(cs.lineHeight) || 20
    const chrome = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom) + parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth)
    const max = Math.round(line * 5 + chrome)
    el.style.height = 'auto'
    const want = el.scrollHeight
    el.style.height = `${Math.min(want, max)}px`
    el.style.overflowY = want > max ? 'auto' : 'hidden'
  }, [draft])

  // ---- @mention autocomplete ----------------------------------------------
  const [caret, setCaret] = useState(0)
  const mentionQuery = useMemo(() => {
    const before = draft.slice(0, caret)
    const m = /(^|\s)@([\w.-]*)$/.exec(before)
    return m ? { start: caret - m[2].length - 1, q: m[2].toLowerCase() } : null
  }, [draft, caret])
  const candidates = useMemo(() => {
    if (!mentionQuery) return []
    const q = mentionQuery.q
    const all = [{ id: 'all', label: 'Everyone', emoji: '📣' }, ...agents.map((a) => ({ id: a.id, label: agentLabel(a), emoji: agentEmoji(a) }))]
    return all.filter((c) => c.id.toLowerCase().startsWith(q) || c.label.toLowerCase().includes(q)).slice(0, 8)
  }, [mentionQuery, agents])
  const [hi, setHi] = useState(0)
  useEffect(() => setHi(0), [mentionQuery?.q])

  const complete = (id: string): void => {
    if (!mentionQuery) return
    const next = `${draft.slice(0, mentionQuery.start)}@${id} ${draft.slice(caret)}`
    setDraft(next)
    const pos = mentionQuery.start + id.length + 2
    requestAnimationFrame(() => {
      box.current?.focus()
      box.current?.setSelectionRange(pos, pos)
      setCaret(pos)
    })
  }

  // ---- posting ---------------------------------------------------------------
  const targetsOf = (text: string): { ids: string[]; all: boolean } => {
    const ids = new Set<string>()
    let all = false
    for (const m of text.matchAll(/(^|\s)@([\w.-]+)/g)) {
      const id = m[2].toLowerCase()
      if (id === 'all') all = true
      else {
        const a = agents.find((x) => x.id.toLowerCase() === id)
        if (a) ids.add(a.id)
      }
    }
    return { ids: all ? agents.map((a) => a.id) : [...ids], all }
  }

  const send = async (): Promise<void> => {
    const text = draft.trim()
    if (!text || !connected || busy) return
    const { ids, all } = targetsOf(text)
    setBusy('post')
    setError(null)
    try {
      await plugin.team.post(text, all ? ['all'] : ids)
      setDraft('')
      void refresh()
      if (ids.length > 0) {
        setPending((prev) => ({ ...prev, ...Object.fromEntries(ids.map((id) => [id, (prev[id] ?? 0) + 1])) }))
        // Every addressed agent gets a turn in its own Team Chat session; the plugin
        // posts each reply back to the board when the run ends.
        await Promise.allSettled(
          ids.map(async (id) => {
            const key = teamSessionKey(id)
            try {
              await api.rpc.request('sessions.create', { key, agentId: id, label: 'Team Chat' })
            } catch {
              /* exists already, or the gateway adopts it on send */
            }
            await api.rpc.sendChat(key, teamPrompt(text, all))
          })
        )
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const waiting = Object.entries(pending).filter(([, n]) => n > 0)

  return (
    <Shell shell={shell} title="Team Chat">
      <ContentHead title="Team Chat" subtitle="One room for you and every agent. @name asks that agent, @all asks everyone, no @ just tells the room.">
        {waiting.length > 0 && (
          <span className="plugin-desc">
            Waiting on {waiting.map(([id]) => agentLabel(agents.find((a) => a.id === id) ?? ({ id } as Agent))).join(', ')}…
          </span>
        )}
      </ContentHead>
      <div className="content-body team-body">
        {pluginPresent === false && (
          <p className="field-hint">Team Chat needs the ClawHQ gateway plugin (Settings → Plugins).</p>
        )}
        {error && <p className="error-text">{error}</p>}
        <div className="team-scroll" ref={scroller}>
          {posts.length === 0 && pluginPresent && (
            <p className="field-hint team-empty">
              Nothing yet. Say something to the room, or ask someone with @ — start typing @ for the list.
            </p>
          )}
          {posts.map((p) => {
            const a = p.fromKind === 'agent' ? agents.find((x) => x.id === p.from) : undefined
            const mine = p.fromKind === 'human'
            return (
              <div key={p.id} className={`team-post${mine ? ' is-mine' : ''}`}>
                <span className="team-avatar" title={p.from}>
                  {mine ? '👤' : a ? agentEmoji(a) : '🤖'}
                </span>
                <div className="team-bubble">
                  <div className="team-meta">
                    <button
                      className="team-from"
                      onClick={() => (a ? onOpenAgent(a.id, p.sessionKey) : undefined)}
                      disabled={!a}
                      title={a ? 'Open this agent' : undefined}
                    >
                      {mine ? 'You' : a ? agentLabel(a) : p.from}
                    </button>
                    <span className="team-time">{timeOf(p.atMs)}</span>
                  </div>
                  <div className="team-text">
                    <PostText text={p.text} agents={agents} />
                  </div>
                </div>
              </div>
            )
          })}
        </div>
        <div className="team-composer">
          {mentionQuery && candidates.length > 0 && (
            <ul className="mention-menu" role="listbox">
              {candidates.map((c, i) => (
                <li
                  key={c.id}
                  role="option"
                  aria-selected={i === hi}
                  className={i === hi ? 'is-active' : ''}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    complete(c.id)
                  }}
                >
                  <span>{c.emoji}</span> {c.label} <span className="plugin-desc">@{c.id}</span>
                </li>
              ))}
            </ul>
          )}
          <textarea
            ref={box}
            rows={1}
            value={draft}
            placeholder={connected ? 'Message the room, or @someone…' : 'Connect to a gateway first'}
            disabled={!connected || pluginPresent === false}
            onChange={(e) => {
              setDraft(e.target.value)
              setCaret(e.target.selectionStart ?? e.target.value.length)
            }}
            onSelect={(e) => setCaret((e.target as HTMLTextAreaElement).selectionStart ?? 0)}
            onKeyDown={(e) => {
              if (mentionQuery && candidates.length > 0) {
                if (e.key === 'ArrowDown') {
                  e.preventDefault()
                  setHi((h) => (h + 1) % candidates.length)
                  return
                }
                if (e.key === 'ArrowUp') {
                  e.preventDefault()
                  setHi((h) => (h - 1 + candidates.length) % candidates.length)
                  return
                }
                if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
                  e.preventDefault()
                  complete(candidates[hi].id)
                  return
                }
                if (e.key === 'Escape') {
                  e.preventDefault()
                  setDraft((d) => `${d} `)
                  return
                }
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
          />
          <button className="btn btn-primary btn-send" onClick={() => void send()} disabled={!connected || !!busy || !draft.trim()}>
            {busy ? 'Posting…' : 'Post'}
          </button>
        </div>
      </div>
    </Shell>
  )
}
