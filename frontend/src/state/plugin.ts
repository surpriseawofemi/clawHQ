import { api } from '../api'
import type { ActivityRecord, Delegation, Presence, Task, TeamPost } from '../types'

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
      api.rpc.request<{ post: TeamPost }>('clawhq.team.post', { text, mentions }).then((r) => r.post)
  }
}

/** The per-agent session Team Chat turns run in; the plugin posts the reply back. */
export const teamSessionKey = (agentId: string): string => `agent:${agentId}:team`

/** The turn an @mention sends into an agent's Team Chat session. */
export function teamPrompt(text: string, toAll: boolean): string {
  return [
    `Team Chat message from the boss (human)${toAll ? ', to everyone' : ', addressed to you'}:`,
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
