import { api } from '../api'
import type { OutboxEvent, ServerIssue } from '../types'

/**
 * Autopilot data (mission, log, issues) for every project ever opened, kept
 * fresh in the background: every 10 seconds while its tab is showing, every 30
 * seconds otherwise. The tab reads from here, so it is never empty on return
 * and the bell's counts stay current without a visit.
 */
export type ApData = { mission: string; events: OutboxEvent[]; issues: ServerIssue[]; loadedAt: number; error?: string }
type Entry = { serverId: string; projectId: string; dir: string; data: ApData; subs: Set<() => void>; timer: number | null; inflight: boolean }

const entries = new Map<string, Entry>()
const key = (serverId: string, projectId: string): string => `${serverId}:${projectId}`

async function fetch(e: Entry): Promise<void> {
  if (e.inflight) return
  e.inflight = true
  try {
    const [mission, events, issues] = await Promise.all([
      api.helper.mission(e.serverId, e.projectId),
      api.helper.outbox(e.serverId, Date.now() - 3 * 86_400_000, 400),
      api.helper.issues(e.serverId, e.projectId)
    ])
    const dir = e.dir.replace(/\/$/, '')
    e.data = { mission, events: events.filter((ev) => !dir || ev.project === dir || ev.project === e.dir), issues, loadedAt: Date.now() }
  } catch (err) {
    e.data = { ...e.data, error: err instanceof Error ? err.message : String(err) }
  } finally {
    e.inflight = false
    e.subs.forEach((cb) => cb())
    schedule(e)
  }
}

function schedule(e: Entry): void {
  if (e.timer) window.clearTimeout(e.timer)
  const delay = e.subs.size > 0 ? 10_000 : 30_000
  e.timer = window.setTimeout(() => void fetch(e), delay)
}

export const autopilot = {
  /** Registers a project for background syncing (idempotent) and returns its data. */
  ensure(serverId: string, projectId: string, dir: string): ApData {
    const k = key(serverId, projectId)
    let e = entries.get(k)
    if (!e) {
      e = { serverId, projectId, dir, data: { mission: '', events: [], issues: [], loadedAt: 0 }, subs: new Set(), timer: null, inflight: false }
      entries.set(k, e)
      void fetch(e)
    } else if (e.dir !== dir) {
      e.dir = dir
      void fetch(e)
    }
    return e.data
  },
  get(serverId: string, projectId: string): ApData | undefined {
    return entries.get(key(serverId, projectId))?.data
  },
  subscribe(serverId: string, projectId: string, cb: () => void): () => void {
    const e = entries.get(key(serverId, projectId))
    if (!e) return () => undefined
    e.subs.add(cb)
    schedule(e) // faster while watched
    return () => {
      e.subs.delete(cb)
      schedule(e)
    }
  },
  refresh(serverId: string, projectId: string): Promise<void> {
    const e = entries.get(key(serverId, projectId))
    return e ? fetch(e) : Promise.resolve()
  },
  /** Pushes a live event in without waiting for the next poll. */
  push(serverId: string, ev: OutboxEvent): void {
    for (const e of entries.values()) {
      if (e.serverId !== serverId) continue
      const dir = e.dir.replace(/\/$/, '')
      if (dir && ev.project !== dir && ev.project !== e.dir) continue
      e.data = { ...e.data, events: [...e.data.events, ev].slice(-400) }
      e.subs.forEach((cb) => cb())
      if (ev.type === 'issue' || ev.type === 'issue-update') void fetch(e)
    }
  }
}

api.onHelperEvent((e) => autopilot.push(e.serverId, e.event))
