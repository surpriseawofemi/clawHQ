import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { Agent, Attachment, ChatMessage, SessionInfo, StreamingReply } from '../types'
import { agentLabel, messageText } from '../types'
import { AgentHeader, type AgentTab } from './AgentHeader'
import { ToolCalls, indexToolResults, toolCallsOf } from './ToolCalls'

type Props = {
  agent: Agent | null
  sessionKey: string | null
  tab: AgentTab
  onTab: (tab: AgentTab) => void
  sessions: SessionInfo[]
  onSelectSession: (key: string | null) => void
  onNewSession: (label?: string) => void
  messages: ChatMessage[]
  stream: StreamingReply | null
  /** History for this thread is being fetched. */
  loadingHistory: boolean
  /** Everything the gateway has for this thread is loaded. */
  historyComplete: boolean
  onLoadFullHistory: () => void
  busy: boolean
  connected: boolean
  onSend: (text: string, paths?: string[]) => void
  onAbort: () => void
  onSettings: () => void
}

const timeOf = (ts?: number): string =>
  ts ? new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''

const sessionTitle = (s: SessionInfo): string => {
  if (s.isMain || s.key.endsWith(':main')) return 'Main thread'
  if (s.label) return s.label
  return s.key.split(':').slice(2).join(':') || s.key
}

