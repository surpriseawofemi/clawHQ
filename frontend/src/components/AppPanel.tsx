import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ClawHQConfig, LoginStatus } from '../types'
import { applyTheme, getTheme, type Theme } from '../theme'

type Props = {
  config: ClawHQConfig | null
  onConfigChanged: (config: ClawHQConfig) => void
}

/**
 * How ClawHQ lives on this computer: whether it stays in the menu bar when the
 * window closes (so the node keeps serving) and whether it starts at login.
 */
export function AppPanel({ config, onConfigChanged }: Props): React.JSX.Element {
  const [login, setLogin] = useState<LoginStatus | null>(null)
  const [theme, setTheme] = useState<Theme>(getTheme())
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.login
      .status()
      .then(setLogin)
      .catch(() => undefined)
  }, [])

  const run = async (label: string, fn: () => Promise<void>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      await fn()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="panel">
      <h3>On this computer</h3>
      <div className="field">
        <span>Appearance</span>
        <div className="tabs tabs-inline">
          {(
            [
              ['system', 'Match the system'],
              ['dark', 'Dark'],
              ['light', 'Light']
            ] as [Theme, string][]
          ).map(([value, label]) => (
            <button
              key={value}
              className={`tab${theme === value ? ' is-active' : ''}`}
              onClick={() => {
                applyTheme(value)
                setTheme(value)
              }}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <label className="check-row">
        <input
          type="checkbox"
          checked={config?.menuBar ?? true}
          disabled={busy !== null}
          onChange={(e) => void run('menubar', async () => onConfigChanged(await api.config.setMenuBar(e.target.checked)))}
        />
        <span>Keep running in the menu bar when the window closes</span>
      </label>
      <p className="field-hint">
        The node role, notifications and approvals only work while ClawHQ runs. With this
        on, closing the window hides it; the menu bar icon brings it back, and Quit is in
        its menu.
      </p>
      <label className="check-row">
        <input
          type="checkbox"
          checked={login?.enabled ?? false}
          disabled={busy !== null || !(login?.supported ?? false)}
          onChange={(e) => void run('login', async () => setLogin(await api.login.set(e.target.checked)))}
        />
        <span>Launch at login</span>
      </label>
      <p className="field-hint">
        {login?.supported
          ? `Registered as ${login.path}; remove that to undo it by hand.`
          : 'Not wired up on this platform yet.'}
      </p>
      {error && <p className="error-text">{error}</p>}
    </section>
  )
}
