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
  version: 1
  gateways: GatewayProfile[]
  activeGatewayId: string
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

export type NodeStatus = {
  enabled: boolean
  connected: boolean
  gatewayId: string
  deviceId: string
  sharedFolders: string[]
  commands: string[]
  desktopControl: boolean
  error: string
  lastInvoke: string
}

/** A system.notify an agent sent to this machine through the node role. */
export type NodeNotification = {
  title: string
  body: string
  atMs: number
}

/** A node paired with the gateway, as reported by node.list. */
export type RemoteNode = {
  nodeId: string
  displayName?: string
  platform?: string
  connected?: boolean
  caps?: string[]
  commands?: string[]
}

export type UpdateStatus = {
  currentVersion: string
  state: string
  available: boolean
  latestVersion: string
  notes: string
  error: string
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
