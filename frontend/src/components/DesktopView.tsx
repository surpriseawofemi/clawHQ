import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { RemoteNode } from '../types'

type Props = {
  node: RemoteNode
}

const INTERVALS = [
  { label: 'Manual', ms: 0 },
  { label: '1s', ms: 1000 },
  { label: '2s', ms: 2000 },
  { label: '5s', ms: 5000 }
]

/** Keys that go through as a `key` action rather than typed text. */
const SPECIAL_KEYS: Record<string, string> = {
  Enter: 'Return',
  Backspace: 'backspace',
  Tab: 'tab',
  Escape: 'escape',
  Delete: 'delete',
  Insert: 'insert',
  ArrowUp: 'up',
  ArrowDown: 'down',
  ArrowLeft: 'left',
  ArrowRight: 'right',
  Home: 'home',
  End: 'end',
  PageUp: 'pageup',
  PageDown: 'pagedown'
}

/**
 * Live view of a paired node's desktop, with optional control.
 *
 * Viewing polls `screen.snapshot`; control sends `computer.act` actions in screenshot
 * pixel coordinates, which the node maps back onto its real screen. Every action is a
 * gateway round trip, so this is built for "log into that account for me", not for
 * gaming: typed characters are batched, and a fresh frame is pulled after each action.
 */
