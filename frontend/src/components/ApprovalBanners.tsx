import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ExecRequest, GatewayExecApproval } from '../types'

type Props = {
  connected: boolean
}

/** A pending decision from either side, flattened for one banner shape. */
type Ask = {
  key: string
  source: 'node' | 'gateway'
  title: string
  command: string
  detail: string
  decisions: { label: string; value: string; primary?: boolean; danger?: boolean }[]
  resolve: (decision: string) => Promise<void>
}

const NODE_DECISIONS: Ask['decisions'] = [
  { label: 'Allow once', value: 'allow', primary: true },
  { label: 'Always', value: 'always' },
  { label: 'Deny', value: 'deny', danger: true }
]

const gatewayDecisionLabel = (d: string): string =>
  ({ 'allow-once': 'Allow once', 'allow-always': 'Always', deny: 'Deny', ask: 'Ask me' })[d] ?? d

/**
 * Commands waiting for a human, from two places.
 *
 * The node role asks when an agent's exec tool targets this machine and the policy
 * says "ask". The gateway asks when its own exec policy wants an operator's decision,
 * for any host; ClawHQ holds the operator.approvals scope so those land here too.
 * Both are answered with one tap and never run silently.
 */
export function ApprovalBanners({ connected }: Props): React.JSX.Element | null {
  const [nodeAsks, setNodeAsks] = useState<ExecRequest[]>([])
  const [gatewayAsks, setGatewayAsks] = useState<GatewayExecApproval[]>([])
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Node side: the status carries the full pending list, the event is the nudge.
  useEffect(() => {
    api.node
      .status()
      .then((s) => setNodeAsks(s.pendingExec ?? []))
      .catch(() => undefined)
    const offStatus = api.onNodeStatus((s) => setNodeAsks(s.pendingExec ?? []))
    const offReq = api.onNodeExecRequest((req) =>
      setNodeAsks((prev) => (prev.some((r) => r.id === req.id) ? prev : [...prev, req]))
    )
    return () => {
      offStatus()
      offReq()
    }
  }, [])

  // Gateway side: requested and resolved events; the list empties on disconnect
  // because a request cannot be answered through a connection that is gone.
  useEffect(() => {
    if (!connected) {
      setGatewayAsks([])
      return
    }
    return api.onGatewayEvent(({ event, payload }) => {
      if (event === 'exec.approval.requested' && payload?.id) {
        setGatewayAsks((prev) =>
          prev.some((a) => a.id === payload.id) ? prev : [...prev, payload as GatewayExecApproval]
        )
      } else if (event === 'exec.approval.resolved' && payload?.id) {
        setGatewayAsks((prev) => prev.filter((a) => a.id !== payload.id))
      }
    })
  }, [connected])

  const asks: Ask[] = [
    ...nodeAsks.map<Ask>((req) => ({
      key: `node-${req.id}`,
      source: 'node',
      title: `${req.agentId || 'An agent'} wants to run a command on this Mac`,
      command: req.command,
      detail: req.cwd ? `in ${req.cwd}` : '',
      decisions: NODE_DECISIONS,
      resolve: async (decision) => {
        await api.node.resolveExec(req.id, decision as 'allow' | 'always' | 'deny')
        setNodeAsks((prev) => prev.filter((r) => r.id !== req.id))
      }
    })),
    ...gatewayAsks.map<Ask>((a) => {
      const host = a.request.host === 'node' ? `node ${(a.request.nodeId ?? '').slice(0, 8)}` : 'the gateway'
      const allowed = a.request.allowedDecisions?.length
        ? a.request.allowedDecisions
        : ['allow-once', 'allow-always', 'deny']
      return {
        key: `gw-${a.id}`,
        source: 'gateway',
        title: `${a.request.agentId || 'An agent'} wants to run a command on ${host}`,
        command: a.request.command ?? '',
        detail: a.request.cwd ? `in ${a.request.cwd}` : '',
        decisions: allowed
          .filter((d) => d !== 'ask')
          .map((d) => ({
            label: gatewayDecisionLabel(d),
            value: d,
            primary: d === 'allow-once',
            danger: d === 'deny'
          })),
        resolve: async (decision) => {
          await api.rpc.request('exec.approval.resolve', { id: a.id, decision })
          setGatewayAsks((prev) => prev.filter((x) => x.id !== a.id))
        }
      }
    })
  ]

  if (asks.length === 0) return null

  const answer = async (ask: Ask, decision: string): Promise<void> => {
    setBusy(ask.key)
    setError(null)
    try {
      await ask.resolve(decision)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="approvals" role="region" aria-label="Commands waiting for approval">
      {asks.map((ask) => (
        <div key={ask.key} className="approval" role="alertdialog">
          <span className="notice-icon">🛡️</span>
          <span className="notice-text">
            <strong>{ask.title}</strong>
            <code className="approval-cmd">{ask.command}</code>
            {ask.detail && <span className="field-hint">{ask.detail}</span>}
          </span>
          <span className="approval-actions">
            {ask.decisions.map((d) => (
              <button
                key={d.value}
                className={`btn btn-sm${d.primary ? ' btn-primary' : ''}${d.danger ? ' btn-danger' : ''}`}
                disabled={busy !== null}
                onClick={() => void answer(ask, d.value)}
              >
                {busy === ask.key ? '…' : d.label}
              </button>
            ))}
          </span>
        </div>
      ))}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
