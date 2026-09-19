import { useEffect, useState } from 'react'
import { TrashIcon } from './icons'
import { api } from '../api'
import type { ConnectionStatus, GatewayProfile } from '../types'

type Props = {
  status: ConnectionStatus
  onConnected: () => void
}

type Mode = 'token' | 'setupCode'

const since = (ms?: number): string => {
  if (!ms) return ''
  const secs = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (secs < 60) return `${secs}s`
  return `${Math.floor(secs / 60)}m ${secs % 60}s`
}

/**
 * Connection settings: a list of saved gateways plus a form to add one.
 *
 * ClawHQ never learns how packets reach the gateway — an SSH tunnel, a Cloudflare
 * tunnel on a domain, a tailnet address and a plain LAN bind all differ only in the
 * URL typed here.
 */
export function ConnectionPanel({ status, onConnected }: Props): React.JSX.Element {
  const [gateways, setGateways] = useState<GatewayProfile[]>([])
  const [mode, setMode] = useState<Mode>('token')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [, forceTick] = useState(0)

  const [form, setForm] = useState({ name: '', url: 'ws://127.0.0.1:18789', token: '' })
  const [setupCode, setSetupCode] = useState('')

  const refresh = (): void => {
    api.connection
      .gateways()
      .then(setGateways)
      .catch(() => undefined)
  }

  useEffect(refresh, [status.phase])

  // Keep the "waiting 45s" counter moving while an approval is outstanding.
  useEffect(() => {
    if (status.phase !== 'pending') return
    const t = setInterval(() => forceTick((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [status.phase])

  const run = async (label: string, fn: () => Promise<void>): Promise<void> => {
    setBusy(label)
    setError(null)
    setNote(null)
    try {
      await fn()
      refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const addGateway = (): Promise<void> =>
    run('add', async () => {
      const next =
        mode === 'token'
          ? await api.connection.connectWithToken('', form.name.trim(), form.url.trim(), form.token.trim())
          : await api.connection.pairWithSetupCode(form.name.trim(), setupCode)

      if (next.phase === 'connected') {
        setNote('Connected.')
        setForm({ ...form, token: '' })
        setSetupCode('')
        onConnected()
      } else if (next.phase === 'pending') {
        setNote('Waiting for an operator to approve this device.')
      }
    })

  const connectSaved = (id: string): Promise<void> =>
    run(`connect-${id}`, async () => {
      const next = await api.connection.connect(id)
      if (next.phase === 'connected') onConnected()
    })

  return (
    <>
      {status.phase === 'pending' && (
        <section className="panel panel-pending">
          <h3>Waiting for approval</h3>
          <p className="field-hint">
            The gateway has this device but an operator has to approve it before any
            scopes are granted. Approve it on the gateway host, then this connects on its
            own — no need to come back here.
          </p>
          <dl className="kv">
            <dt>Device</dt>
            <dd className="mono">{status.deviceId?.slice(0, 24) ?? 'unknown'}</dd>
            <dt>Waiting</dt>
            <dd>{since(status.waitingSinceMs)}</dd>
          </dl>
          <p className="field-hint">
            Approve with <code>openclaw devices approve {status.deviceId?.slice(0, 12)}…</code>{' '}
            or in the Control UI under Devices.
          </p>
          <div className="btn-row">
            <button
              className="btn"
              onClick={() => run('cancel', () => api.connection.cancelApprovalWait())}
            >
              Stop waiting
            </button>
          </div>
        </section>
      )}

      <section className="panel">
        <h3>Gateways</h3>
        {gateways.length === 0 && <p className="field-hint">No gateways saved yet.</p>}

        <div className="gw-list">
          {gateways.map((g) => {
            const active = g.id === status.gatewayId && status.phase === 'connected'
            return (
              <div key={g.id} className={`gw-row${active ? ' is-active' : ''}`}>
                <i className={`dot ${active ? 'dot-ok' : 'dot-off'}`} />
                <span className="gw-meta">
                  <span className="gw-name">{g.name}</span>
                  <span className="gw-url mono">{g.url}</span>
                </span>
                <button
                  className="btn btn-sm"
                  disabled={busy !== null || active}
                  onClick={() => connectSaved(g.id)}
                >
                  {active ? 'Connected' : busy === `connect-${g.id}` ? 'Connecting…' : 'Connect'}
                </button>
                <button
                  className="icon-btn"
                  title="Remove gateway"
                  onClick={() => run(`rm-${g.id}`, async () => void (await api.connection.removeGateway(g.id)))}
                ><TrashIcon /></button>
              </div>
            )
          })}
        </div>

        {status.phase === 'connected' && (
          <dl className="kv">
            <dt>Server</dt>
            <dd>{status.serverVersion ?? 'unknown'}</dd>
            <dt>Scopes</dt>
            <dd>{status.scopes.length ? status.scopes.join(', ') : 'none granted'}</dd>
            <dt>Device</dt>
            <dd className="mono">{status.deviceId?.slice(0, 24) ?? 'unpaired'}</dd>
          </dl>
        )}
      </section>

      <section className="panel">
        <h3>Add a gateway</h3>
        <p className="field-hint">
          Pair once with the gateway&apos;s shared token. ClawHQ is issued its own device
          token in exchange, so the shared token is not stored and is never needed again.
        </p>

        <div className="tabs tabs-inline">
          <button
            className={`tab${mode === 'token' ? ' is-active' : ''}`}
            onClick={() => setMode('token')}
          >
            URL + token
          </button>
          <button
            className={`tab${mode === 'setupCode' ? ' is-active' : ''}`}
            onClick={() => setMode('setupCode')}
          >
            Setup code
          </button>
        </div>

        <label className="field">
          <span>Name</span>
          <input
            value={form.name}
            placeholder="Home Mac, work box, …"
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>

        {mode === 'token' ? (
          <>
            <label className="field">
              <span>Gateway URL</span>
              <input
                className="mono"
                value={form.url}
                placeholder="wss://openclaw.example.com"
                onChange={(e) => setForm({ ...form, url: e.target.value })}
              />
            </label>
            <label className="field">
              <span>Shared token</span>
              <input
                className="mono"
                type="password"
                value={form.token}
                placeholder="gateway.auth.token"
                onChange={(e) => setForm({ ...form, token: e.target.value })}
              />
            </label>
          </>
        ) : (
          <label className="field">
            <span>Setup code</span>
            <textarea
              rows={3}
              className="mono"
              value={setupCode}
              placeholder="from `openclaw qr` — carries its own URL"
              onChange={(e) => setSetupCode(e.target.value)}
            />
          </label>
        )}

        <div className="btn-row">
          <button
            className="btn btn-primary"
            disabled={
              busy !== null ||
              (mode === 'token' ? !form.url.trim() || !form.token.trim() : !setupCode.trim())
            }
            onClick={addGateway}
          >
            {busy === 'add' ? 'Connecting…' : 'Connect'}
          </button>
        </div>

        {error && <p className="error-text">{error}</p>}
        {note && <p className="note-text">{note}</p>}
      </section>
    </>
  )
}
