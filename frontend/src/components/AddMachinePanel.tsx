import { useEffect, useState } from 'react'
import { api } from '../api'
import type { JoinCommand, RemoteNode } from '../types'
import { PairingApprovals } from './PairingApprovals'

type Props = { connected: boolean }

/**
 * Enrol another machine, a Linux server say, as a node of the gateway: mint a
 * one-time code, hand over one command to paste there, approve the pairing when it
 * arrives. The script on the other end installs the OpenClaw CLI, writes a
 * read-only exec policy and registers the node host as a service.
 */
export function AddMachinePanel({ connected }: Props): React.JSX.Element {
  const [join, setJoin] = useState<JoinCommand | null>(null)
  const [nodes, setNodes] = useState<RemoteNode[]>([])
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  const refreshNodes = async (): Promise<void> => {
    if (!connected) return
    try {
      const res = await api.rpc.request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>('node.list')
      setNodes(res?.paired ?? res?.nodes ?? [])
    } catch {
      /* keep what we have */
    }
  }

  useEffect(() => {
    void refreshNodes()
    const t = setInterval(() => {
      setTick((n) => n + 1)
      void refreshNodes()
    }, 10_000)
    const off = api.onGatewayEvent(({ event }) => {
      if (event.startsWith('node.') && !event.startsWith('node.invoke')) void refreshNodes()
    })
    return () => {
      clearInterval(t)
      off()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected])

  const mint = async (): Promise<void> => {
    setBusy(true)
    setError(null)
    try {
      setJoin(await api.node.joinCommand())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const copy = async (): Promise<void> => {
    if (!join) return
    await navigator.clipboard.writeText(join.command)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const expired = join ? join.expiresAtMs > 0 && Date.now() > join.expiresAtMs : false
  const secondsLeft = join ? Math.max(0, Math.round((join.expiresAtMs - Date.now()) / 1000)) : 0
  void tick

  const forget = async (n: RemoteNode): Promise<void> => {
    try {
      await api.rpc.request('node.pair.remove', { nodeId: n.nodeId })
    } catch {
      try {
        await api.rpc.request('device.pair.remove', { deviceId: n.nodeId })
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
        return
      }
    }
    void refreshNodes()
  }

  return (
    <>
      <section className="panel">
        <h3>Add a machine</h3>
        <p className="field-hint">
          A Linux server, another Mac, anything with Node.js 22. It becomes a node of the gateway,
          so agents can read logs and run commands there through ClawHQ, with every write asking
          you first. The machine must be able to reach the gateway: on the same tailnet, or the
          gateway published with Tailscale Funnel.
        </p>
        <div className="btn-row">
          <button className="btn btn-primary" disabled={!connected || busy} onClick={() => void mint()}>
            {busy ? 'Minting…' : join ? 'New code' : 'Get the command'}
          </button>
        </div>
        {join && (
          <>
            <p className="field-hint">
              Paste this on the machine as a user with sudo. The code is single-use and{' '}
              {expired ? 'has expired; mint a new one.' : `expires in ${Math.floor(secondsLeft / 60)}m ${secondsLeft % 60}s.`}
            </p>
            <pre className="md-source join-cmd">{join.command}</pre>
            <div className="btn-row">
              <button className="btn btn-sm" onClick={() => void copy()} disabled={expired}>
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            <p className="field-hint">
              The script installs OpenClaw {'{'}the gateway's version{'}'} if it is missing, writes a read-only exec policy
              (cat, tail, journalctl, systemctl, docker and the like run without asking; everything else asks),
              pairs with <code>{join.gatewayUrl}</code>, and installs the node host as a system service.
              Add <code>--allow-writes</code> to the command to skip the allowlist and have every command ask.
            </p>
          </>
        )}
        {error && <p className="error-text">{error}</p>}
      </section>

      <PairingApprovals connected={connected} />

      <section className="panel">
        <h3>Machines</h3>
        <p className="field-hint">Every node paired with this gateway, including ClawHQ desktops.</p>
        {nodes.length === 0 && <p className="field-hint">{connected ? 'No nodes paired.' : 'Connect to a gateway to see its nodes.'}</p>}
        <div className="gw-list">
          {nodes.map((n) => (
            <div key={n.nodeId} className="gw-row">
              <i className={`dot ${n.connected ? 'dot-ok' : 'dot-off'}`} />
              <span className="gw-meta">
                <span className="gw-name">
                  {n.displayName || n.platform || n.nodeId.slice(0, 8)}
                  <span className="plugin-desc"> · {n.platform ?? '?'} · {n.connected ? 'online' : 'offline'}</span>
                </span>
                <span className="gw-url mono">
                  {(n.commands ?? []).includes('screen.snapshot') ? 'desktop · ' : ''}
                  {(n.commands ?? []).length} commands · {n.nodeId.slice(0, 12)}
                </span>
              </span>
              {!n.connected && (
                <button className="btn btn-sm btn-ghost" onClick={() => void forget(n)}>
                  Forget
                </button>
              )}
            </div>
          ))}
        </div>
      </section>
    </>
  )
}
