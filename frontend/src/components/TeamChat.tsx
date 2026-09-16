import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { plugin, splitReport, teamPrompt, teamSessionKey } from '../state/plugin'
import { bossSessionKey } from '../state/useFleet'
import { ContentHead, Shell, SideHead, type ShellProps } from './layout/Shell'
import type { Agent, ChatMessage, Presence, TeamPost } from '../types'
import { agentEmoji, agentLabel, messageText } from '../types'
import { renderMarkdown } from '../markdown'

type Props = {
  shell: ShellProps
  agents: Agent[]
  connected: boolean
  /** Every loaded thread, by session key (from useFleet). */
  messages: Record<string, ChatMessage[]>
  /** Loads and subscribes a thread so its messages appear in `messages`. */
  openSession: (sessionKey: string) => Promise<unknown>
  onOpenAgent: (agentId: string, sessionKey?: string) => void
}

type Channel = 'room' | 'boss'

/** Agent-to-agent replies stop after this many hops, so two agents cannot loop. */
const MAX_HOPS = 3

const timeOf = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date().toDateString() === d.toDateString()
  return today
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** Markdown (bold, lists, code) with @mentions turned into chips. */
function PostText({ text, agents }: { text: string; agents: Agent[] }): React.JSX.Element {
  const html = useMemo(() => {
    const known = new Set([...agents.map((a) => a.id.toLowerCase()), 'all', 'boss'])
    return renderMarkdown(text).replace(/(^|[\s>(])@([\w.-]+)/g, (_m, pre: string, id: string) => {
      const cls = known.has(id.toLowerCase()) ? 'mention' : 'mention is-unknown'
      return `${pre}<span class="${cls}">@${id}</span>`
    })
  }, [text, agents])
  return <div className="md-body team-md" dangerouslySetInnerHTML={{ __html: html }} />
}

/** One line in either channel, already flattened. */
type Line = { id: string; atMs: number; agent?: Agent; from: string; mine: boolean; text: string; sessionKey?: string }

/**
 * Team Chat, laid out like a chat app: channels and members on the left, the
 * conversation on the right.
 *
 * Room: the shared board the plugin keeps. @agent runs a turn for that agent in
 * its agent:<id>:team session and the plugin posts the reply back; @all does that
 * for everyone; no mention just tells the room, and each agent sees it at the
 * start of its next turn. When an agent's reply mentions another agent, ClawHQ
 * gives that agent a turn too, up to MAX_HOPS deep.
 *
 * Super Boss: every agent's Super Boss Chat session merged into one timeline,
 * so everything agents have said to you is in one place. @agent sends into that
 * agent's Super Boss Chat, @all into everyone's.
 */
export function TeamChat({ shell, agents, connected, messages, openSession, onOpenAgent }: Props): React.JSX.Element {
  const [channel, setChannel] = useState<Channel>('room')
  const [posts, setPosts] = useState<TeamPost[]>([])
  const [presence, setPresence] = useState<Record<string, Presence>>({})
  const [pluginPresent, setPluginPresent] = useState<boolean | null>(null)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLTextAreaElement>(null)
  const handled = useRef<Set<string>>(new Set())

  // ---- data ------------------------------------------------------------------
  const refresh = useCallback(async () => {
    try {
      const status = await api.plugin.status()
      setPluginPresent(status.present)
      if (connected && status.present) {
        const [list, pres] = await Promise.all([plugin.team.list(300), plugin.presence().catch(() => null)])
        setPosts(list)
        if (pres) setPresence(Object.fromEntries(pres.agents.map((p) => [p.agentId, p])))
      } else {
        setPosts([])
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  // Gives an agent a turn on one board post, once per post per agent across every
  // open ClawHQ (the idempotency key makes the gateway drop repeats).
  const giveTurn = useCallback(
    async (post: TeamPost, agentId: string, toAll: boolean, from: string) => {
      const mark = `${post.id}:${agentId}`
      if (handled.current.has(mark)) return
      handled.current.add(mark)
      const key = teamSessionKey(agentId)
      try {
        await api.rpc.request('sessions.create', { key, agentId, label: 'Team Chat' })
      } catch {
        /* already there */
      }
      await plugin.team.turn(agentId, post.id).catch(() => undefined)
      await api.rpc.request('chat.send', { sessionKey: key, message: teamPrompt(post.text, toAll, from), idempotencyKey: `team:${post.id}:${agentId}` })
    },
    []
  )

  useEffect(() => {
    void refresh()
    const off = api.onGatewayEvent(({ event, payload }) => {
      if (event === 'clawhq.presence') {
        const p = payload as { agents?: Presence[] } | undefined
        if (p?.agents) setPresence(Object.fromEntries(p.agents.map((x) => [x.agentId, x])))
        return
      }
      if (event !== 'clawhq.team.changed') return
      const p = payload as { id?: string; from?: string; fromKind?: string; mentions?: string[]; hops?: number } | undefined
      void refresh()
      // An agent addressing another agent: that agent answers too, within the hop cap.
      if (p?.fromKind === 'agent' && p.id && (p.hops ?? 0) < MAX_HOPS) {
        const mentions = (p.mentions ?? []).map((m) => m.toLowerCase())
        const toAll = mentions.includes('all')
        const targets = agents.filter((a) => a.id !== p.from && (toAll || mentions.includes(a.id.toLowerCase())))
        if (targets.length === 0) return
        void plugin.team.list(50).then((list) => {
          const post = list.find((x) => x.id === p.id)
          if (!post) return
          const fromAgent = agents.find((a) => a.id === p.from)
          const from = `${fromAgent ? agentLabel(fromAgent) : p.from} (agent @${p.from})`
          for (const t of targets) void giveTurn(post, t.id, toAll, from).catch((err) => setError(err instanceof Error ? err.message : String(err)))
        })
      }
    })
    return off
  }, [refresh, agents, giveTurn])

  // Super Boss: keep every agent's boss session loaded and subscribed.
  useEffect(() => {
    if (!connected || channel !== 'boss') return
    for (const a of agents) void openSession(bossSessionKey(a.id))
  }, [connected, channel, agents, openSession])

  // ---- lines for the open channel ------------------------------------------------
  const lines = useMemo<Line[]>(() => {
    if (channel === 'room') {
      return posts.map((p) => {
        const a = p.fromKind === 'agent' ? agents.find((x) => x.id === p.from) : undefined
        return { id: p.id, atMs: p.atMs, agent: a, from: p.fromKind === 'human' ? 'You' : a ? agentLabel(a) : p.from, mine: p.fromKind === 'human', text: p.text, sessionKey: p.sessionKey }
      })
    }
    const out: Line[] = []
    for (const a of agents) {
      const key = bossSessionKey(a.id)
      for (const [i, m] of (messages[key] ?? []).entries()) {
        if (m.role !== 'user' && m.role !== 'assistant') continue
        const text = messageText(m).trim()
        if (!text) continue
        const mine = m.role === 'user'
        out.push({ id: `${key}:${m.__openclaw?.id ?? i}`, atMs: m.timestamp ?? 0, agent: a, from: mine ? `You → ${agentLabel(a)}` : agentLabel(a), mine, text, sessionKey: key })
      }
    }
    return out.sort((x, y) => x.atMs - y.atMs)
  }, [channel, posts, agents, messages])

  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines.length, channel])

  // ---- typing ----------------------------------------------------------------------
  const typingIn = (a: Agent): Channel | null => {
    const p = presence[a.id]
    if (!p || p.state !== 'working' || !p.sessionKey) return null
    if (p.sessionKey.endsWith(':team')) return 'room'
    if (p.sessionKey.endsWith(':superboss')) return 'boss'
    return null
  }
  const typingHere = agents.filter((a) => typingIn(a) === channel)

  // ---- composer ----------------------------------------------------------------------
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

  const insertMention = (id: string): void => {
    const el = box.current
    const at = mentionQuery ? mentionQuery.start : el ? (el.selectionStart ?? draft.length) : draft.length
    const end = mentionQuery ? caret : at
    const before = draft.slice(0, at)
    const pad = before.length > 0 && !/\s$/.test(before) ? ' ' : ''
    const next = `${before}${pad}@${id} ${draft.slice(end)}`
    setDraft(next)
    const pos = before.length + pad.length + id.length + 2
    requestAnimationFrame(() => {
      box.current?.focus()
      box.current?.setSelectionRange(pos, pos)
      setCaret(pos)
    })
  }

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
    if (channel === 'boss' && ids.length === 0) {
      setError('Super Boss messages need a target: @agent or @all.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      if (channel === 'room') {
        const post = await plugin.team.post(text, all ? ['all'] : ids)
        setDraft('')
        void refresh()
        await Promise.allSettled(ids.map((id) => giveTurn(post, id, all, 'the boss (human)')))
      } else {
        setDraft('')
        await Promise.allSettled(
          ids.map(async (id) => {
            const key = bossSessionKey(id)
            try {
              await api.rpc.request('sessions.create', { key, agentId: id, label: 'Super Boss Chat' })
            } catch {
              /* already there */
            }
            await openSession(key)
            await api.rpc.sendChat(key, text)
          })
        )
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const fileIssues = async (l: Line): Promise<void> => {
    if (!l.agent) return
    setBusy(true)
    try {
      for (const it of splitReport(l.text)) {
        await plugin.issues.create({ kind: 'question', title: it.title, body: it.body, urgency: it.urgency, from: l.agent.id, fromKind: 'agent', sessionKey: l.sessionKey })
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  // ---- render ----------------------------------------------------------------------
  const side = (
    <>
      <SideHead>
        <span className="side-title">Team Chat</span>
      </SideHead>
      <div className="team-side">
        <div className="team-side-label">Channels</div>
        <button className={`team-channel${channel === 'room' ? ' is-active' : ''}`} onClick={() => setChannel('room')}>
          <span className="team-hash">#</span> room
          <span className="plugin-desc">{posts.length}</span>
        </button>
        <button className={`team-channel${channel === 'boss' ? ' is-active' : ''}`} onClick={() => setChannel('boss')}>
          <span className="team-hash">✉</span> super boss
        </button>
        <div className="team-side-label">Agents · {agents.length}</div>
        {agents.map((a) => {
          const p = presence[a.id]
          const typing = typingIn(a)
          const state = !connected ? 'offline' : p?.state === 'working' ? 'working' : 'online'
          return (
            <button key={a.id} className="team-member" onClick={() => insertMention(a.id)} title={`Insert @${a.id}`}>
              <span className="team-member-avatar">
                {agentEmoji(a)}
                <i className={`desk-dot ${state === 'working' ? 'is-working' : state === 'online' ? 'is-online' : 'is-idle'}`} />
              </span>
              <span className="team-member-meta">
                <span className="team-member-name">{agentLabel(a)}</span>
                <span className="team-member-sub">
                  {typing ? `typing in ${typing === 'room' ? '#room' : 'super boss'}…` : state === 'working' ? (p?.tool ? `using ${p.tool}` : 'working') : state}
                </span>
              </span>
            </button>
          )
        })}
      </div>
    </>
  )

  const sub =
    channel === 'room'
      ? 'Everyone reads this. @name asks that agent, @all asks everyone, no @ just tells the room. Agents answer each other too.'
      : 'Everything every agent has said to you, in one place. @name or @all sends into their Super Boss Chat.'

  return (
    <Shell shell={shell} title="Team Chat" side={side}>
      <ContentHead title={channel === 'room' ? '# room' : 'Super Boss'} subtitle={sub} />
      <div className="content-body team-body">
        {pluginPresent === false && channel === 'room' && <p className="field-hint">The room needs the ClawHQ gateway plugin (Settings → Plugins).</p>}
        {error && <p className="error-text">{error}</p>}
        <div className="team-scroll" ref={scroller}>
          {lines.length === 0 && (
            <p className="field-hint team-empty">
              {channel === 'room' ? 'Nothing yet. Say something, or click an agent on the left to address it.' : 'No Super Boss messages loaded yet.'}
            </p>
          )}
          {lines.map((l) => (
            <div key={l.id} className={`team-post${l.mine ? ' is-mine' : ''}`}>
              <span className="team-avatar" title={l.agent?.id ?? l.from}>
                {l.mine ? '👤' : l.agent ? agentEmoji(l.agent) : '🤖'}
              </span>
              <div className="team-bubble">
                <div className="team-meta">
                  <button className="team-from" onClick={() => (l.agent ? onOpenAgent(l.agent.id, l.sessionKey) : undefined)} disabled={!l.agent} title={l.agent ? 'Open this agent' : undefined}>
                    {l.from}
                  </button>
                  <span className="team-time">{l.atMs ? timeOf(l.atMs) : ''}</span>
                  {channel === 'boss' && !l.mine && l.agent && splitReport(l.text).length > 1 && (
                    <button className="btn btn-sm btn-ghost team-split" onClick={() => void fileIssues(l)} title="One issue per bold heading, so you can answer them one by one">
                      File as {splitReport(l.text).length} issues
                    </button>
                  )}
                </div>
                <div className="team-text">
                  <PostText text={l.text} agents={agents} />
                </div>
              </div>
            </div>
          ))}
        </div>
        <div className="team-typing">{typingHere.length > 0 ? `${typingHere.map(agentLabel).join(', ')} ${typingHere.length === 1 ? 'is' : 'are'} typing…` : ' '}</div>
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
                    insertMention(c.id)
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
            placeholder={!connected ? 'Connect to a gateway first' : channel === 'room' ? 'Message #room, or @someone…' : '@agent or @all, then your message…'}
            disabled={!connected || (channel === 'room' && pluginPresent === false)}
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
                  insertMention(candidates[hi].id)
                  return
                }
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
          />
          <button className="btn btn-primary btn-send" onClick={() => void send()} disabled={!connected || busy || !draft.trim()}>
            {busy ? 'Sending…' : 'Send'}
          </button>
        </div>
      </div>
    </Shell>
  )
}
