/** Shapes returned by the OpenClaw gateway, narrowed to what ClawHQ actually reads. */

export type AgentIdentity = {
  name?: string
  emoji?: string
  theme?: string
}

export type Agent = {
  id: string
  name?: string
  identity?: AgentIdentity
  workspace?: string
  model?: { primary?: string }
  agentRuntime?: { id?: string }
  thinkingOptions?: string[]
  thinkingDefault?: string
  defaultPermissionMode?: string
}

export type AgentsList = {
  defaultId?: string
  agents: Agent[]
}

export type SessionInfo = {
  key: string
  agentId: string
  label?: string
  kind?: string
  isMain?: boolean
  updatedAt?: number
  lastActivityAt?: number
  totalTokens?: number
  contextTokens?: number
  model?: string
  hasActiveRun?: boolean
  status?: string
}

export type ContentBlock = { type: string; text?: string; [k: string]: unknown }

export type ChatMessage = {
  role: 'user' | 'assistant' | 'system' | string
  content: string | ContentBlock[]
  timestamp?: number
  model?: string
  usage?: { totalTokens?: number }
  __openclaw?: { id?: string; senderName?: string }
}

export type Department = {
  id: string
  name: string
  emoji: string
  order: number
}

export type GatewayProfile = {
  id: string
  name: string
  url: string
  lastConnectedAtMs?: number
}

export type ClawHQConfig = {
  version: number
  gateways: GatewayProfile[]
  activeGatewayId: string
  autoConnect: boolean
  autoUpdate: boolean
  /** Keep running in the menu bar (system tray) when the window closes. */
  menuBar: boolean
  departments: Department[]
  assignments: Record<string, string>
}

/** "pending" means the gateway has the request but an operator must approve it. */
export type ConnectionPhase = 'idle' | 'connecting' | 'connected' | 'pending' | 'error'

export type ConnectionStatus = {
  phase: ConnectionPhase
  gatewayId: string
  url: string | null
  scopes: string[]
  deviceId: string | null
  serverVersion: string | null
  error: string | null
  paired: boolean
  waitingSinceMs?: number
}

export type DaemonStatus = {
  installed: boolean
  running: boolean
  pid: number | null
  uptimeMs: number | null
  cliVersion: string | null
  serviceLabel: string | null
  logFile: string | null
  error: string | null
}

/** A message still streaming in, before the `final` frame commits it to history. */
export type StreamingReply = {
  sessionKey: string
  runId: string
  text: string
  phase: string | null
}

/** What the node role is doing right now. */
export type NodePairing = '' | 'connecting' | 'awaiting-approval' | 'reconnecting' | 'connected'

/** How agent commands (system.run) are handled on this machine. */
export type ExecMode = 'off' | 'ask' | 'allow'

/** A command an agent wants to run here, waiting for the user's decision. */
export type ExecRequest = {
  id: string
  command: string
  argv: string[]
  cwd: string
  agentId: string
  sessionKey: string
  atMs: number
  expiresAtMs: number
}

/** One command an agent asked this machine to run, from the local audit log. */
export type ExecRecord = {
  id: string
  atMs: number
  agentId?: string
  sessionKey?: string
  command: string
  cwd?: string
  decision: string
  ran: boolean
  exitCode?: number
  success: boolean
  timedOut?: boolean
  durationMs?: number
  output?: string
  error?: string
}

export type NodeStatus = {
  enabled: boolean
  connected: boolean
  gatewayId: string
  deviceId: string
  sharedFolders: string[]
  commands: string[]
  desktopControl: boolean
  pairing: NodePairing
  error: string
  lastInvoke: string
  execMode: ExecMode
  execAllow: string[]
  /** Per-agent overrides of execMode, by agent id. */
  execAgents: Record<string, ExecMode>
  pendingExec: ExecRequest[]
}

/** An exec approval the gateway is asking operators to decide, from exec.approval.requested. */
export type GatewayExecApproval = {
  id: string
  request: {
    command?: string
    cwd?: string | null
    host?: string | null
    nodeId?: string | null
    agentId?: string | null
    allowedDecisions?: string[]
  }
  createdAtMs?: number
  expiresAtMs?: number
}

/** A system.notify an agent sent to this machine through the node role. */
export type NodeNotification = {
  id: string
  title: string
  body: string
  agentId?: string
  agentName?: string
  agentEmoji?: string
  sessionKey?: string
  /** Machine that received the original, when this copy was relayed from another ClawHQ. */
  origin?: string
  atMs: number
  read: boolean
}

/** A node paired with the gateway, as reported by node.list. */
export type RemoteNode = {
  nodeId: string
  displayName?: string
  platform?: string
  connected?: boolean
  caps?: string[]
  commands?: string[]
  clientId?: string
  version?: string
}

/** Live state of one agent, from the gateway plugin's hooks. */
export type Presence = {
  agentId: string
  state: 'idle' | 'working'
  sinceMs: number
  sessionKey?: string
  runId?: string
  tool?: string
  toolSinceMs?: number
  lastLine?: string
  lastEndMs?: number
  lastSuccess?: boolean
}