export function DesktopView({ node }: Props): React.JSX.Element {
  // `connected` comes from node.list. A paired node that is not connected has nowhere
  // for the gateway to forward the invoke, and the reply is a bare
  // "node.invoke: node not connected" — so say what is going on instead of asking.
  const online = node.connected === true
  const canControl = (node.commands ?? []).includes('computer.act')

  const [src, setSrc] = useState<string | null>(null)
  const [size, setSize] = useState<{ w: number; h: number } | null>(null)
  const [frameId, setFrameId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [intervalMs, setIntervalMs] = useState(0)
  const [lastAt, setLastAt] = useState<number | null>(null)
  const [control, setControl] = useState(false)
  const [typeBox, setTypeBox] = useState('')
  const [actError, setActError] = useState<string | null>(null)

  const imgRef = useRef<HTMLImageElement | null>(null)
  const surfaceRef = useRef<HTMLDivElement | null>(null)
  // Guards against overlapping polls when a capture takes longer than the interval.
  const inFlight = useRef(false)
  const typeBuffer = useRef('')
  const typeTimer = useRef<number | null>(null)
  const clickTimer = useRef<number | null>(null)
  const refreshTimer = useRef<number | null>(null)

  const capture = useCallback(async () => {
    if (inFlight.current || !online) return
    inFlight.current = true
    setLoading(true)
    try {
      const res = await api.rpc.request<{
        payload?: { base64?: string; width?: number; height?: number; displayFrameId?: string }
      }>(
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
      setFrameId(payload.displayFrameId ?? null)
      setLastAt(Date.now())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      inFlight.current = false
      setLoading(false)
    }
  }, [node.nodeId, online])

  // A frame shortly after an action, so the result of a click is visible without
  // waiting for the next poll. Coalesced: ten keystrokes mean one refresh.
  const refreshSoon = useCallback(() => {
    if (refreshTimer.current) window.clearTimeout(refreshTimer.current)
    refreshTimer.current = window.setTimeout(() => void capture(), 350)
  }, [capture])

  // The gateway's cua-computer plugin validates computer.act params against a strict
  // schema (no unknown fields) before forwarding: pointer actions carry x/y, key
  // actions carry `keys`, and screenshot is a separate command entirely.
  const act = useCallback(
    async (params: Record<string, unknown>) => {
      try {
        await api.rpc.request('node.invoke', {
          nodeId: node.nodeId,
          command: 'computer.act',
          params,
          idempotencyKey: `clawhq-act-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
        })
        setActError(null)
        refreshSoon()
      } catch (err) {
        setActError(err instanceof Error ? err.message : String(err))
      }
    },
    [node.nodeId, refreshSoon]
  )

  const flushTyped = useCallback(() => {
    const text = typeBuffer.current
    typeBuffer.current = ''
    typeTimer.current = null
    if (text) void act({ action: 'type', text })
  }, [act])

  const queueTyped = useCallback(
    (text: string) => {
      typeBuffer.current += text
      if (typeTimer.current) window.clearTimeout(typeTimer.current)
      typeTimer.current = window.setTimeout(flushTyped, 250)
    },
    [flushTyped]
  )

  /** Pointer fields for an event on the image: x/y in screenshot pixels plus the frame. */
  const pointOf = (e: React.MouseEvent): Record<string, unknown> | null => {
    const img = imgRef.current
    if (!img || !size) return null
    const rect = img.getBoundingClientRect()
    const x = Math.round(((e.clientX - rect.left) / rect.width) * size.w)
    const y = Math.round(((e.clientY - rect.top) / rect.height) * size.h)
    if (x < 0 || y < 0 || x >= size.w || y >= size.h) return null
    return { x, y, refWidth: size.w, screenIndex: 0, ...(frameId ? { displayFrameId: frameId } : {}) }
  }

  const onClick = (e: React.MouseEvent): void => {
    if (!control) return
    const pt = pointOf(e)
    if (!pt) return
    surfaceRef.current?.focus()
    // Wait briefly for a second click so a double-click is sent as one action rather
    // than two round-trip-separated singles the target would not treat as a pair.
    if (clickTimer.current) {
      window.clearTimeout(clickTimer.current)
      clickTimer.current = null
      void act({ action: 'double_click', ...pt })
      return
    }
    clickTimer.current = window.setTimeout(() => {
      clickTimer.current = null
      void act({ action: 'left_click', ...pt })
    }, 220)
  }

  const onContextMenu = (e: React.MouseEvent): void => {
    e.preventDefault()
    if (!control) return
    const pt = pointOf(e)
    if (pt) void act({ action: 'right_click', ...pt })
  }

  const onWheel = (e: React.WheelEvent): void => {
    if (!control) return
    const pt = pointOf(e)
    if (!pt) return
    e.preventDefault()
    const vertical = Math.abs(e.deltaY) >= Math.abs(e.deltaX)
    const delta = vertical ? e.deltaY : e.deltaX
    const amount = Math.max(1, Math.min(10, Math.round(Math.abs(delta) / 100)))
    const direction = vertical ? (delta > 0 ? 'down' : 'up') : delta > 0 ? 'right' : 'left'
    void act({ action: 'scroll', ...pt, scrollDirection: direction, scrollAmount: amount })
  }

  const onKeyDown = (e: React.KeyboardEvent): void => {
    if (!control) return
    // Leave the app's own shortcuts alone when they are obviously not meant for the
    // remote machine.
    if (e.metaKey && (e.key === 'q' || e.key === 'w' || e.key === 'h' || e.key === 'm')) return
    e.preventDefault()

    // Cmd on a Mac keyboard means Ctrl on the Windows desktop: cmd+v is paste there.
    const mods: string[] = []
    if (e.ctrlKey || e.metaKey) mods.push('ctrl')
    if (e.altKey) mods.push('alt')
    if (e.shiftKey && (e.key.length !== 1 || mods.length > 0)) mods.push('shift')

    const special = SPECIAL_KEYS[e.key] ?? (/^F\d{1,2}$/.test(e.key) ? e.key.toLowerCase() : null)
    if (special) {
      flushTyped()
      void act({ action: 'key', keys: [...mods, special].join('+') })
      return
    }
    if (e.key.length === 1) {
      if (mods.length > 0) {
        flushTyped()
        void act({ action: 'key', keys: [...mods, e.key.toLowerCase()].join('+') })
      } else {
        queueTyped(e.key)
      }
    }
  }

  const onPaste = (e: React.ClipboardEvent): void => {
    if (!control) return
    const text = e.clipboardData.getData('text')
    if (!text) return
    e.preventDefault()
    flushTyped()
    void act({ action: 'type', text })
  }

  const sendTypeBox = (): void => {
    const text = typeBox
    if (!text) return
    setTypeBox('')
    void act({ action: 'type', text })
  }

  // Capture once when the node changes or comes online, then on the chosen interval.
  useEffect(() => {
    setSrc(null)
    setError(null)
    setActError(null)
    void capture()
  }, [capture])

  useEffect(() => {
    if (intervalMs <= 0 || !online) return
    const t = setInterval(() => void capture(), intervalMs)
    return () => clearInterval(t)
  }, [intervalMs, capture, online])

  // Taking control with no polling would leave you clicking blind between actions.
  const toggleControl = (): void => {
    const next = !control
    setControl(next)
    if (next) {
      if (intervalMs === 0) setIntervalMs(2000)
      surfaceRef.current?.focus()
    } else {
      flushTyped()
    }
  }

  useEffect(() => {
    if (!online || !canControl) setControl(false)
  }, [online, canControl])

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
            {!online ? 'offline' : size ? `${size.w}×${size.h}` : 'no capture yet'}
            {lastAt ? ` · updated ${new Date(lastAt).toLocaleTimeString()}` : ''}
            {control ? ' · you are in control' : ''}
          </p>
        </div>
        <div className="desk-controls">
          {online && canControl && (
            <button
              className={`btn btn-sm${control ? ' btn-danger' : ' btn-primary'}`}
              onClick={toggleControl}
              title="Send your mouse and keyboard to that desktop"
            >
              {control ? 'Release control' : 'Take control'}
            </button>
          )}
          {INTERVALS.map((opt) => (
            <button
              key={opt.ms}
              className={`btn btn-sm${intervalMs === opt.ms ? ' btn-primary' : ''}`}
              onClick={() => setIntervalMs(opt.ms)}
              disabled={!online}
            >
              {opt.label}
            </button>
          ))}
          <button className="btn btn-sm" onClick={() => void capture()} disabled={loading || !online}>
            Refresh
          </button>
        </div>
      </header>

      <div className="desk-stage">
        {!online ? (
          <div className="desk-offline">
            <p className="thread-empty">This desktop is offline.</p>
            <p className="onboard-hint">
              The gateway still has it paired, but nothing is connected under that identity, so
              there is no screen to capture. Start ClawHQ with its node role enabled — or the
              OpenClaw node app — on that machine and this view wakes up on its own.
            </p>
          </div>
        ) : (
          <div
            ref={surfaceRef}
            className={`desk-surface${control ? ' is-control' : ''}`}
            tabIndex={0}
            onClick={onClick}
            onContextMenu={onContextMenu}
            onWheel={onWheel}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
          >
            {error && <p className="error-text">{error}</p>}
            {!error && !src && <p className="thread-empty">Capturing…</p>}
            {src && (
              <img ref={imgRef} className="desk-image" src={src} alt="Remote desktop" draggable={false} />
            )}
          </div>
        )}
      </div>

      {online && control && (
        <footer className="desk-typebar">
          <input
            className="mono"
            placeholder="Type text to send exactly — handy for passwords — then Enter"
            value={typeBox}
            onChange={(e) => setTypeBox(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') sendTypeBox()
            }}
          />
          <button className="btn btn-sm" onClick={sendTypeBox} disabled={!typeBox}>
            Send text
          </button>
          <button className="btn btn-sm" onClick={() => void act({ action: 'key', keys: 'Return' })}>
            ⏎ Enter
          </button>
          <button className="btn btn-sm" onClick={() => void act({ action: 'key', keys: 'tab' })}>
            ⇥ Tab
          </button>
          {actError && <span className="error-text">{actError}</span>}
        </footer>
      )}
      {online && !control && canControl && actError && (
        <footer className="desk-typebar">
          <span className="error-text">{actError}</span>
        </footer>
      )}
    </main>
  )
}
