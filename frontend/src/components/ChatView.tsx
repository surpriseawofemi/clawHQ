import { useEffect, useRef, useState } from 'react'
import type { Agent, ChatMessage, StreamingReply } from '../types'
import { agentEmoji, agentLabel, messageText } from '../types'

type Props = {
  agent: Agent | null
  messages: ChatMessage[]
  stream: StreamingReply | null
  busy: boolean
  connected: boolean
  onSend: (text: string) => void
  onAbort: () => void
  onSettings: () => void
}

const timeOf = (ts?: number): string =>
  ts ? new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''

export function ChatView({
  agent,
  messages,
  stream,
  busy,
  connected,
  onSend,
  onAbort,
  onSettings
}: Props): React.JSX.Element {
  const [draft, setDraft] = useState('')
  const scroller = useRef<HTMLDivElement>(null)
  const composer = useRef<HTMLTextAreaElement>(null)

  // Follow the tail as replies stream in.
  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, stream?.text])

  useEffect(() => {
    composer.current?.focus()
  }, [agent?.id])

  const submit = (): void => {
    const text = draft.trim()
    if (!text || !connected) return
    onSend(text)
    setDraft('')
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

  return (
    <main className="chat">
      <header className="chat-head">
        <span className="chat-avatar">{agentEmoji(agent)}</span>
        <div className="chat-title">
          <h1>
            {agentLabel(agent)}
            {stream && <i className="dot-active" title="Working" />}
          </h1>
          <p>
            {agent.model?.primary ?? 'default model'}
            {agent.identity?.theme ? ` · ${agent.identity.theme}` : ''}
          </p>
        </div>
        <button className="icon-btn" onClick={onSettings} title="Agent settings">
          ⚙
        </button>
      </header>

      <div className="messages" ref={scroller}>
        {visible.length === 0 && !stream && (
          <p className="thread-empty">No messages yet — say hello.</p>
        )}

        {visible.map((msg, i) => {
          const text = messageText(msg)
          if (!text.trim()) return null
          const mine = msg.role === 'user'
          return (
            <article key={msg.__openclaw?.id ?? `${msg.timestamp}-${i}`} className={`msg ${mine ? 'msg-user' : 'msg-agent'}`}>
              <div className="msg-body">{text}</div>
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

      <footer className="composer">
        <textarea
          ref={composer}
          value={draft}
          rows={1}
          placeholder={connected ? `Message ${agentLabel(agent)}…` : 'Not connected to a gateway'}
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
          <button className="btn btn-send" onClick={submit} disabled={!connected || busy || !draft.trim()}>
            Send
          </button>
        )}
      </footer>
    </main>
  )
}