/** A parent agent waiting on a child it spawned. */
export type Delegation = {
  childSessionKey: string
  childAgentId: string
  parentAgentId?: string
  parentSessionKey?: string
  label?: string
  runId?: string
  sinceMs: number
}

export type TaskStatus = 'todo' | 'doing' | 'done' | 'failed'

/** A card on the shared task board, kept by the gateway plugin. */
export type Task = {
  id: string
  title: string
  details?: string
  agentId?: string
  status: TaskStatus
  createdAtMs: number
  updatedAtMs: number
  createdBy: string
  sessionKey?: string
  runId?: string
  startedAtMs?: number
  finishedAtMs?: number
  result?: string
  error?: string
}

/** One agent run, as the gateway plugin records it on agent_end. */
export type ActivityRecord = {
  id: string
  atMs: number
  agentId?: string
  sessionKey?: string
  runId?: string
  success: boolean
  durationMs?: number
  error?: string
  summary?: string
  channel?: string
}

/** The ClawHQ gateway plugin, as seen from this ClawHQ. */
export type PluginStatus = {
  checked: boolean
  present: boolean
  version: string
  features: string[]
  package: string
  error: string
  /** Newest version on ClawHub, once looked up. */
  latest: string
  updateAvailable: boolean
  /** Step of an update in flight: uninstalling, installing, enabling; empty when idle. */
  upgrading: string
}

/** A thread as kept in the on-disk cache: the messages as JSON plus the stamp they were fetched at. */
export type CachedThread = {
  gatewayId: string
  key: string
  json: string
  updatedAtMs: number
  full: boolean
  count: number
  storedAtMs: number
}

export type CacheStamp = {
  key: string
  updatedAtMs: number
  count: number
  full: boolean
}

/** The one command that enrols another machine as a node, with its one-time code. */
export type JoinCommand = {
  command: string
  code: string
  expiresAtMs: number
  gatewayUrl: string
}

/** Whether ClawHQ starts with the user's session, and whether this OS supports it. */
export type LoginStatus = {
  enabled: boolean
  supported: boolean
  path: string
}

export type UpdateStatus = {
  currentVersion: string
  state: string
  available: boolean
  latestVersion: string
  notes: string
  error: string
  autoUpdate: boolean
  lastCheckedAtMs: number
}

export const UNASSIGNED = '__unassigned__'

/** Flatten assistant content blocks into displayable text. */
export function messageText(msg: ChatMessage): string {
  if (typeof msg.content === 'string') return msg.content
  if (!Array.isArray(msg.content)) return ''
  return msg.content
    .filter((b) => b?.type === 'text' && typeof b.text === 'string')
    .map((b) => b.text as string)
    .join('\n')
}

export function agentLabel(agent: Agent): string {
  return agent.identity?.name?.trim() || agent.name?.trim() || agent.id
}

export function agentEmoji(agent: Agent): string {
  return agent.identity?.emoji?.trim() || '🤖'
}

/** Something picked for the composer: a text file, an image, a folder, or a skip. */
export type Attachment = {
  path: string
  name: string
  kind: 'text' | 'image' | 'folder' | 'unsupported'
  size: number
  note?: string
}

/** One post on the Team Chat board, kept by the gateway plugin. */
export type TeamPost = {
  id: string
  atMs: number
  from: string
  fromKind: 'human' | 'agent'
  text: string
  mentions: string[]
  sessionKey?: string
  runId?: string
  hops?: number
  replyTo?: string
}

/** Something an agent needs from you, or a task you gave an agent, answered one by one. */
export type IssueKind = 'question' | 'task' | 'issue' | 'improvement'
export type IssueStatus = 'open' | 'in-progress' | 'resolved'
export type IssueUrgency = 'low' | 'normal' | 'high' | 'urgent'
export type IssueReply = { id: string; by: string; byKind: 'human' | 'agent'; text: string; atMs: number }
export type Issue = {
  id: string
  kind: IssueKind
  title: string
  body?: string
  from: string
  fromKind: 'human' | 'agent'
  assigneeAgentId?: string
  urgency: IssueUrgency
  status: IssueStatus
  createdAtMs: number
  updatedAtMs: number
  resolvedAtMs?: number
  sessionKey?: string
  replies: IssueReply[]
}

/** A machine reached over SSH. Local to this ClawHQ. */
export type ServerProfile = {
  id: string
  name: string
  host: string
  port: number
  user: string
  auth: 'agent' | 'key' | 'password'
  keyPath?: string
  /** Only ever sent up; the list never returns it. */
  password?: string
  hasPassword?: boolean
  dir?: string
  addedAtMs: number
  lastOkAtMs?: number
  actions?: ServerAction[]
  projects?: ServerProject[]
  activeProjectId?: string
  /** Periodic health check with warnings in the bell. */
  monitor?: boolean
  /** Terminals run in tmux on the server and survive a ClawHQ restart. */
  tmux?: boolean
  platform?: string
  /** Kept to this ClawHQ: agents never see it, it takes no tasks, and it is not in Team Chat. */
  isolated?: boolean
  /** Agents may hand it tasks (default on). */
  tasks?: boolean
  /** Agent ids allowed to hand it tasks; empty means every agent. */
  taskAgents?: string[]
  /** One line agents see when choosing a server. */
  description?: string
}

