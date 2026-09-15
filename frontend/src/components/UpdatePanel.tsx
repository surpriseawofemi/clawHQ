import { useEffect, useState } from 'react'
import { api } from '../api'
import type { UpdateStatus } from '../types'

const ago = (ms: number): string => {
  if (!ms) return 'not yet'
  const diff = Date.now() - ms
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`
  return `${Math.round(diff / 3_600_000)}h ago`
}

/**
 * Update status, the manual trigger, and the automatic-install switch.
 *
 * The app checks GitHub on its own every hour. With automatic installs on, a newer
 * release is downloaded, swapped in and relaunched without anyone clicking; off,
 * the panel shows what is available and waits for the Install button.
 */
export function UpdatePanel(): React.JSX.Element {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [busy, setBusy] = useState<'check' | 'install' | 'auto' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)

  useEffect(() => {
    api.update
      .status()
      .then(setStatus)
      .catch(() => undefined)
  }, [])

  const install = async (): Promise<void> => {
    setBusy('install')
    setError(null)
    try {
      // The app relaunches itself on success, so there is no post-state to render.
      await api.update.install()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setBusy(null)
    }
  }

  const check = async (): Promise<void> => {
    setBusy('check')
    setError(null)
    setNote(null)
    try {
      const next = await api.update.check()
      setStatus(next)
      if (!next.available) {
        setNote('You are on the latest version.')
      } else if (next.autoUpdate) {
        // The switch says install without asking, so a manual check does too.
        setNote(`Installing ${next.latestVersion}…`)
        await install()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy((b) => (b === 'check' ? null : b))
    }
  }

  const setAuto = async (on: boolean): Promise<void> => {
    setBusy('auto')
    setError(null)
    try {
      setStatus(await api.update.setAutoUpdate(on))
      if (on) setNote('Updates now install by themselves. Checking now…')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className={`panel${status?.available ? ' panel-pending' : ''}`}>
      <h3>Updates</h3>

      <dl className="kv">
        <dt>Version</dt>
        <dd className="mono">{status?.currentVersion || 'unknown'}</dd>
        {status?.available && (
          <>
            <dt>Available</dt>
            <dd className="mono">{status.latestVersion}</dd>
          </>
        )}
        <dt>Last check</dt>
        <dd>{ago(status?.lastCheckedAtMs ?? 0)}</dd>
      </dl>

      {status?.available && status.notes && (
        <details className="onboard-manual">
          <summary>What&apos;s new in {status.latestVersion}</summary>
          <p className="field-hint release-notes">{status.notes}</p>
        </details>
      )}

      <label className="check-row">
        <input
          type="checkbox"
          checked={status?.autoUpdate ?? true}
          disabled={busy !== null}
          onChange={(e) => void setAuto(e.target.checked)}
        />
        <span>Download and install updates automatically</span>
      </label>

      <div className="btn-row">
        <button className="btn" onClick={() => void check()} disabled={busy !== null}>
          {busy === 'check' ? 'Checking…' : 'Check now'}
        </button>
        {status?.available && (
          <button className="btn btn-primary" onClick={() => void install()} disabled={busy !== null}>
            {busy === 'install' ? 'Installing…' : `Install ${status.latestVersion} and restart`}
          </button>
        )}
      </div>

      <p className="field-hint">
        Checked every hour. Installing replaces the app in place and relaunches it; with
        the switch on that happens as soon as a newer release is found.
      </p>

      {error && <p className="error-text">{error}</p>}
      {note && <p className="note-text">{note}</p>}
    </section>
  )
}
