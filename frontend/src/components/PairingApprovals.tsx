import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'

/** A pending device-pairing request. Field names are defensive: the gateway's exact
 *  shape varies a little by client, and an unapprovable row is worse than a plain one. */
type PendingRequest = {
  requestId?: string
  id?: string
  deviceId?: string
  nodeId?: string
  /** Which family this came from, so it is resolved with the matching RPC. */
  kind?: 'device' | 'node'
  caps?: string[]
  commands?: string[]
  displayName?: string
  clientId?: string
  platform?: string
  roles?: string[]
  role?: string
  scopes?: string[]
  createdAtMs?: number
}

type Props = {
  connected: boolean
}

const ago = (ms?: number): string => {
  if (!ms) return ''
  const secs = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (secs < 60) return `${secs}s ago`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  return `${Math.floor(secs / 3600)}h ago`
}

/**
 * Approve or reject devices asking to pair with the gateway.
 *
 * This is the one job that otherwise needs the Control UI or the CLI — including
 * approving ClawHQ's own node role, which always requires an operator's blessing.
 */
export function PairingApprovals({ connected }: Props): React.JSX.Element | null {
  const [pending, setPending] = useState<PendingRequest[]>([])
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!connected) return
    // Devices and nodes have separate pending queues and separate approve RPCs, but
    // to the user they are one list of "things asking for access".
    const collect = async (
      method: string,
      kind: 'device' | 'node'
    ): Promise<PendingRequest[]> => {
      try {
        const res = await api.rpc.request<{ pending?: PendingRequest[] }>(method)
        return (res?.pending ?? []).map((r) => ({ ...r, kind }))
      } catch {
        return []
      }
    }

    try {
      const [devices, nodes] = await Promise.all([
        collect('device.pair.list', 'device'),
        collect('node.pair.list', 'node')
      ])
      setPending([...devices, ...nodes])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [connected])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // A request can arrive at any moment, so react to the gateway's own events rather
  // than making the user reopen this screen.
  useEffect(() => {
    return api.onGatewayEvent(({ event }) => {
      if (event.startsWith('device.pair.') || event.startsWith('node.pair.')) void refresh()
    })
  }, [refresh])

  const resolve = async (req: PendingRequest, approve: boolean): Promise<void> => {
    const requestId = req.requestId ?? req.id
    if (!requestId) {
      setError('That request has no id, so it cannot be resolved from here.')
      return
    }
    setBusy(requestId)
    setError(null)
    const family = req.kind === 'node' ? 'node' : 'device'
    try {
      await api.rpc.request(
        `${family}.pair.${approve ? 'approve' : 'reject'}`,
        { requestId }
      )
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  // Stay out of the way entirely when there is nothing to approve.
  if (!connected || (pending.length === 0 && !error)) return null

  return (
    <section className="panel panel-pending">
      <h3>Pending device approvals</h3>
      <p className="field-hint">
        Devices asking to pair with this gateway. Check the roles and scopes before
        approving — this is what grants them access.
      </p>

      <div className="gw-list">
        {pending.map((req) => {
          const requestId = req.requestId ?? req.id ?? ''
          const roles = req.roles ?? (req.role ? [req.role] : [])
          return (
            <div key={requestId || req.deviceId || req.nodeId} className="gw-row">
              <span className="gw-meta">
                <span className="gw-name">
                  {req.displayName || req.clientId || 'unknown client'}
                  {req.platform ? ` · ${req.platform}` : ''}
                </span>
                <span className="gw-url mono">
                  {req.kind === 'node'
                    ? `node · ${(req.commands ?? []).join(', ') || 'no commands'}`
                    : roles.length > 0
                      ? `roles: ${roles.join(', ')}`
                      : 'no roles'}
                  {req.scopes?.length ? ` · ${req.scopes.join(', ')}` : ''}
                </span>
                <span className="gw-url mono">
                  {(req.deviceId ?? req.nodeId)?.slice(0, 16) ?? ''}… {ago(req.createdAtMs ?? (req as any).ts)}
                </span>
              </span>
              <button
                className="btn btn-sm btn-primary"
                disabled={busy !== null}
                onClick={() => resolve(req, true)}
              >
                {busy === requestId ? '…' : 'Approve'}
              </button>
              <button
                className="btn btn-sm btn-danger"
                disabled={busy !== null}
                onClick={() => resolve(req, false)}
              >
                Reject
              </button>
            </div>
          )
        })}
      </div>

      {error && <p className="error-text">{error}</p>}
    </section>
  )
}