/** A folder on a server the coding agents work in. */
export type ServerProject = { id: string; name: string; dir: string; agent?: AgentID; sessions?: Record<string, string>; mode?: string }
export type AgentID = 'claude' | 'codex' | 'gemini' | 'grok' | 'opencode'
export type AgentStatus = { id: AgentID; label: string; installed: boolean; version: string; path: string; loggedIn: boolean; account?: string; install: string; loginHint: string }

/** A saved command on a server, run from a button. */
export type ServerAction = { id: string; name: string; command: string; confirm: boolean; projectId?: string }

export type ServerCheck = { id: string; label: string; ok: boolean; value: string; hint?: string }

export type ServerHealth = {
  platform?: 'windows' | 'unix' | string
  tmux: { installed: boolean; version: string }
  agents: AgentStatus[]
  cores: number
  ok: boolean
  error?: string
  checkedAtMs: number
  hostname: string
  os: string
  user: string
  uptime: string
  load: string
  disk: string
  memory: string
  home: string
  node: { installed: boolean; version: string }
  git: { installed: boolean; version: string }
  claude: { installed: boolean; version: string; path: string; loggedIn: boolean; account: string }
  checks: ServerCheck[]
}

export type FileEntry = { name: string; path: string; isDir: boolean; size: number; modMs: number; mode: string; link: boolean }
export type DirListing = { path: string; parent: string; home: string; entries: FileEntry[] }
export type FileContent = { path: string; size: number; modMs: number; mime: string; isText: boolean; text?: string; truncated: boolean; dataUrl?: string }
export type Transfer = { serverId: string; name: string; done: number; total: number; finished: boolean; error?: string }

/** Headless Claude Code on a server. */
export type ClaudeBlock = { type: 'text' | 'tool_use' | 'tool_result'; text?: string; name?: string; input?: unknown; isError?: boolean }
export type ClaudeMsg = { role: 'user' | 'assistant'; atMs: number; blocks: ClaudeBlock[] }
export type ClaudeSession = { id: string; modMs: number; size: number; prompt: string }
export type ClaudeState = { agent: AgentID; mode: 'auto' | 'semi' | 'manual'; sessionId: string; running: boolean; dir: string; projectId: string }
export type ClaudeEvent = {
  serverId: string
  runId: string
  type: 'init' | 'delta' | 'assistant' | 'user' | 'result' | 'error' | 'done'
  sessionId?: string
  text?: string
  message?: { role?: string; content?: unknown }
  result?: { total_cost_usd?: number; duration_ms?: number; num_turns?: number; is_error?: boolean; subtype?: string }
}

/** One headless agent run on a server, from ClawHQ's local run log. */
export type ServerRun = { serverId: string; serverName: string; projectId: string; project: string; agent: string; atMs: number; durationMs: number; costUsd: number; turns: number; ok: boolean; summary: string; source: 'chat' | 'task' }
/** Token usage summed from Claude Code's session files on a server (terminal sessions included). */
export type SessionUsage = { serverId: string; project: string; sessions: number; messages: number; inputTokens: number; outputTokens: number; cacheRead: number }

/** What a project folder contains, read once, with actions to suggest. */
export type ProjectInfo = {
  dir: string
  exists: boolean
  gitRemote?: string
  gitBranch?: string
  gitDirty: number
  package?: string
  scripts: string[]
  goModule?: string
  composer?: string
  python: boolean
  docker: boolean
  pm2?: string
  hasClaudeMd: boolean
  hasEnv: boolean
  files: number
  suggested: ServerAction[]
}

/** The ClawHQ helper on a server and how each project is wired to it. */
export type ProjectWiring = { projectId: string; dir: string; mcp: boolean; hooks: boolean; claudeMd: boolean; mission: boolean; wired: boolean; openIssues: number }
export type HelperStatus = { installed: boolean; version: string; current: boolean; projects: ProjectWiring[] }
/** One line of the helper's outbox: a reply the session gave, a log line, an issue. */
export type OutboxEvent = {
  ts: number
  type: 'reply' | 'session-end' | 'log' | 'issue' | 'issue-update' | 'recommendation' | 'rec-update'
  r?: number
  effort?: string
  event?: string
  project: string
  sessionId?: string
  text?: string
  kind?: string
  model?: string
  usage?: { input: number; output: number; cacheRead: number } | null
  files?: string[]
  billing?: string
  n?: number
  title?: string
  needsBoss?: boolean
  urgency?: string
  status?: string
  note?: string
}
export type ServerRecommendation = { r: number; title: string; effort: string; status: 'open' | 'later' | 'accepted' | 'dismissed'; issueN?: number; createdAt: number; updatedAt: number }
export type ServerIssue = { n: number; title: string; status: 'open' | 'done' | 'dismissed'; needsBoss: boolean; urgency: string; createdAt: number; updatedAt: number; notes: { ts: number; text: string }[] }