const sizeOf = (bytes: number): string =>
  bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(0)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`

const chipIcon = (a: Attachment): string =>
  a.kind === 'folder' ? '📁' : a.kind === 'image' ? '🖼' : a.kind === 'text' ? '📄' : '⚠️'

export function ChatView({
  agent,
  sessionKey,
  tab,
  onTab,
  sessions,
  onSelectSession,
  onNewSession,
  messages,
  stream,
  loadingHistory,
  historyComplete,
  onLoadFullHistory,
  busy,
  connected,
  onSend,
  onAbort,
  onSettings
}: Props): React.JSX.Element {
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [picking, setPicking] = useState(false)
  const scroller = useRef<HTMLDivElement>(null)
  const composer = useRef<HTMLTextAreaElement>(null)

  // Follow the tail as replies stream in.
  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, stream?.text])

  useEffect(() => {
    composer.current?.focus()
    setAttachments([])
  }, [agent?.id, sessionKey])

  const sendable = attachments.filter((a) => a.kind !== 'unsupported')

  const submit = (): void => {
    const text = draft.trim()
    if ((!text && sendable.length === 0) || !connected) return
    onSend(text, sendable.map((a) => a.path))
    setDraft('')
    setAttachments([])
  }

  const pick = async (what: 'files' | 'folder'): Promise<void> => {
    setPicking(true)
    try {
      const picked = what === 'files' ? await api.attachments.pickFiles() : await api.attachments.pickFolder()
      if (picked?.length) {
        setAttachments((prev) => {
          const seen = new Set(prev.map((a) => a.path))
          return [...prev, ...picked.filter((a) => !seen.has(a.path))]
        })
      }
    } catch {
      // A cancelled dialog is not an error worth showing.
    } finally {
      setPicking(false)
      composer.current?.focus()
    }
  }

  if (!agent) {
    return (
      <main className="chat chat-empty">
        <div className="empty-card">
          <span className="empty-emoji">🦞</span>
          <h2>Pick an agent</h2>
          <p>Choose someone from the left to open the conversation.</p>
        </div>
      </main>
    )
  }

  const visible = messages.filter((m) => m.role === 'user' || m.role === 'assistant')
  const toolResults = indexToolResults(messages)
  const placeholder = !connected
    ? 'Not connected to a gateway'
    : sendable.length > 0
      ? 'Add a note about the files, or just press Enter to send them…'
      : `Message ${agentLabel(agent)}…`

  return (
    <main className="chat">
      <AgentHeader agent={agent} tab={tab} onTab={onTab} working={!!stream}>
        <div className="session-bar">
          <select
            value={sessionKey ?? ''}
            disabled={!connected}
            title="Which conversation with this agent"
            onChange={(e) => onSelectSession(e.target.value.endsWith(':main') ? null : e.target.value)}
          >
            {sessions.map((s) => (
              <option key={s.key} value={s.key}>
                {sessionTitle(s)}
              </option>
            ))}
          </select>
          <button
            className="btn btn-sm"
            disabled={!connected}
            title="Start a fresh thread with this agent"
            onClick={() => onNewSession()}
          >
            ＋ New session
          </button>
        </div>
        <button className="icon-btn" onClick={onSettings} title="Agent settings">
          ⚙
        </button>
      </AgentHeader>

      <div className="messages" ref={scroller}>
        {loadingHistory && visible.length === 0 && (
          <p className="thread-empty thread-loading">
            <i className="spinner" /> Loading messages…
          </p>
        )}
        {visible.length === 0 && !stream && !loadingHistory && (
          <p className="thread-empty">No messages yet — say hello.</p>
        )}
        {visible.length > 0 && !historyComplete && (
          <div className="thread-more">
            <button className="btn btn-sm btn-ghost" disabled={loadingHistory} onClick={onLoadFullHistory}>
              {loadingHistory ? 'Loading…' : 'Load earlier messages'}
            </button>
          </div>
        )}

        {visible.map((msg, i) => {
          const text = messageText(msg)
          const calls = msg.role === 'assistant' ? toolCallsOf(msg, toolResults) : []
          if (!text.trim() && calls.length === 0) return null
          const mine = msg.role === 'user'
          return (
            <article key={msg.__openclaw?.id ?? `${msg.timestamp}-${i}`} className={`msg ${mine ? 'msg-user' : 'msg-agent'}`}>
              {text.trim() && <div className="msg-body">{text}</div>}
              <ToolCalls calls={calls} />
              <div className="msg-meta">
                {mine ? (msg.__openclaw?.senderName ?? 'You') : agentLabel(agent)}
                {msg.timestamp ? ` · ${timeOf(msg.timestamp)}` : ''}
              </div>
            </article>
          )
        })}

        {stream && (
          <article className="msg msg-agent is-streaming">
            <div className="msg-body">
              {stream.text || <span className="thinking">{stream.phase ?? 'thinking'}…</span>}
              <span className="caret" />
            </div>
            <div className="msg-meta">{agentLabel(agent)} · now</div>
          </article>
        )}
      </div>

      <div className="composer-wrap">
        {attachments.length > 0 && (
          <div className="chips">
            {attachments.map((a) => (
              <span key={a.path} className={`chip${a.kind === 'unsupported' ? ' is-bad' : ''}`} title={a.path}>
                <span>{chipIcon(a)}</span>
                <span className="chip-name">{a.name}</span>
                <span className="chip-note">{a.note ? a.note : sizeOf(a.size)}</span>
                <button
                  title="Remove"
                  onClick={() => setAttachments((prev) => prev.filter((x) => x.path !== a.path))}
                >
                  ✕
                </button>
              </span>
            ))}
          </div>
        )}
        <footer className="composer">
          <div className="attach-row">
            <button
              className="icon-btn"
              title="Attach files — text goes inline, images as attachments"
              disabled={!connected || picking}
              onClick={() => void pick('files')}
            >
              📎
            </button>
            <button
              className="icon-btn"
              title="Attach a folder — its text files are sent so the agent can read them"
              disabled={!connected || picking}
              onClick={() => void pick('folder')}
            >
              📁
            </button>
          </div>
          <textarea
            ref={composer}
            value={draft}
            rows={1}
            placeholder={placeholder}
            disabled={!connected}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              // Enter sends; Shift+Enter is a newline, like every chat app.
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                submit()
              }
            }}
          />
          {stream ? (
            <button className="btn btn-stop" onClick={onAbort} title="Stop this run">
              ■ Stop
            </button>
          ) : (
            <button
              className="btn btn-send"
              onClick={submit}
              disabled={!connected || busy || (!draft.trim() && sendable.length === 0)}
            >
              Send
            </button>
          )}
        </footer>
      </div>
    </main>
  )
}
