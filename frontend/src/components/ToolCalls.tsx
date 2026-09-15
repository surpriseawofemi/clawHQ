import { useState } from 'react'
import type { ChatMessage, ContentBlock } from '../types'

/** A tool call an agent made, paired with its result once that arrived. */
export type ToolCall = {
  id: string
  name: string
  arguments: unknown
  result?: string
  resultIsError?: boolean
}

/** The tool_result blocks live in later messages; index them by call id. */
export function indexToolResults(messages: ChatMessage[]): Map<string, ContentBlock> {
  const map = new Map<string, ContentBlock>()
  for (const m of messages) {
    if (!Array.isArray(m.content)) continue
    for (const b of m.content) {
      if (b.type === 'tool_result' && typeof b.tool_use_id === 'string') map.set(b.tool_use_id, b)
    }
  }
  return map
}

/** Tool calls in one message, with results looked up in the index. */
export function toolCallsOf(msg: ChatMessage, results: Map<string, ContentBlock>): ToolCall[] {
  if (!Array.isArray(msg.content)) return []
  const calls: ToolCall[] = []
  for (const b of msg.content) {
    if (b.type !== 'toolcall' && b.type !== 'tool_use') continue
    const id = typeof b.id === 'string' ? b.id : ''
    const r = id ? results.get(id) : undefined
    calls.push({
      id: id || `${msg.timestamp}-${calls.length}`,
      name: typeof b.name === 'string' ? b.name : 'tool',
      arguments: b.arguments ?? b.input ?? {},
      result: r ? resultText(r) : undefined,
      resultIsError: r?.is_error === true
    })
  }
  return calls
}

function resultText(r: ContentBlock): string {
  const c = r.content
  if (typeof c === 'string') return c
  if (Array.isArray(c)) {
    return (c as ContentBlock[])
      .map((x) => (typeof x.text === 'string' ? x.text : x.type === 'tool_reference' ? `→ ${String(x.tool_name ?? '')}` : `[${x.type}]`))
      .join('\n')
  }
  return ''
}

/** "mcp__openclaw__portal" reads better as "portal". */
export const prettyToolName = (name: string): string => name.replace(/^mcp__[^_]+__/, '').replace(/^mcp__/, '')

/** A one-line gist of the arguments, for the collapsed row. */
function argsGist(args: unknown): string {
  if (!args || typeof args !== 'object') return typeof args === 'string' ? args : ''
  const entries = Object.entries(args as Record<string, unknown>)
  const preferred = ['command', 'query', 'path', 'file_path', 'url', 'action', 'message', 'prompt', 'name']
  const pick = preferred.find((k) => typeof (args as Record<string, unknown>)[k] === 'string')
  const raw = pick ? String((args as Record<string, unknown>)[pick]) : entries.map(([k, v]) => `${k}=${JSON.stringify(v)}`).join(' ')
  return raw.length > 110 ? raw.slice(0, 107) + '…' : raw
}

const clip = (s: string, n: number): string => (s.length > n ? s.slice(0, n - 1) + '…' : s)

type Props = {
  calls: ToolCall[]
}

/** Compact rows under an agent message: what it called, with what, and what came back. */
export function ToolCalls({ calls }: Props): React.JSX.Element | null {
  const [open, setOpen] = useState<Set<string>>(new Set())
  if (calls.length === 0) return null
  const toggle = (id: string): void =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  return (
    <div className="tool-calls">
      {calls.map((c) => {
        const isOpen = open.has(c.id)
        return (
          <div key={c.id} className={`tool-call${c.resultIsError ? ' is-error' : ''}${c.result === undefined ? ' is-pending' : ''}`}>
            <button className="tool-call-row" onClick={() => toggle(c.id)} aria-expanded={isOpen}>
              <span className="tool-call-icon" aria-hidden="true">
                {c.result === undefined ? '◌' : c.resultIsError ? '✕' : '✓'}
              </span>
              <span className="tool-call-name">{prettyToolName(c.name)}</span>
              <span className="tool-call-gist mono">{argsGist(c.arguments)}</span>
              <span className="tool-call-chev" aria-hidden="true">
                {isOpen ? '▾' : '▸'}
              </span>
            </button>
            {isOpen && (
              <div className="tool-call-detail">
                <div className="tool-call-label">arguments</div>
                <pre className="mono">{clip(JSON.stringify(c.arguments, null, 2), 4000)}</pre>
                <div className="tool-call-label">{c.result === undefined ? 'no result recorded' : 'result'}</div>
                {c.result !== undefined && <pre className="mono">{clip(c.result, 6000) || '(empty)'}</pre>}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
