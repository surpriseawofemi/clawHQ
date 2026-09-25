import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api } from '../api'
import { takeChatPrefill } from './ServersPage'
import { renderMarkdown } from '../markdown'
import type { AgentID, ClaudeBlock, ClaudeMsg, ClaudeSession, ClaudeState } from '../types'

const AGENT_LABEL: Record<AgentID, string> = { claude: 'Claude Code', codex: 'Codex', gemini: 'Gemini CLI', grok: 'Grok CLI' }

type Queued = { id: string; text: string }

const timeOf = (ms: number): string => (ms ? new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '')
const ago = (ms: number): string => {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86_400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86_400)}d ago`
}

function ToolRow({ b }: { b: ClaudeBlock }): React.JSX.Element {
  const [open, setOpen] = useState(false)
  if (b.type === 'tool_use') {
    const input = typeof b.input === 'string' ? b.input : JSON.stringify(b.input ?? {}, null, 1)
    const short = (() => {
      const i = (b.input ?? {}) as Record<string, unknown>
      return String(i.command ?? i.file_path ?? i.path ?? i.pattern ?? i.query ?? i.url ?? i.description ?? '').slice(0, 140)
    })()
    return (
      <div className="cc-tool">
        <button className="cc-tool-head" onClick={() => setOpen((o) => !o)}>
          <span className="cc-tool-name">🔧 {b.name}</span>
          <span className="cc-tool-short">{short}</span>
        </button>
        {open && <pre className="cc-tool-body">{input.slice(0, 6000)}</pre>}
      </div>
    )
  }
  return (
    <div className={`cc-tool cc-result${b.isError ? ' is-bad' : ''}`}>
      <button className="cc-tool-head" onClick={() => setOpen((o) => !o)}>
        <span className="cc-tool-name">{b.isError ? '⚠️ error' : '↩ result'}</span>
        <span className="cc-tool-short">{(b.text ?? '').split('\n')[0].slice(0, 140)}</span>
      </button>
      {open && <pre className="cc-tool-body">{(b.text ?? '').slice(0, 6000)}</pre>}
    </div>
  )
}

/**
 * Chat with Claude Code on a server: every message is one headless run over
 * SSH that resumes the same Claude Code session, so this and the terminal share
 * one conversation. Text streams in; tool calls fold; the session picker lists
 * what Claude Code has for the project folder.
 */
type Live = { projectId: string; projectName: string; dir: string; session: string; helperWired: boolean }
const chatMemory = new Map<string, ClaudeMsg[]>()

export function ClaudeChat({ serverId, serverName, live }: { serverId: string; serverName: string; live?: Live }): React.JSX.Element {
  // Live mode: type into the interactive Claude Code running in tmux on the server;
  // its replies come back through the helper's hook. Remembered per project.
  const liveKey = `clawhq.live.${serverId}.${live?.projectId ?? ''}`
  const [isLive, setIsLive] = useState<boolean>(() => {
    try {
      return live ? localStorage.getItem(liveKey) === '1' : false
    } catch {
      return false
    }
  })
  const setLiveMode = (on: boolean): void => {
    setIsLive(on)
    try {
      localStorage.setItem(liveKey, on ? '1' : '0')
    } catch {
      /* per machine */
    }
  }
  const [state, setState] = useState<ClaudeState | null>(null)
  const chatKey = `${serverId}:${live?.projectId ?? ''}`
  const [msgs, setMsgs] = useState<ClaudeMsg[]>(chatMemory.get(chatKey) ?? [])
  useEffect(() => {
    chatMemory.set(chatKey, msgs)
  }, [chatKey, msgs])
  const [sessions, setSessions] = useState<ClaudeSession[]>([])
  const [showSessions, setShowSessions] = useState(false)
  const [draft, setDraft] = useState(() => takeChatPrefill())
  const [stream, setStream] = useState<{ runId: string; text: string; blocks: ClaudeBlock[] } | null>(null)
  const [queue, setQueue] = useState<Queued[]>([])
  const [error, setError] = useState<string | null>(null)
  const [cost, setCost] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const scroller = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLTextAreaElement>(null)
  const streamRef = useRef(stream)
  streamRef.current = stream

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const st = await api.claude.state(serverId)
      setState(st)
      if (isLive && live) {
        const ev = await api.helper.outbox(serverId, Date.now() - 3 * 86_400_000, 300)
        setMsgs(
          ev
            .filter((e) => e.type === 'reply' && (!live.dir || e.project === live.dir) && (e.text ?? '').trim())
            .map((e) => ({ role: 'assistant' as const, atMs: e.ts, blocks: [{ type: 'text' as const, text: e.text ?? '' }] }))
        )
      } else {
        setMsgs(await api.claude.history(serverId))
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [serverId, isLive, live])
  useEffect(() => {
    void reload()
  }, [reload])
  useEffect(() => {
    if (!isLive) return
    const t = setInterval(() => void reload(), 20_000)
    return () => clearInterval(t)
  }, [isLive, reload])

  const send = useCallback(
    async (text: string) => {
      if (isLive && live) {
        try {
          setMsgs((prev) => [...prev, { role: 'user', atMs: Date.now(), blocks: [{ type: 'text', text }] }])
          setLiveWaiting(true)
          const name = await api.helper.startSession(serverId, live.projectId, '')
          await api.helper.sendToSession(serverId, name, text)
          setError(null)
        } catch (err) {
          setLiveWaiting(false)
          setError(err instanceof Error ? err.message : String(err))
        }
        return
      }
      try {
        const runId = await api.claude.send(serverId, text)
        setMsgs((prev) => [...prev, { role: 'user', atMs: Date.now(), blocks: [{ type: 'text', text }] }])
        setStream({ runId, text: '', blocks: [] })
        setState((s) => (s ? { ...s, running: true } : s))
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      }
    },
    [serverId, isLive, live]
  )

  // Events from Go: deltas stream, whole assistant/user records carry tool calls,
  // result ends the run; then the next queued message goes out.
  useEffect(
    () =>
      api.onClaudeEvent((e) => {
        if (e.serverId !== serverId) return
        if (e.type === 'delta') {
          setStream((l) => (l ? { ...l, text: l.text + (e.text ?? '') } : { runId: e.runId, text: e.text ?? '', blocks: [] }))
        } else if (e.type === 'assistant' || e.type === 'user') {
          const content = (e.message?.content ?? []) as unknown[]
          if (!Array.isArray(content)) return
          const blocks: ClaudeBlock[] = []
          for (const raw of content) {
            const b = raw as { type?: string; name?: string; input?: unknown; content?: unknown; is_error?: boolean; text?: string }
            if (b.type === 'tool_use') blocks.push({ type: 'tool_use', name: b.name, input: b.input })
            else if (b.type === 'tool_result') {
              const c = b.content
              const text = typeof c === 'string' ? c : Array.isArray(c) ? (c as { type?: string; text?: string }[]).filter((x) => x.type === 'text').map((x) => x.text ?? '').join('\n') : ''
              blocks.push({ type: 'tool_result', text, isError: b.is_error })
            }
          }
          if (blocks.length === 0) return
          setStream((l) => {
            const base = l ?? { runId: e.runId, text: '', blocks: [] }
            // Text already streamed stays; tool blocks join the running message.
            const done: ClaudeBlock[] = base.text ? [{ type: 'text', text: base.text }] : []
            setMsgs((prev) => [...prev, { role: e.type === 'user' ? 'user' : 'assistant', atMs: Date.now(), blocks: [...done, ...blocks] }])
            return { ...base, text: '' }
          })
        } else if (e.type === 'result') {
          const r = e.result
          if (r?.total_cost_usd !== undefined) setCost(`$${r.total_cost_usd.toFixed(3)} · ${Math.round((r.duration_ms ?? 0) / 1000)}s · ${r.num_turns ?? 0} turns`)
          setStream((l) => {
            if (l && l.text.trim()) setMsgs((prev) => [...prev, { role: 'assistant', atMs: Date.now(), blocks: [{ type: 'text', text: l.text }] }])
            return null
          })
          if (r?.is_error) setError(e.text || 'Claude Code reported an error')
          setState((s) => (s ? { ...s, running: false, sessionId: e.sessionId || s.sessionId } : s))
        } else if (e.type === 'error') {
          setError(e.text || 'Claude Code failed')
          setStream(null)
          setState((s) => (s ? { ...s, running: false } : s))
        } else if (e.type === 'done') {
          setStream(null)
          setState((s) => (s ? { ...s, running: false } : s))
          setQueue((q) => {
            if (q.length === 0) return q
            const [next, ...rest] = q
            void send(next.text)
            return rest
          })
        }
      }),
    [serverId, send]
  )

  useEffect(() => {
    if (!isLive || !live) return
    return api.onHelperEvent((e) => {
      if (e.serverId !== serverId || e.event.type !== 'reply' || (live.dir && e.event.project !== live.dir)) return
      const text = (e.event.text ?? '').trim()
      if (!text) return
      setMsgs((prev) => [...prev, { role: 'assistant', atMs: e.event.ts, blocks: [{ type: 'text', text }] }])
      setLiveWaiting(false)
    })
  }, [isLive, live, serverId])
  const [liveWaiting, setLiveWaiting] = useState(false)

  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [msgs.length, stream?.text, liveWaiting])

  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const cs = getComputedStyle(el)
    const line = parseFloat(cs.lineHeight) || 20
    const chrome = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom) + parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth)
    const max = Math.round(line * 6 + chrome)
    el.style.height = 'auto'
    const want = el.scrollHeight
    el.style.height = `${Math.min(want, max)}px`
    el.style.overflowY = want > max ? 'auto' : 'hidden'
  }, [draft])

  const submit = (): void => {
    const text = draft.trim()
    if (!text) return
    setDraft('')
    if (!isLive && (state?.running || stream)) setQueue((q) => [...q, { id: `q-${Date.now()}`, text }])
    else void send(text)
  }

  const running = Boolean(state?.running || stream) || liveWaiting

  return (
    <div className="cc">
      <div className="cc-bar">
        {live && (
          <div className="seg" title={live.helperWired ? 'Live: the interactive Claude Code in tmux on the server, replies via the helper. Headless: one run per message.' : 'Wire the project on Health first to use the live session'}>
            <button className={!isLive ? 'is-active' : ''} onClick={() => { setLiveMode(false); setMsgs([]) }}>Headless</button>
            <button className={isLive ? 'is-active' : ''} disabled={!live.helperWired} onClick={() => { setLiveMode(true); setMsgs([]) }}>Live session</button>
          </div>
        )}
        {!isLive && (
        <select value={state?.agent ?? 'claude'} onChange={(e) => void api.claude.setAgent(serverId, e.target.value).then((st) => { setState(st); setMsgs([]); void reload() }).catch((err) => setError(String(err)))} aria-label="Agent" title="Which coding agent answers in this project">
          <option value="claude">Claude Code</option>
          <option value="codex">Codex</option>
          <option value="gemini">Gemini CLI (experimental)</option>
          <option value="grok">Grok CLI</option>
        </select>
        )}
        {isLive && live && <span className="plugin-desc mono">tmux: {live.session}</span>}
        {!isLive && (state?.agent ?? 'claude') === 'claude' && (
        <div className="cc-session">
          <button className="btn btn-sm" onClick={() => { setShowSessions((s) => !s); if (!showSessions) void api.claude.sessions(serverId).then(setSessions).catch(() => undefined) }}>
            {state?.sessionId ? `Session ${state.sessionId.slice(0, 8)}` : 'New session'} ▾
          </button>
          {showSessions && (
            <div className="cc-sessions">
              <button className="cc-sess" onClick={() => { void api.claude.setSession(serverId, '').then(() => { setShowSessions(false); setMsgs([]); void reload() }) }}>
                <span className="cc-sess-title">＋ New session</span>
              </button>
              {sessions.map((s) => (
                <button key={s.id} className={`cc-sess${s.id === state?.sessionId ? ' is-active' : ''}`} onClick={() => { void api.claude.setSession(serverId, s.id).then(() => { setShowSessions(false); void reload() }) }}>
                  <span className="cc-sess-title">{s.prompt}</span>
                  <span className="cc-sess-sub">{ago(s.modMs)} · {Math.round(s.size / 1024)} KB</span>
                </button>
              ))}
              {sessions.length === 0 && <p className="field-hint">No sessions for this folder yet.</p>}
            </div>
          )}
        </div>
        )}
        {!isLive && (state?.agent ?? 'claude') !== 'claude' && (
          <button className="btn btn-sm" onClick={() => { void api.claude.setSession(serverId, '').then(() => { setMsgs([]); void reload() }) }} title="Forget the current thread and start fresh">
            {state?.sessionId ? `Thread ${state.sessionId.slice(0, 8)} · New` : 'New thread'}
          </button>
        )}
        <select value={state?.mode ?? 'auto'} onChange={(e) => void api.claude.setMode(serverId, e.target.value).then(setState).catch((err) => setError(String(err)))} aria-label="Permission mode" title="Auto: runs anything. Semi: reads, edits and safe commands. Manual: reads only.">
          <option value="auto">Auto</option>
          <option value="semi">Semi</option>
          <option value="manual">Manual</option>
        </select>
        <span className="plugin-desc cc-dir">{state?.dir || '~'}</span>
        {cost && <span className="plugin-desc">{cost}</span>}
        <button className="btn btn-sm btn-ghost" onClick={() => void reload()} disabled={loading} title="Reload from the session file">↻</button>
      </div>
      {error && <p className="error-text">{error}</p>}
      <div className="cc-scroll" ref={scroller}>
        {msgs.length === 0 && !stream && <p className="field-hint team-empty">{loading ? 'Loading…' : `Talk to ${AGENT_LABEL[state?.agent ?? 'claude']} on ${serverName}${(state?.agent ?? 'claude') === 'claude' ? '. Same session as the terminal; pick one from the menu above or start fresh.' : '. History for this agent shows only what happened in this window.'}`}</p>}
        {msgs.map((m, i) => (
          <article key={i} className={`msg ${m.role === 'user' ? 'msg-user' : 'msg-agent'}`}>
            {m.blocks.map((b, j) =>
              b.type === 'text' ? (
                m.role === 'user' ? (
                  <div key={j} className="msg-body">{b.text}</div>
                ) : (
                  <div key={j} className="msg-body is-md team-md" dangerouslySetInnerHTML={{ __html: renderMarkdown(b.text ?? '') }} />
                )
              ) : (
                <ToolRow key={j} b={b} />
              )
            )}
            <div className="msg-meta">{m.role === 'user' ? 'You' : AGENT_LABEL[state?.agent ?? 'claude']}{m.atMs ? ` · ${timeOf(m.atMs)}` : ''}</div>
          </article>
        ))}
        {stream && (
          <article className="msg msg-agent is-streaming">
            <div className={`msg-body${stream.text ? ' is-md team-md' : ''}`}>
              {stream.text ? <span dangerouslySetInnerHTML={{ __html: renderMarkdown(stream.text) }} /> : <span className="thinking">working…</span>}
              <span className="caret" />
            </div>
            <div className="msg-meta">{AGENT_LABEL[state?.agent ?? 'claude']} · now</div>
          </article>
        )}
      </div>
      {queue.length > 0 && (
        <div className="queue">
          {queue.map((q, i) => (
            <div key={q.id} className="queue-item" title={q.text}>
              <span className="queue-badge">{i === 0 ? 'Next' : `#${i + 1}`}</span>
              <span className="queue-text">{q.text}</span>
              <button className="btn btn-sm" onClick={() => { setQueue((qq) => [q, ...qq.filter((x) => x.id !== q.id)]); void api.claude.abort(serverId) }} title="Stop the current run and send this now">Send now</button>
              <button className="btn btn-sm btn-ghost" onClick={() => setQueue((qq) => qq.filter((x) => x.id !== q.id))}>✕</button>
            </div>
          ))}
        </div>
      )}
      <div className="team-composer">
        <textarea
          ref={box}
          rows={1}
          value={draft}
          placeholder={isLive ? `Type to the live session on ${serverName} (#12 to discuss an issue)…` : running ? 'Type the next message; it sends when this run ends…' : `Message ${AGENT_LABEL[state?.agent ?? 'claude']} on ${serverName}…`}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
        />
        {isLive && liveWaiting ? (
          <>
            <button className="btn btn-primary btn-send" onClick={submit} disabled={!draft.trim()} title="Sends now; the live session queues input itself">Send</button>
            <button className="btn btn-ghost btn-sm" onClick={() => setLiveWaiting(false)} title="Stop waiting for a reply here (the session keeps working)">not waiting</button>
          </>
        ) : running ? (
          <>
            <button className="btn btn-send" onClick={submit} disabled={!draft.trim()} title="Queue it">Queue</button>
            <button className="btn btn-stop" onClick={() => void api.claude.abort(serverId)} title="Stop this run">■ Stop</button>
          </>
        ) : (
          <button className="btn btn-primary btn-send" onClick={submit} disabled={!draft.trim()}>Send</button>
        )}
      </div>
    </div>
  )
}
