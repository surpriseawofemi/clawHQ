import { useEffect, useState } from 'react'
import { api } from '../api'
import { Logo } from './Logo'
import type { ConnectionStatus, GatewayProfile } from '../types'

type Props = {
  /** Lets the person into ClawHQ with no gateway: Servers work over SSH alone. */
  onSkip?: () => void
  status: ConnectionStatus
  onConnected: () => void
}

const lastUsed = (ms?: number): string => {
  if (!ms) return 'never connected'
  const diff = Date.now() - ms
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return `${Math.round(diff / 86_400_000)}d ago`
}

/**
 * The screen before a connection lands.
 *
 * With saved gateways it is a picker: the last used one is being connected to in the
 * background, the rest are one click away, and adding another is a button. Without
 * any, it is the first-run pairing form. Pairing is device-based, so each gateway is
 * paired once with its shared token or a setup code and reconnects by itself after.
 */
export function Onboarding({ status, onConnected, onSkip }: Props): React.JSX.Element {
  const [gateways, setGateways] = useState<GatewayProfile[] | null>(null)
  const [showAdd, setShowAdd] = useState(false)
  const [cliReady, setCliReady] = useState<boolean | null>(null)
  const [mode, setMode] = useState<'token' | 'setupCode'>('token')
  const [form, setForm] = useState({ name: '', url: 'ws://127.0.0.1:18789', token: '' })
  const [setupCode, setSetupCode] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.connection
      .gateways()
      .then((list) => setGateways([...list].sort((a, b) => (b.lastConnectedAtMs ?? 0) - (a.lastConnectedAtMs ?? 0))))
      .catch(() => setGateways([]))
  }, [status.phase, status.gatewayId])

  useEffect(() => {
    api.daemon
      .cliAvailable()
      .then(setCliReady)
      .catch(() => setCliReady(false))
  }, [])

  const run = async (label: string, fn: () => Promise<ConnectionStatus>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      const next = await fn()
      if (next.phase === 'connected') onConnected()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  if (status.phase === 'pending') {
    return (
      <div className="onboarding">
        <div className="onboard-card">
          <Logo size={56} className="onboard-mark" />
          <h1>Waiting for approval</h1>
          <p className="onboard-sub">The gateway has this device. An operator needs to approve it.</p>
          <p className="onboard-hint">
            Run <code>openclaw devices approve {status.deviceId?.slice(0, 12)}…</code> on the gateway
            host, or approve it in the Control UI under Devices. ClawHQ connects by itself once
            that happens.
          </p>
          {status.error?.startsWith('still waiting') && <p className="onboard-hint">{status.error}</p>}
          <button className="btn" onClick={() => api.connection.cancelApprovalWait()}>
            Stop waiting
          </button>
        </div>
      </div>
    )
  }

  const saved = gateways ?? []
  const active = saved.find((g) => g.id === status.gatewayId) ?? saved[0]
  const connecting = status.phase === 'connecting'

  if (saved.length > 0 && !showAdd) {
    return (
      <div className="onboarding">
        <div className="onboard-card onboard-wide">
          <Logo size={48} className="onboard-mark" />
          <h1>{connecting && active ? `Connecting to ${active.name}…` : 'Choose a gateway'}</h1>
          <p className="onboard-sub">
            {connecting
              ? 'This keeps retrying in the background. Pick another one if it is down.'
              : active
                ? `${active.name} is not answering right now.`
                : 'Pick where your agents live.'}
          </p>

          <div className="gw-list onboard-list">
            {saved.map((g) => {
              const isActive = g.id === status.gatewayId
              const rowBusy = busy === `connect-${g.id}` || (isActive && connecting)
              return (
                <div
                  key={g.id}
                  className={`gw-row${isActive ? ' is-active' : ''}`}
                  role="button"
                  tabIndex={0}
                  onClick={() => !rowBusy && run(`connect-${g.id}`, () => api.connection.connect(g.id))}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void run(`connect-${g.id}`, () => api.connection.connect(g.id))
                  }}
                >
                  <i className={`dot ${rowBusy ? 'dot-warn' : 'dot-off'}`} />
                  <span className="gw-meta">
                    <span className="gw-name">{g.name}</span>
                    <span className="gw-url mono">
                      {g.url} · {lastUsed(g.lastConnectedAtMs)}
                    </span>
                  </span>
                  <button className="btn btn-sm" disabled={busy !== null || rowBusy}>
                    {rowBusy ? 'Connecting…' : 'Connect'}
                  </button>
                </div>
              )
            })}
          </div>

          {onSkip && (
            <div className="onboard-skip">
              <button className="btn" onClick={onSkip}>
                Continue without a gateway
              </button>
              <span className="field-hint">Servers, files and terminals work over SSH on their own. Agents, Team Chat and Issues need the gateway.</span>
            </div>
          )}
          {status.phase === 'error' && status.error && !error && (
            <p className="onboard-hint">{status.error}</p>
          )}
          {error && <p className="error-text">{error}</p>}

          <div className="btn-row onboard-actions">
            <button className="btn" onClick={() => setShowAdd(true)}>
              Add a gateway
            </button>
          </div>
          <p className="onboard-hint">
            Auto-connect to the last used gateway can be switched off in Settings → Gateways.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="onboarding">
      <div className="onboard-card">
        <Logo size={64} className="onboard-mark" />
        <h1>{saved.length > 0 ? 'Add a gateway' : 'ClawHQ'}</h1>
        <p className="onboard-sub">
          {saved.length > 0 ? 'Pair once; it reconnects by itself after.' : 'One desk for your whole agent org.'}
        </p>

        {cliReady === true && (
          <>
            <button
              className="btn btn-primary btn-lg"
              disabled={busy !== null}
              onClick={() =>
                run('auto', async () => {
                  const { setupCode: code } = await api.daemon.mintSetupCode()
                  return await api.connection.pairWithSetupCode(form.name.trim() || 'Local OpenClaw', code)
                })
              }
            >
              {busy === 'auto' ? 'Connecting…' : 'Connect to local OpenClaw'}
            </button>
            <p className="onboard-hint">
              Uses the <code>openclaw</code> CLI on this machine.
            </p>
          </>
        )}

        <details className="onboard-manual" open={cliReady === false || saved.length > 0}>
          <summary>Connect to a gateway</summary>

          <input
            placeholder="Name, e.g. Office server"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />

          <div className="tabs tabs-inline">
            <button className={`tab${mode === 'token' ? ' is-active' : ''}`} onClick={() => setMode('token')}>
              URL + token
            </button>
            <button
              className={`tab${mode === 'setupCode' ? ' is-active' : ''}`}
              onClick={() => setMode('setupCode')}
            >
              Setup code
            </button>
          </div>

          {mode === 'token' ? (
            <>
              <input
                className="mono"
                value={form.url}
                placeholder="wss://openclaw.example.com"
                onChange={(e) => setForm({ ...form, url: e.target.value })}
              />
              <input
                className="mono"
                type="password"
                value={form.token}
                placeholder="shared gateway token"
                onChange={(e) => setForm({ ...form, token: e.target.value })}
              />
              <button
                className="btn"
                disabled={busy !== null || !form.url.trim() || !form.token.trim()}
                onClick={() =>
                  run('token', () =>
                    api.connection.connectWithToken('', form.name.trim(), form.url.trim(), form.token.trim())
                  )
                }
              >
                {busy === 'token' ? 'Connecting…' : 'Connect'}
              </button>
            </>
          ) : (
            <>
              <textarea
                rows={4}
                className="mono"
                placeholder="Paste the setup code from `openclaw qr`"
                value={setupCode}
                onChange={(e) => setSetupCode(e.target.value)}
              />
              <button
                className="btn"
                disabled={busy !== null || !setupCode.trim()}
                onClick={() => run('code', () => api.connection.pairWithSetupCode(form.name.trim(), setupCode))}
              >
                {busy === 'code' ? 'Pairing…' : 'Pair'}
              </button>
            </>
          )}
        </details>

        {saved.length > 0 && (
          <button className="btn btn-ghost" onClick={() => setShowAdd(false)}>
            Back to saved gateways
          </button>
        )}

        {error && <p className="error-text">{error}</p>}
        {!error && status.phase === 'error' && status.error && <p className="error-text">{status.error}</p>}
      </div>
    </div>
  )
}
