import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { DesktopView } from './DesktopView'
import type { RemoteNode } from '../types'

type Props = {
  /** Node to show first, from the URL the main window opened this with. */
  initialNodeId: string | null
}

/**
 * The detached desktop viewer: its own native window, so it can sit beside the
 * chat, on a second display, or pinned above everything else.
 *
 * It shares the Go side with the main window, so the gateway connection, events
 * and the node list are the same ones the sidebar uses.
 */
export function DesktopWindow({ initialNodeId }: Props): React.JSX.Element {
  const [desktops, setDesktops] = useState<RemoteNode[]>([])
  const [selected, setSelected] = useState<string | null>(initialNodeId)
  const [pinned, setPinned] = useState(false)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const nodes = await api.rpc.request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>('node.list')
      const list = (nodes?.paired ?? nodes?.nodes ?? []).filter((n) => (n.commands ?? []).includes('screen.snapshot'))
      setDesktops(list)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    api.connection
      .status()
      .then((s) => setConnected(s.phase === 'connected'))
      .catch(() => undefined)
    return api.onConnectionStatus((s) => setConnected(s.phase === 'connected'))
  }, [])

  useEffect(() => {
    if (!connected) return
    void refresh()
    const tick = setInterval(() => void refresh(), 10_000)
    const off = api.onGatewayEvent(({ event }) => {
      if (event.startsWith('node.') && !event.startsWith('node.invoke')) void refresh()
    })
    return () => {
      clearInterval(tick)
      off()
    }
  }, [connected, refresh])

  useEffect(() => api.onDesktopSelect(setSelected), [])

  const sorted = [...desktops].sort((a, b) => Number(b.connected === true) - Number(a.connected === true))
  // Nothing is shown until a desktop is chosen; the list is the front page.
  const node = selected ? sorted.find((d) => d.nodeId === selected) : undefined

  useEffect(() => {
    document.title = node
      ? `${node.displayName || node.platform || 'Desktop'}${node.connected ? '' : ' (offline)'}`
      : 'Desktops'
  }, [node])

  const remove = async (target: RemoteNode): Promise<void> => {
    try {
      await api.rpc.request('node.pair.remove', { nodeId: target.nodeId })
    } catch {
      try {
        await api.rpc.request('device.pair.remove', { deviceId: target.nodeId })
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
        return
      }
    }
    if (selected === target.nodeId) setSelected(null)
    void refresh()
  }

  const togglePin = (): void => {
    const next = !pinned
    setPinned(next)
    void api.window.setDesktopAlwaysOnTop(next)
  }

  return (
    <div className="desk-window">
      <div className="desk-window-bar">
        {node ? (
          <>
            <button className="btn btn-sm btn-ghost" onClick={() => setSelected(null)} title="Back to the list">
              ‹ Desktops
            </button>
            <span className="desk-panel-title">
              {(node.displayName || node.platform || 'desktop') + (node.connected ? '' : ' (offline)')}
            </span>
          </>
        ) : (
          <span className="desk-panel-title">
            {!connected ? 'Not connected to a gateway' : sorted.length === 0 ? 'No desktops paired' : `${sorted.length} desktop${sorted.length === 1 ? '' : 's'}`}
          </span>
        )}
        <span className="desk-panel-spacer" />
        <button
          className={`btn btn-sm${pinned ? ' btn-primary' : ''}`}
          onClick={togglePin}
          title="Keep this window above other apps"
        >
          {pinned ? 'Pinned on top' : 'Pin on top'}
        </button>
      </div>
      {error && <p className="error-text desk-window-error">{error}</p>}
      <div className="desk-panel-body">
        {node ? (
          <DesktopView key={node.nodeId} node={node} compact />
        ) : (
          <div className="desk-list">
            {sorted.length === 0 && (
              <p className="thread-empty">Pair a machine with its node role on and it shows up here.</p>
            )}
            {sorted.map((d) => (
              <div key={d.nodeId} className="gw-row">
                <i className={`dot ${d.connected ? 'dot-ok' : 'dot-off'}`} />
                <span className="gw-meta">
                  <span className="gw-name">{d.displayName || d.platform || 'desktop'}</span>
                  <span className="gw-url mono">
                    {d.platform ?? ''}
                    {d.platform ? ' · ' : ''}
                    {d.connected ? 'online' : 'offline'} · {d.nodeId.slice(0, 12)}
                  </span>
                </span>
                {!d.connected && (
                  <button className="btn btn-sm btn-ghost" title="Forget this desktop" onClick={() => void remove(d)}>
                    Forget
                  </button>
                )}
                <button
                  className={`btn btn-sm${d.connected ? ' btn-primary' : ''}`}
                  disabled={!d.connected}
                  onClick={() => setSelected(d.nodeId)}
                >
                  Connect
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
