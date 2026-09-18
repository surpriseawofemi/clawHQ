import { Events } from '@wailsio/runtime'
import {
  CacheService,
  ClaudeService,
  ClipboardService,
  ConfigService,
  DaemonService,
  DiagService,
  ExecLogService,
  FileService,
  GatewayService,
  HelperService,
  InboxService,
  LoginService,
  MediaService,
  NodeService,
  PluginService,
  ServerService,
  UpdateService,
  WindowService
} from '../bindings/github.com/surpriseawofemi/clawhq'
import type {
  Attachment,
  CachedThread,
  CacheStamp,
  ClawHQConfig,
  ConnectionStatus,
  DaemonStatus,
  Department,
  ExecMode,
  ExecRecord,
  ExecRequest,
  GatewayProfile,
  JoinCommand,
  LoginStatus,
  NodeStatus,
  PluginStatus,
  UpdateStatus,
  NodeNotification,
  ServerProfile,
  ServerHealth,
  ServerAction,
  ProjectInfo,
  DirListing,
  FileContent,
  Transfer,
  ClaudeEvent,
  ClaudeMsg,
  ClaudeSession,
  ClaudeState,
  ServerRun,
  SessionUsage,
  HelperStatus,
  OutboxEvent,
  ServerIssue
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
    resolveExec: (id: string, decision: 'allow' | 'always' | 'trust' | 'deny'): Promise<NodeStatus> =>
      NodeService.ResolveExec(id, decision) as Promise<NodeStatus>,
    /** Override the exec mode for one agent; an empty mode goes back to the machine-wide one. */
    setAgentExecMode: (agentId: string, mode: ExecMode | ''): Promise<NodeStatus> =>
      NodeService.SetAgentExecMode(agentId, mode) as Promise<NodeStatus>,
    /** Drop the node identity and pair again with the current command list. */
    rePair: (): Promise<NodeStatus> => NodeService.RePair() as Promise<NodeStatus>,
    /** One-time command to enrol another machine as a node. */
    joinCommand: (gatewayUrl = ''): Promise<JoinCommand> => NodeService.JoinCommand(gatewayUrl) as Promise<JoinCommand>
  },
  update: {
    status: (): Promise<UpdateStatus> => UpdateService.Status() as Promise<UpdateStatus>,
    check: (): Promise<UpdateStatus> => UpdateService.Check() as Promise<UpdateStatus>,
    install: (): Promise<void> => UpdateService.Install(),
    setAutoUpdate: (on: boolean): Promise<UpdateStatus> =>
      UpdateService.SetAutoUpdate(on) as Promise<UpdateStatus>
  },
  shell: {
    openPath: (path: string): Promise<void> => DaemonService.OpenPath(path)
  },
  inbox: {
    list: (): Promise<NodeNotification[]> => InboxService.List() as Promise<NodeNotification[]>,
    unread: (): Promise<number> => InboxService.Unread(),
    markAllRead: (): Promise<NodeNotification[]> => InboxService.MarkAllRead() as Promise<NodeNotification[]>,
    markRead: (id: string): Promise<NodeNotification[]> => InboxService.MarkRead(id) as Promise<NodeNotification[]>,
    remove: (id: string): Promise<NodeNotification[]> => InboxService.Delete(id) as Promise<NodeNotification[]>,
    clear: (): Promise<NodeNotification[]> => InboxService.Clear() as Promise<NodeNotification[]>
  },
  cache: {
    get: (gatewayId: string, key: string): Promise<CachedThread | null> =>
      CacheService.Get(gatewayId, key) as Promise<CachedThread | null>,
    put: (gatewayId: string, key: string, messagesJson: string, updatedAtMs: number, full: boolean, count: number): Promise<void> =>
      CacheService.Put(gatewayId, key, messagesJson, updatedAtMs, full, count),
    stamps: (gatewayId: string): Promise<CacheStamp[]> => CacheService.Stamps(gatewayId) as Promise<CacheStamp[]>,
    clear: (): Promise<void> => CacheService.Clear(),
    size: (): Promise<number> => CacheService.Size()
  },
  plugin: {
    status: (): Promise<PluginStatus> => PluginService.Status() as Promise<PluginStatus>,
    recheck: (): Promise<PluginStatus> => PluginService.Recheck() as Promise<PluginStatus>,
    /** Install from ClawHub with capability consent and the hook policy the plugin needs. */
    install: (): Promise<PluginStatus> => PluginService.Install() as Promise<PluginStatus>,
    /** Replace the plugin with the newest ClawHub version (the gateway restarts twice). */
    update: (): Promise<PluginStatus> => PluginService.Update() as Promise<PluginStatus>
  },
  /** The gateway plugin appeared, vanished or changed version. */
  onPluginStatus: (cb: (status: PluginStatus) => void): (() => void) => {
    return Events.On('plugin:status', (raw: any) => {
      const st = eventPayload<PluginStatus>(raw)
      if (st) cb(st)
    })
  },
  /** ClawHQ's own config changed on the Go side (an org mirror from the gateway plugin). */
  onConfigChanged: (cb: (config: ClawHQConfig) => void): (() => void) => {
    return Events.On('config:changed', (raw: any) => {
      const cfg = eventPayload<ClawHQConfig>(raw)
      if (cfg) cb(cfg)
    })
  },
  login: {
    status: (): Promise<LoginStatus> => LoginService.Status() as Promise<LoginStatus>,
    set: (on: boolean): Promise<LoginStatus> => LoginService.Set(on) as Promise<LoginStatus>
  },
  servers: {
    list: (): Promise<ServerProfile[]> => ServerService.List() as Promise<ServerProfile[]>,
    save: (p: ServerProfile): Promise<ServerProfile[]> => ServerService.Save(p as never) as Promise<ServerProfile[]>,
    remove: (id: string): Promise<ServerProfile[]> => ServerService.Remove(id) as Promise<ServerProfile[]>,
    health: (id: string): Promise<ServerHealth> => ServerService.Health(id) as Promise<ServerHealth>,
    installClaude: (id: string): Promise<string> => ServerService.InstallClaude(id),
    installAgent: (id: string, agent: string): Promise<string> => ServerService.InstallAgent(id, agent),
    addProject: (id: string, name: string, dir: string, agent: string): Promise<ServerProfile[]> => ServerService.AddProject(id, name, dir, agent) as Promise<ServerProfile[]>,
    updateProject: (id: string, projectId: string, name: string, dir: string): Promise<ServerProfile[]> => ServerService.UpdateProject(id, projectId, name, dir) as Promise<ServerProfile[]>,
    removeProject: (id: string, projectId: string): Promise<ServerProfile[]> => ServerService.RemoveProject(id, projectId) as Promise<ServerProfile[]>,
    selectProject: (id: string, projectId: string): Promise<ServerProfile[]> => ServerService.SelectProject(id, projectId) as Promise<ServerProfile[]>,
    openShell: (id: string, cols: number, rows: number): Promise<string> => ServerService.OpenShell(id, cols, rows),
    openShellIn: (id: string, dir: string, cols: number, rows: number, session = ''): Promise<string> => ServerService.OpenShellIn(id, dir, cols, rows, session),
    tmuxSessions: (id: string): Promise<string[]> => ServerService.TmuxSessions(id).then((r) => r ?? []),
    killTmux: (id: string, session: string): Promise<void> => ServerService.KillTmux(id, session),
    installTmux: (id: string): Promise<string> => ServerService.InstallTmux(id),
    disconnect: (id: string): Promise<void> => ServerService.Disconnect(id),
    projectInfo: (id: string, dir: string): Promise<ProjectInfo> => ServerService.ProjectInfo(id, dir) as Promise<ProjectInfo>,
    write: (shellId: string, b64: string): Promise<void> => ServerService.Write(shellId, b64),
    resize: (shellId: string, cols: number, rows: number): Promise<void> => ServerService.Resize(shellId, cols, rows),
    closeShell: (shellId: string): Promise<void> => ServerService.CloseShell(shellId),
    saveAction: (serverId: string, a: ServerAction): Promise<ServerAction[]> => ServerService.SaveAction(serverId, a as never).then((r) => (r ?? []) as ServerAction[]),
    removeAction: (serverId: string, actionId: string): Promise<ServerAction[]> => ServerService.RemoveAction(serverId, actionId).then((r) => (r ?? []) as ServerAction[]),
    /** Runs one command; output arrives as action:out, the end as action:exit. */
    run: (serverId: string, command: string): Promise<string> => ServerService.RunCommand(serverId, command),
    runIn: (serverId: string, command: string, dir: string): Promise<string> => ServerService.RunCommandIn(serverId, command, dir),
    stop: (runId: string): Promise<void> => ServerService.StopCommand(runId)
  },
  onActionOut: (cb: (e: { serverId: string; runId: string; data: string }) => void): (() => void) =>
    Events.On('action:out', (raw: any) => {
      const e = eventPayload<{ serverId: string; runId: string; data: string }>(raw)
      if (e) cb(e)
    }),
  onActionExit: (cb: (e: { serverId: string; runId: string; code: number; error?: string }) => void): (() => void) =>
    Events.On('action:exit', (raw: any) => {
      const e = eventPayload<{ serverId: string; runId: string; code: number; error?: string }>(raw)
      if (e) cb(e)
    }),
  helper: {
    status: (id: string): Promise<HelperStatus> => HelperService.Status(id) as Promise<HelperStatus>,
    install: (id: string): Promise<HelperStatus> => HelperService.Install(id) as Promise<HelperStatus>,
    wire: (id: string, projectId: string): Promise<HelperStatus> => HelperService.Wire(id, projectId) as Promise<HelperStatus>,
    unwire: (id: string, projectId: string): Promise<HelperStatus> => HelperService.Unwire(id, projectId) as Promise<HelperStatus>,
    outbox: (id: string, sinceTs: number, limit = 300): Promise<OutboxEvent[]> => HelperService.Outbox(id, sinceTs, limit).then((r) => (r ?? []) as OutboxEvent[]),
    issues: (id: string, projectId: string): Promise<ServerIssue[]> => HelperService.Issues(id, projectId).then((r) => (r ?? []) as ServerIssue[]),
    issueDetails: (id: string, projectId: string, n: number): Promise<string> => HelperService.IssueDetails(id, projectId, n),
    mission: (id: string, projectId: string): Promise<string> => HelperService.Mission(id, projectId),
    setMission: (id: string, projectId: string, text: string): Promise<void> => HelperService.SetMission(id, projectId, text),
    sendToSession: (id: string, session: string, text: string): Promise<void> => HelperService.SendToSession(id, session, text),
    startSession: (id: string, projectId: string, resume: string): Promise<string> => HelperService.StartSession(id, projectId, resume)
  },
  onHelperEvent: (cb: (e: { serverId: string; event: OutboxEvent }) => void): (() => void) =>
    Events.On('helper:event', (raw: any) => {
      const e = eventPayload<{ serverId: string; event: OutboxEvent }>(raw)
      if (e) cb(e)
    }),
  clipboard: {
    read: (): Promise<string> => ClipboardService.Read(),
    write: (text: string): Promise<void> => ClipboardService.Write(text)
  },
  claude: {
    state: (serverId: string): Promise<ClaudeState> => ClaudeService.State(serverId) as Promise<ClaudeState>,
    setMode: (serverId: string, mode: string): Promise<ClaudeState> => ClaudeService.SetMode(serverId, mode) as Promise<ClaudeState>,
    setSession: (serverId: string, sessionId: string): Promise<ClaudeState> => ClaudeService.SetSession(serverId, sessionId) as Promise<ClaudeState>,
    setAgent: (serverId: string, agent: string): Promise<ClaudeState> => ClaudeService.SetAgent(serverId, agent) as Promise<ClaudeState>,
    send: (serverId: string, text: string): Promise<string> => ClaudeService.Send(serverId, text),
    abort: (serverId: string): Promise<void> => ClaudeService.Abort(serverId),
    sessions: (serverId: string): Promise<ClaudeSession[]> => ClaudeService.Sessions(serverId).then((r) => (r ?? []) as ClaudeSession[]),
    history: (serverId: string): Promise<ClaudeMsg[]> => ClaudeService.History(serverId).then((r) => (r ?? []) as ClaudeMsg[]),
    runsSince: (atMs: number): Promise<ServerRun[]> => ClaudeService.RunsSince(atMs).then((r) => (r ?? []) as ServerRun[]),
    usageSince: (serverId: string, atMs: number): Promise<SessionUsage[]> => ClaudeService.UsageSince(serverId, atMs).then((r) => (r ?? []) as SessionUsage[])
  },
  onClaudeEvent: (cb: (e: ClaudeEvent) => void): (() => void) =>
    Events.On('claude:event', (raw: any) => {
      const e = eventPayload<ClaudeEvent>(raw)
      if (e) cb(e)
    }),
  files: {
    list: (serverId: string, dir: string): Promise<DirListing> => FileService.List(serverId, dir) as Promise<DirListing>,
    read: (serverId: string, path: string): Promise<FileContent> => FileService.Read(serverId, path) as Promise<FileContent>,
    write: (serverId: string, path: string, text: string): Promise<FileContent> => FileService.Write(serverId, path, text) as Promise<FileContent>,
    mkdir: (serverId: string, path: string): Promise<void> => FileService.Mkdir(serverId, path),
    touch: (serverId: string, path: string): Promise<void> => FileService.Touch(serverId, path),
    rename: (serverId: string, from: string, to: string): Promise<void> => FileService.Rename(serverId, from, to),
    remove: (serverId: string, path: string): Promise<void> => FileService.Delete(serverId, path),
    /** Opens the native picker and uploads the chosen files into the folder. */
    upload: (serverId: string, dir: string): Promise<string[]> => FileService.Upload(serverId, dir).then((r) => r ?? []),
    download: (serverId: string, path: string): Promise<string> => FileService.Download(serverId, path)
  },
  onFileProgress: (cb: (t: Transfer) => void): (() => void) =>
    Events.On('files:progress', (raw: any) => {
      const t = eventPayload<Transfer>(raw)
      if (t) cb(t)
    }),
  /** Terminal output (base64 chunks) and exits for open shells. */
  onShellOut: (cb: (e: { id: string; data: string }) => void): (() => void) =>
    Events.On('ssh:out', (raw: any) => {
      const e = eventPayload<{ id: string; data: string }>(raw)
      if (e) cb(e)
    }),
  onShellExit: (cb: (e: { id: string; error?: string }) => void): (() => void) =>
    Events.On('ssh:exit', (raw: any) => {
      const e = eventPayload<{ id: string; error?: string }>(raw)
      if (e) cb(e)
    }),
  media: {
    /** A data: URL for an artifact an agent attached to a reply. Cached in Go. */
    fetch: (artifactId: string, sessionKey: string): Promise<string> => MediaService.Fetch(artifactId, sessionKey) as Promise<string>
  },
  diag: {
    report: (kind: string, message: string, stack: string): Promise<void> =>
      DiagService.Report(kind, message, stack) as Promise<void>,
    path: (): Promise<string> => DiagService.Path() as Promise<string>
  },
  execLog: {
    list: (): Promise<ExecRecord[]> => ExecLogService.List() as Promise<ExecRecord[]>,
    clear: (): Promise<ExecRecord[]> => ExecLogService.Clear() as Promise<ExecRecord[]>
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
    setMenuBar: (on: boolean): Promise<ClawHQConfig> =>
      ConfigService.SetMenuBar(on) as Promise<ClawHQConfig>,
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
