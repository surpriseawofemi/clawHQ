import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { renderMarkdown } from '../markdown'
import { terminals } from '../state/terminals'
import { liveSessionName } from '../state/serverNav'
import { autopilot } from '../state/autopilot'
import { getPrefs, setPref } from '../prefs'
import { Toggle } from './Toggle'
import { RefreshIcon } from './icons'
import type { HelperStatus, OutboxEvent, ServerIssue, ServerProfile, ServerProject, ServerRecommendation } from '../types'

const when = (ms: number): string => {
  const d = new Date(ms)
  const today = new Date().toDateString() === d.toDateString()
  return today ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
const tokens = (n: number): string => (n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${Math.round(n / 1e3)}k` : String(n))

const START_INSTRUCTION = (mission: string): string =>
  `Standing instruction from the boss via ClawHQ. Read the mission below and keep it: create an hourly schedule for yourself that runs the loop it describes, log with clawhq_log, open numbered issues with clawhq_issue for anything that needs me, and keep details in the issue files. Confirm in one line when the schedule exists.\n\n---\n${mission.trim()}`
const PAUSE_INSTRUCTION = 'From the boss via ClawHQ: pause the hourly schedule until I say resume. Confirm in one line.'
const RESUME_INSTRUCTION = 'From the boss via ClawHQ: resume the hourly schedule and continue the mission. Confirm in one line.'

/**
 * Autopilot: the standing mission for a project, the live Claude Code session
 * that carries it out (in tmux, so it survives ClawHQ closing), the hourly log
 * the helper collects, and the numbered issues waiting for the boss.
 */
const apMemory = new Map<string, { mission: string; events: OutboxEvent[]; issues: ServerIssue[]; details: { n: number; md: string; kind?: 'issue' | 'rec' } | null; showDone: boolean }>()

export function AutopilotView({ server, project, helper, onHelper, onDiscuss }: { server: ServerProfile; project: ServerProject; helper: HelperStatus | null; onHelper: (h: HelperStatus) => void; onDiscuss: (text: string) => void }): React.JSX.Element {
  const memKey = `${server.id}:${project.id}`
  const mem = apMemory.get(memKey)
  const initial = autopilot.ensure(server.id, project.id, project.dir)
  const [mission, setMission] = useState(initial.mission)
  const [missionDraft, setMissionDraft] = useState(mem?.mission ?? initial.mission)
  const [events, setEvents] = useState<OutboxEvent[]>(initial.events)
  const [issues, setIssues] = useState<ServerIssue[]>(initial.issues)
  const [recs, setRecs] = useState<ServerRecommendation[]>(initial.recs)
  const [details, setDetails] = useState<{ n: number; md: string; kind?: 'issue' | 'rec' } | null>(mem?.details ?? null)
  const [view, setViewState] = useState(getPrefs().apView)
  const setView = (v: typeof view): void => { setViewState(v); setPref('apView', v) }
  const [showParked, setShowParked] = useState(false)
  const [resume, setResume] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showDone, setShowDoneState] = useState(getPrefs().apShowDone)
  const [sort, setSortState] = useState(getPrefs().apSort)
  const setShowDone = (v: boolean): void => { setShowDoneState(v); setPref('apShowDone', v) }
  const setSort = (v: typeof sort): void => { setSortState(v); setPref('apSort', v) }
  useEffect(() => {
    apMemory.set(memKey, { mission, events, issues, details, showDone })
  }, [memKey, mission, events, issues, details, showDone])
  const wiring = helper?.projects.find((p) => p.projectId === project.id)
  const session = liveSessionName(project.name)
  const dir = project.dir

  // The store keeps this project synced in the background; here we mirror it.
  const load = useCallback(async () => {
    await autopilot.refresh(server.id, project.id)
  }, [server.id, project.id])
  useEffect(() => {
    autopilot.ensure(server.id, project.id, project.dir)
    const apply = (): void => {
      const d = autopilot.get(server.id, project.id)
      if (!d) return
      setMission(d.mission)
      setMissionDraft((cur) => (cur === '' || cur === mission ? d.mission : cur))
      setEvents(d.events)
      setIssues(d.issues)
      setRecs(d.recs)
      setError(d.error ?? null)
    }
    apply()
    return autopilot.subscribe(server.id, project.id, apply)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server.id, project.id, project.dir])

  const act = async (label: string, fn: () => Promise<unknown>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      await fn()
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }
  const send = (text: string): Promise<void> => api.helper.startSession(server.id, project.id, resume).then((name) => api.helper.sendToSession(server.id, name, text))

  const open = issues.filter((i) => i.status === 'open')
  const rank: Record<string, number> = { urgent: 0, high: 1, normal: 2, low: 3 }
  const shown = [...(showDone ? issues : open)].sort((a, b) => {
    if (sort === 'newest') return b.createdAt - a.createdAt
    if (sort === 'oldest') return a.createdAt - b.createdAt
    if (sort === 'number') return a.n - b.n
    if (sort === 'urgency') return (rank[a.urgency] ?? 2) - (rank[b.urgency] ?? 2) || b.updatedAt - a.updatedAt
    // needs you first, then urgency, then newest
    if ((a.status === 'open') !== (b.status === 'open')) return a.status === 'open' ? -1 : 1
    if (a.needsBoss !== b.needsBoss) return a.needsBoss ? -1 : 1
    return (rank[a.urgency] ?? 2) - (rank[b.urgency] ?? 2) || b.updatedAt - a.updatedAt
  })
  const openRecs = recs.filter((r) => r.status === 'open')
  const parkedRecs = recs.filter((r) => r.status === 'later')
  const shownRecs = showParked ? recs.filter((r) => r.status === 'open' || r.status === 'later') : openRecs
  const setRec = (r: number, status: string): void => {
    void act(`rec-${r}`, () => api.helper.setRecommendation(server.id, project.id, r, status).then(setRecs))
  }
  const recent = useMemo(() => [...events].reverse().slice(0, 120), [events])
  const lastReply = [...events].reverse().find((e) => e.type === 'reply')
  const billing = lastReply?.billing

  return (
    <div className="ap">
      {error && <p className="error-text">{error}</p>}
      {!helper?.installed && <p className="field-hint">Install the ClawHQ helper on the Health tab first.</p>}
      {helper?.installed && !wiring?.wired && (
        <p className="field-hint ap-wire">
          <b>{project.name}</b> is not wired yet (hooks and tools in the project).{' '}
          <button className="btn btn-sm btn-primary" disabled={busy !== null} onClick={() => void act('wire', () => api.helper.wire(server.id, project.id).then(onHelper))}>
            {busy === 'wire' ? 'Wiring…' : 'Wire project'}
          </button>
        </p>
      )}

      <section className="ap-card">
        <div className="ap-head">
          <span className="seg ap-seg" role="tablist">
            <button className={view === 'issues' ? 'is-active' : ''} onClick={() => setView('issues')}>Issues{open.length > 0 ? ` · ${open.length}` : ''}</button>
            <button className={view === 'recs' ? 'is-active' : ''} onClick={() => setView('recs')} title="Optional ideas from the session: take them, park them or dismiss them">Recommendations{openRecs.length > 0 ? ` · ${openRecs.length}` : ''}</button>
          </span>
          <span className="plugin-desc">{view === 'issues' ? 'numbered by the session' : 'optional · nothing here needs doing'}</span>
          <span className="row-tools">
            <button className="icon-btn" title="Refresh" disabled={busy === 'refresh'} onClick={() => { setBusy('refresh'); void load().finally(() => setBusy(null)) }}>
              <RefreshIcon />
            </button>
            {view === 'issues' ? (
              <>
                <select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label="Sort issues" className="issue-sort">
                  <option value="needs">Needs you first</option>
                  <option value="urgency">Urgency</option>
                  <option value="newest">Newest</option>
                  <option value="oldest">Oldest</option>
                  <option value="number">Number</option>
                </select>
                <Toggle on={showDone} onChange={setShowDone} label="Show done" />
              </>
            ) : (
              <Toggle on={showParked} onChange={setShowParked} label={`Show parked${parkedRecs.length ? ` (${parkedRecs.length})` : ''}`} />
            )}
          </span>
        </div>
        {view === 'recs' && shownRecs.length === 0 && <p className="field-hint">No recommendations{parkedRecs.length && !showParked ? ` (${parkedRecs.length} parked)` : ''}.</p>}
        {view === 'recs' && (
          <div className="ap-issues">
            {shownRecs.map((r) => (
              <div key={r.r} className={`ap-issue ap-rec${r.status === 'later' ? ' is-done' : ''}`}>
                <button className="ap-issue-main" onClick={() => void api.helper.recommendationDetails(server.id, project.id, r.r).then((md) => setDetails({ n: r.r, md, kind: 'rec' })).catch((err) => setError(String(err)))}>
                  <span className="ap-n">R{r.r}</span>
                  <span className="ap-title">{r.title}</span>
                  <span className="ap-meta">{r.status === 'later' ? 'parked · ' : ''}{r.effort} · {when(r.updatedAt)}</span>
                </button>
                <span className="ap-issue-tools">
                  <button className="btn btn-sm btn-primary" disabled={busy !== null} title="Turn it into a numbered issue the session works on" onClick={() => setRec(r.r, 'accepted')}>Do it</button>
                  {r.status === 'later' ? (
                    <button className="btn btn-sm" disabled={busy !== null} onClick={() => setRec(r.r, 'open')}>Unpark</button>
                  ) : (
                    <button className="btn btn-sm" disabled={busy !== null} title="Keep it, out of the way" onClick={() => setRec(r.r, 'later')}>Later</button>
                  )}
                  <button className="btn btn-sm btn-ghost" disabled={busy !== null} title="Drop it; the session will not suggest it again" onClick={() => setRec(r.r, 'dismissed')}>Dismiss</button>
                </span>
              </div>
            ))}
          </div>
        )}
        {view === 'issues' && shown.length === 0 && <p className="field-hint">Nothing waiting.</p>}
        {view === 'issues' && <div className="ap-issues">
          {shown.map((i) => (
            <div key={i.n} className={`ap-issue${i.status !== 'open' ? ' is-done' : ''}${i.needsBoss ? ' needs-boss' : ''}`}>
              <button className="ap-issue-main" onClick={() => void api.helper.issueDetails(server.id, project.id, i.n).then((md) => setDetails({ n: i.n, md })).catch((err) => setError(String(err)))}>
                <span className="ap-n">#{i.n}</span>
                <span className="ap-title">{i.title}</span>
                <span className="ap-meta">
                  {i.status !== 'open' ? i.status : i.needsBoss ? 'needs you' : 'note'} · {i.urgency} · {when(i.updatedAt)}
                </span>
              </button>
              <span className="ap-issue-tools">
                <button className="btn btn-sm" onClick={() => onDiscuss(`#${i.n}: `)} title="Talk to the live session about this issue">Discuss</button>
                <button className="btn btn-sm btn-ghost" onClick={() => onDiscuss(`Fix #${i.n} now. When done, mark it done with clawhq_issue_update and log one line.`)} title="Tell the session to fix it">Fix</button>
              </span>
            </div>
          ))}
        </div>}
        {details && (
          <div className="ap-details">
            <div className="ap-head">
              <h4>{details.kind === 'rec' ? `R${details.n}` : `#${details.n}`} details</h4>
              <button className="btn btn-sm btn-ghost" onClick={() => setDetails(null)}>Close</button>
            </div>
            <div className="team-md" dangerouslySetInnerHTML={{ __html: renderMarkdown(details.md || '_No details file._') }} />
          </div>
        )}
      </section>

      <section className="ap-card">
        <div className="ap-head">
          <h3>Log</h3>
          <span className="plugin-desc">last three days · short lines from the session, newest first</span>
        </div>
        {recent.length === 0 && <p className="field-hint">Nothing logged yet.</p>}
        <div className="ap-log">
          {recent.map((e, i) => (
            <div key={`${e.ts}-${i}`} className={`ap-line is-${e.type}${e.kind === 'warn' ? ' is-warn' : ''}`}>
              <span className="ap-time">{when(e.ts)}</span>
              <span className="ap-text">
                {e.type === 'log' && <>{e.kind === 'change' ? '🔧 ' : e.kind === 'warn' ? '⚠️ ' : '· '}{e.text}</>}
                {e.type === 'issue' && <>🔴 opened #{e.n}: {e.title}{e.needsBoss ? ' (needs you)' : ''}</>}
                {e.type === 'issue-update' && <>{e.status === 'done' ? '✅' : e.status === 'dismissed' ? '⬜' : '✎'} #{e.n} {e.status}{e.note ? `: ${e.note}` : ''}</>}
                {e.type === 'recommendation' && <>💡 R{e.r} suggested: {e.title}{e.effort ? ` (${e.effort})` : ''}</>}
                {e.type === 'rec-update' && <>💡 R{e.r} {e.status === 'accepted' ? `accepted → #${e.n}` : e.status === 'later' ? 'parked' : e.status === 'open' ? 'unparked' : e.status}</>}
                {e.type === 'reply' && <span className="ap-reply" title={e.text}>💬 {(e.text || '').split('\n')[0].slice(0, 160)}{e.files?.length ? ` · ${e.files.length} file${e.files.length === 1 ? '' : 's'} edited` : ''}</span>}
                {e.type === 'session-end' && <>⏹ session ended</>}
              </span>
            </div>
          ))}
        </div>
      </section>
      <div className="ap-grid">
        <section className="ap-card ap-mission">
          <div className="ap-head">
            <h3>Mission</h3>
            <span className="plugin-desc">.clawhq/MISSION.md · the session reads it with clawhq_mission</span>
            <button className="btn btn-sm btn-primary" disabled={busy !== null || missionDraft === mission} onClick={() => void act('mission', () => api.helper.setMission(server.id, project.id, missionDraft))}>
              {busy === 'mission' ? 'Saving…' : 'Save'}
            </button>
          </div>
          <textarea className="ap-textarea" value={missionDraft} onChange={(e) => setMissionDraft(e.target.value)} spellCheck={false} placeholder={wiring?.wired ? 'Empty mission.' : 'Wire the project to get a mission template.'} />
        </section>

        <section className="ap-card ap-session">
          <div className="ap-head">
            <h3>Live session</h3>
            <span className="plugin-desc mono">tmux: {session}</span>
          </div>
          <p className="field-hint">An interactive Claude Code in tmux on the server, started in <code>{dir || '~'}</code>, so it keeps its memory and its own hourly schedule between your visits. Start fresh, or resume the session you have been talking to: paste its id and ClawHQ runs <code>claude --resume &lt;id&gt;</code> there. Once the tmux session exists, the id is not needed again.</p>
          <div className="ap-row">
            <input value={resume} onChange={(e) => setResume(e.target.value)} placeholder="Claude session id to resume (optional)" spellCheck={false} />
          </div>
          <div className="btn-row">
            <button className="btn btn-primary" disabled={busy !== null || !mission.trim()} title="Starts the session if needed and sends the mission with the instruction to schedule itself hourly" onClick={() => void act('start', () => send(START_INSTRUCTION(mission)))}>
              {busy === 'start' ? 'Sending…' : 'Start mission'}
            </button>
            <button className="btn" disabled={busy !== null} onClick={() => void act('pause', () => send(PAUSE_INSTRUCTION))}>Pause</button>
            <button className="btn" disabled={busy !== null} onClick={() => void act('resume', () => send(RESUME_INSTRUCTION))}>Resume</button>
            <button
              className="btn"
              disabled={busy !== null}
              title="Attach a terminal tab to the live session"
              onClick={() => void act('term', () => api.helper.startSession(server.id, project.id, resume).then((name) => { terminals.create(server.id, project.id, dir, true, name, 'Live session') }))}
            >
              Open in terminal
            </button>
          </div>
          {lastReply && (
            <p className="field-hint">
              Last reply {when(lastReply.ts)}{lastReply.model ? ` · ${lastReply.model}` : ''}
              {lastReply.usage ? ` · ${tokens(lastReply.usage.input + lastReply.usage.output)} tokens` : ''}
              {billing === 'subscription' ? ' · subscription' : billing === 'api' ? ' · API billing' : ''}
            </p>
          )}
        </section>
      </div>

    </div>
  )
}
