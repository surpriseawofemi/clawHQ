import { useEffect, useState } from 'react'
import { api } from '../api'
import type { UpdateStatus } from '../types'

/**
 * Update status and the manual trigger.
 *
 * The app also polls GitHub on its own every six hours; this panel exists so the
 * running version is visible and an update can be taken immediately rather than
 * whenever the timer next fires.
 */
export function UpdatePanel(): React.JSX.Element {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [busy, setBusy] = useState<'check' | 'install' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)

  useEffect(() => {
    api.update
      .status()
      .then(setStatus)
      .catch(() => undefined)
  }, [])

  const check = async (): Promise<void> => {
    setBusy('check')
    setError(null)
    setNote(null)
    try {
      const next = await api.update.check()
      setStatus(next)
      if (!next.available) setNote('You are on the latest version.')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

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
      </dl>

      {status?.available && status.notes && (
        <details className="onboard-manual">
          <summary>What&apos;s new in {status.latestVersion}</summary>
          <p className="field-hint release-notes">{status.notes}</p>
        </details>
      )}

      <div className="btn-row">
        <button className="btn" onClick={check} disabled={busy !== null}>
          {busy === 'check' ? 'Checking…' : 'Check now'}
        </button>
        {status?.available && (
          <button className="btn btn-primary" onClick={install} disabled={busy !== null}>
            {busy === 'install' ? 'Installing…' : `Install ${status.latestVersion} and restart`}
          </button>
        )}
      </div>

      <p className="field-hint">
        Checked automatically every six hours. Installing replaces the app in place and
        relaunches it.
      </p>

      {error && <p className="error-text">{error}</p>}
      {note && <p className="note-text">{note}</p>}
    </section>
  )
}
