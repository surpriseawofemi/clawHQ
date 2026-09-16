import { api } from '../api'
import type { ActivityRecord, Delegation, Presence, Task } from '../types'

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
  }
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
