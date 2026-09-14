import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { RemoteNode } from '../types'

type Props = {
  node: RemoteNode
}

const INTERVALS = [
  { label: 'Manual', ms: 0 },
  { label: '2s', ms: 2000 },
  { label: '5s', ms: 5000 },
  { label: '15s', ms: 15000 }
]

/**
 * Live view of a paired node's desktop.
 *
 * Snapshot-polled rather than streamed: `screen.snapshot` is a request/response command,
 * so there is no video channel to attach to. The interval is the user's call because
 * every frame is a full PNG over the gateway.
 */
export function DesktopView({ node }: Props): React.JSX.Element {
  const [src, setSrc] = useState<string | null>(null)
  const [size, setSize] = useState<{ w: number; h: number } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [intervalMs, setIntervalMs] = useState(0)
  const [lastAt, setLastAt] = useState<number | null>(null)

  // Guards against overlapping polls when a capture takes longer than the interval.
  const inFlight = useRef(false)

  const capture = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    setLoading(true)
    try {
      const res = await api.rpc.request<{ payload?: { base64?: string; width?: number; height?: number } }>(
        'node.invoke',
        {
          nodeId: node.nodeId,
          command: 'screen.snapshot',
          params: { screenIndex: 0, maxWidth: 1600 },
          idempotencyKey: `clawhq-${Date.now()}`
        }
      )
      const payload = res?.payload
      if (!payload?.base64) throw new Error('the node returned no image')
      setSrc(`data:image/png;base64,${payload.base64}`)
      if (payload.width && payload.height) setSize({ w: payload.width, h: payload.height })
      setLastAt(Date.now())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      inFlight.current = false
      setLoading(false)
    }
  }, [node.nodeId])

  // Capture once when the node changes, then on the chosen interval.
  useEffect(() => {
    setSrc(null)
    setError(null)
    void capture()
  }, [capture])

  useEffect(() => {
    if (intervalMs <= 0) return
    const t = setInterval(() => void capture(), intervalMs)
    return () => clearInterval(t)
  }, [intervalMs, capture])

  return (
    <main className="chat">
      <header className="chat-head">
        <span className="chat-avatar">🖥️</span>
        <div className="chat-title">
          <h1>
            {node.displayName || node.platform || 'Desktop'}
            {loading && <i className="dot-active" title="Capturing" />}
          </h1>
          <p>
            {size ? `${size.w}×${size.h}` : 'no capture yet'}
            {lastAt ? ` · updated ${new Date(lastAt).toLocaleTimeString()}` : ''}
          </p>
        </div>
        <div className="desk-controls">
          {INTERVALS.map((opt) => (
            <button
              key={opt.ms}
              className={`btn btn-sm${intervalMs === opt.ms ? ' btn-primary' : ''}`}
              onClick={() => setIntervalMs(opt.ms)}
            >
              {opt.label}
            </button>
          ))}
          <button className="btn btn-sm" onClick={() => void capture()} disabled={loading}>
            Refresh
          </button>
        </div>
      </header>

      <div className="desk-stage">
        {error && <p className="error-text">{error}</p>}
        {!error && !src && <p className="thread-empty">Capturing…</p>}
        {src && <img className="desk-image" src={src} alt="Remote desktop" />}
      </div>
    </main>
  )
}
