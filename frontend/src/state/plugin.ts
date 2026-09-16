import { api } from '../api'
import type { ActivityRecord, Delegation, Issue, Presence, Task, TeamPost } from '../types'

/**
 * Calls to the ClawHQ gateway plugin. Thin: each is one RPC with its shape typed.
 * Pages check `api.plugin.status()` before leaning on these.
 */
export const plugin = {
  presence: (): Promise<{ agents: Presence[]; delegations: Delegation[]; atMs: number }> =>
    api.rpc.request('clawhq.presence.get', {}),
  activity: (params: { agentId?: string; sinceMs?: number; limit?: number } = {}): Promise<ActivityRecord[]> =>
    api.rpc.request<{ activity?: ActivityRecord[] }>('clawhq.activity.list', params).then((r) => r?.activity ?? []),
  tasks: {
    list: (params: { agentId?: string; status?: string } = {}): Promise<Task[]> =>
      api.rpc.request<{ tasks?: Task[] }>('clawhq.tasks.list', params).then((r) => r?.tasks ?? []),
    create: (input: { title: string; details?: string; agentId?: string }): Promise<Task> =>
      api.rpc.request<{ task: Task }>('clawhq.tasks.create', input).then((r) => r.task),
    update: (id: string, patch: Partial<Task>): Promise<Task> =>
      api.rpc.request<{ task: Task }>('clawhq.tasks.update', { id, ...patch }).then((r) => r.task),
    remove: (id: string): Promise<void> => api.rpc.request('clawhq.tasks.delete', { id }).then(() => undefined)
  },
  team: {
    list: (limit = 200): Promise<TeamPost[]> =>
      api.rpc.request<{ posts?: TeamPost[] }>('clawhq.team.list', { limit }).then((r) => r?.posts ?? []),
    post: (text: string, mentions: string[]): Promise<TeamPost> =>
      api.rpc.request<{ post: TeamPost }>('clawhq.team.post', { text, mentions }).then((r) => r.post),
    turn: (agentId: string, postId: string): Promise<void> =>
      api.rpc.request('clawhq.team.turn', { agentId, postId }).then(() => undefined)
  },
  issues: {
    list: (status?: string): Promise<Issue[]> =>
      api.rpc.request<{ issues?: Issue[] }>('clawhq.issues.list', status ? { status } : {}).then((r) => r?.issues ?? []),
    create: (input: { kind?: string; title: string; body?: string; assigneeAgentId?: string; urgency?: string; from?: string; fromKind?: 'human' | 'agent'; sessionKey?: string }): Promise<Issue> =>
      api.rpc.request<{ issue: Issue }>('clawhq.issues.create', input).then((r) => r.issue),
    reply: (id: string, text: string): Promise<Issue> =>
      api.rpc.request<{ issue: Issue }>('clawhq.issues.reply', { id, text }).then((r) => r.issue),
    update: (id: string, patch: { status?: string; urgency?: string; assigneeAgentId?: string }): Promise<Issue> =>
      api.rpc.request<{ issue: Issue }>('clawhq.issues.update', { id, ...patch }).then((r) => r.issue),
    remove: (id: string): Promise<void> => api.rpc.request('clawhq.issues.delete', { id }).then(() => undefined)
  }
}

/** Splits a long report into one item per bold heading ("**Title** — body"). */
export function splitReport(text: string): { title: string; body: string; urgency: string }[] {
  const out: { title: string; body: string; urgency: string }[] = []
  const re = /^\s*\*\*(.+?)\*\*\s*(?:[—:-]+\s*)?/
  let cur: { title: string; body: string[] } | null = null
  for (const line of text.split('\n')) {
    const m = re.exec(line)
    if (m) {
      if (cur) out.push({ title: cur.title, body: cur.body.join('\n').trim(), urgency: 'normal' })
      cur = { title: m[1].trim(), body: [line.slice(m[0].length)] }
    } else if (cur) {
      cur.body.push(line)
    }
  }
  if (cur) out.push({ title: cur.title, body: cur.body.join('\n').trim(), urgency: 'normal' })
  for (const it of out) {
    const hay = `${it.title} ${it.body}`.toLowerCase()
    if (/needs you|need your|within the hour|urgent|asap|nothing sends without/.test(hay)) it.urgency = 'high'
    if (/^done\b|^— done|\bdone\.|both told/.test(it.body.toLowerCase()) && !/needs you|question/.test(hay)) it.urgency = 'low'
  }
  return out
}

/** The per-agent session Team Chat turns run in; the plugin posts the reply back. */
export const teamSessionKey = (agentId: string): string => `agent:${agentId}:team`

/** The turn an @mention sends into an agent's Team Chat session. */
export function teamPrompt(text: string, toAll: boolean, from = 'the boss (human)'): string {
  return [
    `Team Chat message from ${from}${toAll ? ', to everyone' : ', addressed to you'}:`,
    '',
    text,
    '',
    'Your reply is posted to the Team Chat board automatically, where the boss and every agent can read it. Keep it short. Address someone with @agent-id if the answer is for them; use clawhq_team_post to say more later.'
  ].join('\n')
}

/** The message that starts a task in an agent's thread. The agent closes the card itself. */
export function taskPrompt(task: Task): string {
  const lines = [
    `Task from the ClawHQ board (id ${task.id}): ${task.title}`,
    task.details ? `\n${task.details}` : '',
    '',
    'Do this now. Call clawhq_task_update with status "doing" when you start, and again with status "done" and a short result when finished, or "failed" with the reason if you cannot.'
  ]
  return lines.filter((l) => l !== undefined).join('\n')
}
