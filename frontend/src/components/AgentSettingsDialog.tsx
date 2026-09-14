import { useEffect, useState } from 'react'
import type { Agent, ClawHQConfig } from '../types'
import { agentLabel } from '../types'
import { api } from '../api'

type Props = {
  agent: Agent
  allAgents: Agent[]
  config: ClawHQConfig | null
  onClose: () => void
  onSaved: () => void
  onConfigChanged: (config: ClawHQConfig) => void
}

type Draft = {
  name: string
  emoji: string
  theme: string
  model: string
  thinkingDefault: string
  departmentId: string
  allowAgents: string[]
}

export function AgentSettingsDialog({
  agent,
  allAgents,
  config,
  onClose,
  onSaved,
  onConfigChanged
}: Props): React.JSX.Element {
  const [draft, setDraft] = useState<Draft>({
    name: agent.identity?.name ?? '',
    emoji: agent.identity?.emoji ?? '',
    theme: agent.identity?.theme ?? '',
    model: agent.model?.primary ?? '',
    thinkingDefault: agent.thinkingDefault ?? '',
    departmentId: config?.assignments?.[agent.id] ?? '',
    allowAgents: []
  })
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [models, setModels] = useState<string[]>([])

  // The roster and the agent's current delegation list come from different RPCs than
  // agents.list, so fetch them when the dialog opens.
  useEffect(() => {
    let live = true
    // config.get takes no path — it returns the whole config, already parsed under
    // `config` (with `raw` as the on-disk text alongside it).
    api.rpc
      .request<any>('config.get', {})
      .then((res) => {
        if (!live) return
        const parsed = res?.config ?? (res?.raw ? JSON.parse(res.raw) : null)
        const allow = parsed?.agents?.entries?.[agent.id]?.subagents?.allowAgents
        setDraft((d) => ({ ...d, allowAgents: Array.isArray(allow) ? allow : [] }))
      })
      .catch(() => undefined)

    api.rpc
      .request<any>('models.list', {})
      .then((res) => {
        if (!live) return
        const list: string[] = (res?.models ?? res?.items ?? [])
          .map((m: any) => m?.id ?? m?.model)
          .filter((id: unknown): id is string => typeof id === 'string')
        setModels([...new Set(list)].sort())
      })
      .catch(() => undefined)

    return () => {
      live = false
    }
  }, [agent.id])

  const toggleDelegate = (id: string): void =>
    setDraft((d) => ({
      ...d,
      allowAgents: d.allowAgents.includes(id)
        ? d.allowAgents.filter((x) => x !== id)
        : [...d.allowAgents, id]
    }))

  const save = async (): Promise<void> => {
    setSaving(true)
    setError(null)
    try {
      // Department lives in ClawHQ's own config; everything else is real OpenClaw config.
      const nextConfig = await api.config.assignAgent(
        agent.id,
        draft.departmentId || null
      )
      onConfigChanged(nextConfig)

      const patch: Record<string, unknown> = {
        agentId: agent.id,
        identity: {
          ...(draft.name.trim() ? { name: draft.name.trim() } : {}),
          ...(draft.emoji.trim() ? { emoji: draft.emoji.trim() } : {}),
          ...(draft.theme.trim() ? { theme: draft.theme.trim() } : {})
        }
      }
      if (draft.model.trim()) patch.model = draft.model.trim()
      if (draft.thinkingDefault) patch.thinkingDefault = draft.thinkingDefault
      patch.subagents = { allowAgents: draft.allowAgents }

      await api.rpc.request('agents.update', patch)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const others = allAgents.filter((a) => a.id !== agent.id)

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>
            {draft.emoji || '🤖'} {agentLabel(agent)}
          </h2>
          <button className="icon-btn" onClick={onClose}>
            ✕
          </button>
        </header>

        <div className="modal-body">
          <div className="field-row">
            <label className="field">
              <span>Display name</span>
              <input
                value={draft.name}
                placeholder={agent.id}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label className="field field-narrow">
              <span>Emoji</span>
              <input
                value={draft.emoji}
                placeholder="🤖"
                onChange={(e) => setDraft({ ...draft, emoji: e.target.value })}
              />
            </label>
          </div>

          <label className="field">
            <span>Personality / theme</span>
            <textarea
              rows={3}
              value={draft.theme}
              placeholder="How this agent should carry itself."
              onChange={(e) => setDraft({ ...draft, theme: e.target.value })}
            />
          </label>

          <div className="field-row">
            <label className="field">
              <span>Model</span>
              <input
                list={`models-${agent.id}`}
                value={draft.model}
                placeholder="anthropic/claude-opus-5"
                onChange={(e) => setDraft({ ...draft, model: e.target.value })}
              />
              <datalist id={`models-${agent.id}`}>
                {models.map((m) => (
                  <option key={m} value={m} />
                ))}
              </datalist>
            </label>
            <label className="field field-narrow">
              <span>Thinking</span>
              <select
                value={draft.thinkingDefault}
                onChange={(e) => setDraft({ ...draft, thinkingDefault: e.target.value })}
              >
                <option value="">default</option>
                {(agent.thinkingOptions ?? []).map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <label className="field">
            <span>Department</span>
            <select
              value={draft.departmentId}
              onChange={(e) => setDraft({ ...draft, departmentId: e.target.value })}
            >
              <option value="">Unassigned</option>
              {(config?.departments ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.emoji} {d.name}
                </option>
              ))}
            </select>
          </label>

          <div className="field">
            <span>Can delegate to</span>
            <p className="field-hint">
              Who this agent may hand work to as sub-agents — this is the reporting line.
            </p>
            <div className="chip-grid">
              {others.map((other) => {
                const on = draft.allowAgents.includes(other.id)
                return (
                  <button
                    key={other.id}
                    className={`chip${on ? ' chip-on' : ''}`}
                    onClick={() => toggleDelegate(other.id)}
                    type="button"
                  >
                    {other.identity?.emoji ?? '🤖'} {agentLabel(other)}
                  </button>
                )
              })}
              {others.length === 0 && <p className="field-hint">No other agents configured.</p>}
            </div>
          </div>

          {error && <p className="error-text">{error}</p>}
        </div>

        <footer className="modal-foot">
          <span className="modal-note">Workspace: {agent.workspace ?? 'not set'}</span>
          <div className="modal-actions">
            <button className="btn" onClick={onClose}>
              Cancel
            </button>
            <button className="btn btn-primary" onClick={save} disabled={saving}>
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </footer>
      </div>
    </div>
  )
}
