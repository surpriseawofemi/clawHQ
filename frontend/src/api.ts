import { Events } from '@wailsio/runtime'
import {
  ConfigService,
  DaemonService,
  GatewayService,
  NodeService,
  UpdateService,
  WindowService
} from '../bindings/github.com/surpriseawofemi/clawhq'
import type {
  Attachment,
  ClawHQConfig,
  ConnectionStatus,
  DaemonStatus,
  Department,
  ExecMode,
  ExecRequest,
  GatewayProfile,
  NodeStatus,
  UpdateStatus,
  NodeNotification
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
    renameGateway: (gatewayId: string, name: string): Promise<ClawHQConfig> =>
      GatewayService.RenameGateway(gatewayId, name) as Promise<ClawHQConfig>,
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
    /** Switch the role on; pairing and approval happen by themselves. */
    enable: (): Promise<NodeStatus> => NodeService.Enable() as Promise<NodeStatus>,
    disable: (): Promise<NodeStatus> => NodeService.Disable() as Promise<NodeStatus>,
    setSharedFolders: (folders: string[]): Promise<NodeStatus> =>
      NodeService.SetSharedFolders(folders) as Promise<NodeStatus>,
    setDesktopControl: (on: boolean): Promise<NodeStatus> =>
      NodeService.SetDesktopControl(on) as Promise<NodeStatus>,
    setExecPolicy: (mode: ExecMode, allow: string[]): Promise<NodeStatus> =>
      NodeService.SetExecPolicy(mode, allow) as Promise<NodeStatus>,
    resolveExec: (id: string, decision: 'allow' | 'always' | 'deny'): Promise<NodeStatus> =>
      NodeService.ResolveExec(id, decision) as Promise<NodeStatus>,
    /** Drop the node identity and pair again with the current command list. */
    rePair: (): Promise<NodeStatus> => NodeService.RePair() as Promise<NodeStatus>
  },
  update: {
    status: (): Promise<UpdateStatus> => UpdateService.Status() as Promise<UpdateStatus>,
    check: (): Promise<UpdateStatus> => UpdateService.Check() as Promise<UpdateStatus>,
    install: (): Promise<void> => UpdateService.Install()
  },
  shell: {
    openPath: (path: string): Promise<void> => DaemonService.OpenPath(path)
  },
  window: {
    /** Open (or focus) the detached desktop window, pointed at a node. */
    openDesktop: (nodeId: string): Promise<void> => WindowService.OpenDesktop(nodeId),
    setDesktopAlwaysOnTop: (on: boolean): Promise<void> => WindowService.SetDesktopAlwaysOnTop(on),
    closeDesktop: (): Promise<void> => WindowService.CloseDesktop()
  },
  /** The main window asked the desktop window to show a different node. */
  onDesktopSelect: (cb: (nodeId: string) => void): (() => void) => {
    return Events.On('desktop:select', (raw: any) => {
      const sel = eventPayload<{ nodeId: string }>(raw)
      if (sel?.nodeId) cb(sel.nodeId)
    })
  },
  rpc: {
    request: rpcRequest,
    sendChat: async (sessionKey: string, message: string): Promise<{ runId: string }> => {
      const raw = await GatewayService.SendChat(sessionKey, message)
      return raw ? JSON.parse(raw) : { runId: '' }
    },
    /** Send with files: text is inlined, images travel as attachments, the rest is reported in `skipped`. */
    sendChatWithFiles: async (
      sessionKey: string,
      message: string,
      paths: string[]
    ): Promise<{ runId?: string; skipped?: string[] }> => {
      const raw = await GatewayService.SendChatWithFiles(sessionKey, message, paths)
      return raw ? JSON.parse(raw) : {}
    }
  },
  attachments: {
    pickFiles: (): Promise<Attachment[]> => GatewayService.PickFiles() as Promise<Attachment[]>,
    pickFolder: (): Promise<Attachment[]> => GatewayService.PickFolder() as Promise<Attachment[]>
  },
  config: {
    get: (): Promise<ClawHQConfig> => ConfigService.Get() as Promise<ClawHQConfig>,
    path: (): Promise<string> => ConfigService.Path(),
    setAutoConnect: (on: boolean): Promise<ClawHQConfig> =>
      ConfigService.SetAutoConnect(on) as Promise<ClawHQConfig>,
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
  onNodeNotification: (cb: (n: NodeNotification) => void): (() => void) => {
    return Events.On('node:notify', (raw: any) => {
      const n = eventPayload<NodeNotification>(raw)
      if (n) cb(n)
    })
  },
  /** An agent wants to run a command on this machine and the policy says ask. */
  onNodeExecRequest: (cb: (req: ExecRequest) => void): (() => void) => {
    return Events.On('node:exec-request', (raw: any) => {
      const req = eventPayload<ExecRequest>(raw)
      if (req) cb(req)
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
