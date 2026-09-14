import { useEffect, useState } from 'react'
import { api } from '../api'
import { Logo } from './Logo'
import type { ConnectionStatus } from '../types'

type Props = {
  status: ConnectionStatus
  onConnected: () => void
}

/**
 * First-run connection.
 *
 * The gateway grants scopes to a signed device identity, not to a bare credential, so
 * ClawHQ has to pair once. Either credential does it: the gateway's shared token, or
 * a one-time setup code. When the local OpenClaw CLI is present we can mint a code
 * ourselves and make it a single click.
 */
export function Onboarding({ status, onConnected }: Props): React.JSX.Element {
  const [cliReady, setCliReady] = useState<boolean | null>(null)
  const [mode, setMode] = useState<'token' | 'setupCode'>('token')
  const [form, setForm] = useState({ name: 'OpenClaw', url: 'ws://127.0.0.1:18789', token: '' })
  const [setupCode, setSetupCode] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

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
          <p className="onboard-sub">
            The gateway has this device. An operator needs to approve it.
          </p>
          <p className="onboard-hint">
            Run <code>openclaw devices approve {status.deviceId?.slice(0, 12)}…</code> on the
            gateway host, or approve it in the Control UI under Devices. ClawHQ connects
            by itself once that happens.
          </p>
          <button className="btn" onClick={() => api.connection.cancelApprovalWait()}>
            Stop waiting
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="onboarding">
      <div className="onboard-card">
        <Logo size={64} className="onboard-mark" />
        <h1>ClawHQ</h1>
        <p className="onboard-sub">One desk for your whole agent org.</p>

        {cliReady === true && (
          <>
            <button
              className="btn btn-primary btn-lg"
              disabled={busy !== null}
              onClick={() =>
                run('auto', async () => {
                  const { setupCode: code } = await api.daemon.mintSetupCode()
                  return await api.connection.pairWithSetupCode('Local OpenClaw', code)
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

        <details className="onboard-manual" open={cliReady === false}>
          <summary>Connect to a gateway</summary>

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
                    api.connection.connectWithToken('', form.name, form.url.trim(), form.token.trim())
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
                onClick={() => run('code', () => api.connection.pairWithSetupCode(form.name, setupCode))}
              >
                {busy === 'code' ? 'Pairing…' : 'Pair'}
              </button>
            </>
          )}
        </details>

        {error && <p className="error-text">{error}</p>}
      </div>
    </div>
  )
}
