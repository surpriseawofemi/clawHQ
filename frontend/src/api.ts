import { Events } from '@wailsio/runtime'
import {
  ConfigService,
  DaemonService,
  GatewayService,
  NodeService,
  UpdateService
} from '../bindings/github.com/surpriseawofemi/clawhq'
import type {
  ClawHQConfig,
  ConnectionStatus,
  DaemonStatus,
  Department,
  GatewayProfile,
  NodeStatus,
  UpdateStatus
} from './types'

/**
 * Adapter over the generated Wails bindings.
 *
 * The components were written against an IPC surface with this shape, so keeping it
 * lets the UI stay transport-agnostic. Two Wails-specific details are absorbed here:
 * gateway RPC payloads cross as JSON strings (the gateway's 424 methods share no Go
 * type), and Wails event callbacks receive an envelope whose `data` holds the payload.
 */

/** Gateway RPC payloads arrive as strings; parse once, here. */
async function rpcRequest<T = any>(method: string, params?: unknown): Promise<T> {
  const raw = await GatewayService.Request(method, params ? JSON.stringify(params) : '')
  return (raw ? JSON.parse(raw) : null) as T
}

/**
 * Wails delivers events as `{ data: [payload] }` (variadic emit args) or `{ data: payload }`
 * depending on how the event was emitted. Normalise both to the payload itself.
 */
function eventPayload<T>(evt: any): T {
  const data = evt?.data
  return (Array.isArray(data) ? data[0] : data) as T
}

export const api = {
  connection: {
    status: (): Promise<ConnectionStatus> => GatewayService.Status() as Promise<ConnectionStatus>,
    gateways: (): Promise<GatewayProfile[]> =>
      GatewayService.Gateways() as Promise<GatewayProfile[]>,
    hasPairing: (gatewayId: string): Promise<boolean> => GatewayService.HasPairing(gatewayId),
    /** Pair or reconnect with a gateway URL + its shared token. */
    connectWithToken: (
      gatewayId: string,
      name: string,
      url: string,
      token: string
    ): Promise<ConnectionStatus> =>
      GatewayService.ConnectWithToken(gatewayId, name, url, token) as Promise<ConnectionStatus>,
    pairWithSetupCode: (name: string, setupCode: string): Promise<ConnectionStatus> =>
      GatewayService.PairWithSetupCode(name, setupCode) as Promise<ConnectionStatus>,
    /** Reconnect to a saved gateway using its stored device token. */
    connect: (gatewayId?: string): Promise<ConnectionStatus> =>
      GatewayService.Connect(gatewayId ?? '') as Promise<ConnectionStatus>,
    removeGateway: (gatewayId: string): Promise<ClawHQConfig> =>
      GatewayService.RemoveGateway(gatewayId) as Promise<ClawHQConfig>,
    forgetPairing: (gatewayId: string): Promise<void> => GatewayService.ForgetPairing(gatewayId),
    cancelApprovalWait: (): Promise<void> => GatewayService.CancelApprovalWait(),
    disconnect: (): Promise<void> => GatewayService.Disconnect()
  },
  daemon: {
    status: (): Promise<DaemonStatus> => DaemonService.Status() as Promise<DaemonStatus>,
    control: (action: 'start' | 'stop' | 'restart'): Promise<DaemonStatus> =>
      DaemonService.Control(action) as Promise<DaemonStatus>,
    mintSetupCode: async (): Promise<{ setupCode: string }> => ({
      setupCode: await DaemonService.MintSetupCode()
    }),
    cliAvailable: (): Promise<boolean> => DaemonService.CLIAvailable()
  },
  node: {
    status: (): Promise<NodeStatus> => NodeService.Status() as Promise<NodeStatus>,
    enable: (token: string): Promise<NodeStatus> => NodeService.Enable(token) as Promise<NodeStatus>,
    disable: (): Promise<NodeStatus> => NodeService.Disable() as Promise<NodeStatus>,
    setSharedFolders: (folders: string[]): Promise<NodeStatus> =>
      NodeService.SetSharedFolders(folders) as Promise<NodeStatus>
  },
  update: {
    status: (): Promise<UpdateStatus> => UpdateService.Status() as Promise<UpdateStatus>,
    check: (): Promise<UpdateStatus> => UpdateService.Check() as Promise<UpdateStatus>,
    install: (): Promise<void> => UpdateService.Install()
  },
  shell: {
    openPath: (path: string): Promise<void> => DaemonService.OpenPath(path)
  },
  rpc: {
    request: rpcRequest,
    sendChat: async (sessionKey: string, message: string): Promise<{ runId: string }> => {
      const raw = await GatewayService.SendChat(sessionKey, message)
      return raw ? JSON.parse(raw) : { runId: '' }
    }
  },
  config: {
    get: (): Promise<ClawHQConfig> => ConfigService.Get() as Promise<ClawHQConfig>,
    path: (): Promise<string> => ConfigService.Path(),
    assignAgent: (agentId: string, departmentId: string | null): Promise<ClawHQConfig> =>
      ConfigService.AssignAgent(agentId, departmentId ?? '') as Promise<ClawHQConfig>,
    upsertDepartment: (dept: Partial<Department>): Promise<ClawHQConfig> =>
      ConfigService.UpsertDepartment({
        id: dept.id ?? '',
        name: dept.name ?? '',
        emoji: dept.emoji ?? '',
        order: dept.order ?? 0
      } as Department) as Promise<ClawHQConfig>,
    removeDepartment: (id: string): Promise<ClawHQConfig> =>
      ConfigService.RemoveDepartment(id) as Promise<ClawHQConfig>
  },
  /** Subscribe to gateway event frames. Returns an unsubscribe function. */
  onGatewayEvent: (cb: (evt: { event: string; payload: any }) => void): (() => void) => {
    return Events.On('gateway:event', (raw: any) => {
      const frame = eventPayload<{ event: string; payload: any }>(raw)
      if (!frame) return
      // The Go side forwards the payload as raw JSON; decode it for the UI.
      let payload = frame.payload
      if (typeof payload === 'string') {
        try {
          payload = JSON.parse(payload)
        } catch {
          /* leave as-is if it was never JSON */
        }
      }
      cb({ event: frame.event, payload })
    })
  },
  onNodeStatus: (cb: (status: NodeStatus) => void): (() => void) => {
    return Events.On('node:status', (raw: any) => {
      const status = eventPayload<NodeStatus>(raw)
      if (status) cb(status)
    })
  },
  onConnectionStatus: (cb: (status: ConnectionStatus) => void): (() => void) => {
    return Events.On('gateway:status', (raw: any) => {
      const status = eventPayload<ConnectionStatus>(raw)
      if (status) cb(status)
    })
  }
}

export type ClawHQApi = typeof api
