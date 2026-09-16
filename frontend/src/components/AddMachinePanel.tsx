import { useEffect, useState } from 'react'
import { api } from '../api'
import type { JoinCommand, RemoteNode } from '../types'
import { PairingApprovals } from './PairingApprovals'
import { readMode, setMode, readTrust, hostKind, hostGlyph, hostLabel, TRUST_LABEL, type MachineMode, type TrustMode } from '../state/execPolicy'

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

  const [modes, setModes] = useState<Record<string, MachineMode | 'custom' | 'unknown'>>({})
  const [trust, setTrust] = useState<Record<string, TrustMode>>({})
  const [modeBusy, setModeBusy] = useState<string | null>(null)

  const refreshNodes = async (): Promise<void> => {
    if (!connected) return
    try {
      const res = await api.rpc.request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>('node.list')
      const list = res?.paired ?? res?.nodes ?? []
      setNodes(list)
      // Each connected node reports its own policy file. OpenClaw node hosts get a
      // mode read out of it; ClawHQ desktops have their own Trust page, so only
      // their setting is shown.
      const online = list.filter((n) => n.connected)
      const hosts = online.filter((n) => hostKind(n) === 'openclaw')
      const desktops = online.filter((n) => hostKind(n) === 'clawhq')
      const [m, t] = await Promise.all([
        Promise.all(hosts.map(async (n) => [n.nodeId, await readMode(n.nodeId)] as const)),
        Promise.all(desktops.map(async (n) => [n.nodeId, await readTrust(n.nodeId)] as const)),
      ])
      setModes((prev) => ({ ...prev, ...Object.fromEntries(m) }))
      setTrust((prev) => ({ ...prev, ...Object.fromEntries(t) }))
    } catch {
      /* keep what we have */
    }
  }

  const changeMode = async (nodeId: string, mode: MachineMode): Promise<void> => {
    setModeBusy(nodeId)
    setError(null)
    try {
      await setMode(nodeId, mode)
      setModes((prev) => ({ ...prev, [nodeId]: mode }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setModeBusy(null)
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

  // The address the new machine will dial. A ClawHQ on the gateway host talks to it
  // over loopback, which no other machine can use, so the field is prefilled from any
  // saved gateway with a real address and can be typed over.
  const [address, setAddress] = useState('')
  useEffect(() => {
    api.connection
      .gateways()
      .then((list) => {
        const real = list.find((g) => !/^(wss?|https?):\/\/(127\.0\.0\.1|localhost|\[?::1\]?|0\.0\.0\.0)(:|\/|$)/i.test(g.url))
        if (real && !address) setAddress(real.url)
      })
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const mint = async (): Promise<void> => {
    setBusy(true)
    setError(null)
    try {
      setJoin(await api.node.joinCommand(address.trim()))
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
          A Linux server, another Mac, anything with Node.js 24. It becomes a node of the gateway,
          so agents can read logs and run commands there through ClawHQ, in the mode you choose:
          auto, semi-auto or manual. The machine must be able to reach the gateway: on the same tailnet, or the
          gateway published with Tailscale Funnel.
        </p>
        <div className="field">
          <span>Gateway address the machine will dial</span>
          <input
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            placeholder="wss://gateway-host.tailnet.ts.net"
            spellCheck={false}
          />
          <p className="field-hint">
            Not a loopback address: the machine dials this from where it is. On a tailnet it is the gateway host's
            Tailscale name, <code>wss://&lt;host&gt;.&lt;tailnet&gt;.ts.net</code>.
          </p>
        </div>
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
              The script needs Node.js 24 (it tells you how to install it if missing), installs the OpenClaw CLI at the
              gateway's version, pairs with <code>{join.gatewayUrl}</code>, and installs the node host as a system service.
              It starts the machine in semi-auto mode: reads run, writes ask. Add <code>--mode auto</code> for a machine
              agents may change freely, or <code>--mode manual</code> to have everything ask. The mode can be changed here
              at any time once the machine is online.
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
                  <span className="host-glyph" title={hostLabel(hostKind(n))}>
                    {hostGlyph(hostKind(n))}
                  </span>{' '}
                  {n.displayName || n.platform || n.nodeId.slice(0, 8)}
                  <span className="plugin-desc"> · {n.platform ?? '?'} · {n.connected ? 'online' : 'offline'}</span>
                </span>
                <span className="gw-url mono">
                  {hostLabel(hostKind(n))}
                  {n.version ? ` ${n.version}` : ''} · {(n.commands ?? []).length} commands · {n.nodeId.slice(0, 12)}
                </span>
              </span>
              {n.connected && hostKind(n) === 'clawhq' && (
                <span className="plugin-desc" title="Change it under Settings → Trust on that ClawHQ">
                  {TRUST_LABEL[trust[n.nodeId] ?? 'unknown']}
                </span>
              )}
              {n.connected && hostKind(n) === 'openclaw' && (
                <select
                  value={modes[n.nodeId] === 'semi' || modes[n.nodeId] === 'auto' || modes[n.nodeId] === 'manual' ? modes[n.nodeId] : ''}
                  onChange={(e) => void changeMode(n.nodeId, e.target.value as MachineMode)}
                  disabled={modeBusy === n.nodeId}
                  aria-label="Exec mode"
                  title="How commands from agents are handled on this machine"
                >
                  {modes[n.nodeId] === 'custom' && <option value="">Custom</option>}
                  {modes[n.nodeId] === 'unknown' && <option value="">Mode unknown</option>}
                  <option value="auto">Auto: run anything</option>
                  <option value="semi">Semi-auto: reads run, writes ask</option>
                  <option value="manual">Manual: everything asks</option>
                </select>
              )}
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
