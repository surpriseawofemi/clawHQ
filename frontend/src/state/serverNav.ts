/** A one-shot request to open a server page somewhere specific (from Issues, the bell). */
export type ServerNav = { serverId: string; projectId?: string; tab?: 'health' | 'chat' | 'files' | 'actions' | 'terminal' | 'autopilot'; prefill?: string }
let pending: ServerNav | null = null
export const serverNav = {
  set(n: ServerNav): void {
    pending = n
  },
  take(): ServerNav | null {
    const n = pending
    pending = null
    return n
  }
}
/** The tmux session name the live Claude Code of a project runs in (mirrors Go). */
export const liveSessionName = (projectName: string): string => `clawhq-live-${projectName.toLowerCase().replace(/[^a-z0-9_-]/g, '-')}`
