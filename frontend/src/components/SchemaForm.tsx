import { useEffect, useState } from 'react'
import {
  REDACTED,
  enumOptions,
  gatewayConfig,
  humanize,
  schemaKind,
  type JsonSchema
} from '../state/gatewayConfig'

type Props = {
  /** Config path of the object being edited, e.g. ['plugins','entries','x','config']. */
  path: string[]
  /** Current value at that path; undefined renders defaults. */
  value: unknown
  /** Called with the full path of the changed leaf and its new value (null removes). */
  onChange: (path: string[], value: unknown) => Promise<void>
  /** Show fields the gateway marks as advanced. */
  showAdvanced?: boolean
  /** Nesting depth, for indentation. */
  depth?: number
}

/**
 * Renders an object from the gateway's config schema as a form.
 *
 * Every field type the gateway uses in plugin and MCP config is covered: switches,
 * text, numbers, enums, string lists, string maps and nested objects. Anything else
 * falls back to a JSON box so nothing is unreachable. Leaves write on commit, one
 * `config.patch` per change, which keeps the hash check meaningful.
 */
export function SchemaForm({ path, value, onChange, showAdvanced = false, depth = 0 }: Props): React.JSX.Element {
  const schema = gatewayConfig.lookup(path)
  const props = schema?.properties ?? {}
  const obj = (value && typeof value === 'object' ? value : {}) as Record<string, unknown>
  const keys = Object.keys(props)

  if (keys.length === 0) {
    return (
      <JsonField path={path} value={value} onChange={onChange} label={gatewayConfig.label(path)} />
    )
  }

  return (
    <div className={`sf-group${depth > 0 ? ' sf-nested' : ''}`}>
      {keys.map((key) => {
        const childPath = [...path, key]
        const hint = gatewayConfig.hint(childPath)
        if (hint?.advanced && !showAdvanced) return null
        const child = props[key]
        const kind = schemaKind(child)
        const label = gatewayConfig.label(childPath)
        const help = hint?.help ?? child?.description

        if (kind === 'object') {
          return (
            <details key={key} className="sf-section" open={depth === 0 && Object.keys(obj[key] ?? {}).length > 0}>
              <summary>
                <span>{label}</span>
                {help && <small className="field-hint">{help}</small>}
              </summary>
              <SchemaForm
                path={childPath}
                value={obj[key]}
                onChange={onChange}
                showAdvanced={showAdvanced}
                depth={depth + 1}
              />
            </details>
          )
        }

        return (
          <Field
            key={key}
            kind={kind}
            schema={child}
            path={childPath}
            label={label}
            help={help}
            sensitive={hint?.sensitive}
            value={obj[key]}
            onChange={onChange}
          />
        )
      })}
    </div>
  )
}

type FieldProps = {
  kind: ReturnType<typeof schemaKind>
  schema: JsonSchema | undefined
  path: string[]
  label: string
  help?: string
  sensitive?: boolean
  value: unknown
  onChange: (path: string[], value: unknown) => Promise<void>
}

function Field({ kind, schema, path, label, help, sensitive, value, onChange }: FieldProps): React.JSX.Element {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const commit = async (next: unknown): Promise<void> => {
    setBusy(true)
    setError(null)
    try {
      await onChange(path, next)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const id = `sf-${path.join('-')}`

  switch (kind) {
    case 'boolean':
      return (
        <div className="sf-field sf-inline">
          <label className="check-row" htmlFor={id}>
            <input
              id={id}
              type="checkbox"
              checked={value === true}
              disabled={busy}
              onChange={(e) => void commit(e.target.checked)}
            />
            <span>{label}</span>
          </label>
          {help && <p className="field-hint">{help}</p>}
          {error && <p className="error-text">{error}</p>}
        </div>
      )
    case 'enum': {
      const options = enumOptions(schema) ?? []
      return (
        <label className="sf-field field" htmlFor={id}>
          <span>{label}</span>
          <select
            id={id}
            value={value === undefined || value === null ? '' : String(value)}
            disabled={busy}
            onChange={(e) => void commit(e.target.value === '' ? null : e.target.value)}
          >
            <option value="">(not set)</option>
            {options.map((o) => (
              <option key={String(o)} value={String(o)}>
                {String(o)}
              </option>
            ))}
          </select>
          {help && <p className="field-hint">{help}</p>}
          {error && <p className="error-text">{error}</p>}
        </label>
      )
    }
    case 'number':
      return (
        <TextField
          id={id}
          label={label}
          help={help}
          value={value === undefined || value === null ? '' : String(value)}
          busy={busy}
          error={error}
          type="number"
          onCommit={(text) => commit(text.trim() === '' ? null : Number(text))}
        />
      )
    case 'string':
      return (
        <TextField
          id={id}
          label={label}
          help={help}
          value={value === REDACTED ? '' : value === undefined || value === null ? '' : String(value)}
          placeholder={value === REDACTED ? 'set on the gateway (enter a new value to replace)' : undefined}
          busy={busy}
          error={error}
          type={sensitive || value === REDACTED ? 'password' : 'text'}
          onCommit={(text) => commit(text === '' ? null : text)}
        />
      )
    case 'string-list':
      return (
        <TextField
          id={id}
          label={label}
          help={(help ? help + ' ' : '') + 'One per line.'}
          value={Array.isArray(value) ? (value as unknown[]).map(String).join('\n') : ''}
          busy={busy}
          error={error}
          multiline
          onCommit={(text) => {
            const list = text
              .split('\n')
              .map((s) => s.trim())
              .filter(Boolean)
            return commit(list.length === 0 ? null : list)
          }}
        />
      )
    case 'string-map':
      return <StringMapField id={id} label={label} help={help} value={value} busy={busy} error={error} commit={commit} />
    default:
      return <JsonField path={path} value={value} onChange={onChange} label={label} help={help} />
  }
}

type TextProps = {
  id: string
  label: string
  help?: string
  value: string
  placeholder?: string
  busy: boolean
  error: string | null
  type?: 'text' | 'number' | 'password'
  multiline?: boolean
  onCommit: (text: string) => Promise<void>
}

/** Text input that writes on blur or Enter, never on every keystroke. */
function TextField({ id, label, help, value, placeholder, busy, error, type = 'text', multiline, onCommit }: TextProps): React.JSX.Element {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  const dirty = draft !== value

  const save = (): void => {
    if (dirty) void onCommit(draft)
  }

  return (
    <label className="sf-field field" htmlFor={id}>
      <span>
        {label}
        {dirty && <em className="sf-dirty"> unsaved, press Enter</em>}
      </span>
      {multiline ? (
        <textarea
          id={id}
          className="mono"
          rows={Math.min(8, Math.max(2, draft.split('\n').length))}
          value={draft}
          placeholder={placeholder}
          disabled={busy}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={save}
        />
      ) : (
        <input
          id={id}
          className={type === 'number' ? undefined : 'mono'}
          type={type}
          value={draft}
          placeholder={placeholder}
          disabled={busy}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={save}
          onKeyDown={(e) => {
            if (e.key === 'Enter') save()
          }}
        />
      )}
      {help && <p className="field-hint">{help}</p>}
      {error && <p className="error-text">{error}</p>}
    </label>
  )
}

type MapProps = {
  id: string
  label: string
  help?: string
  value: unknown
  busy: boolean
  error: string | null
  commit: (next: unknown) => Promise<void>
}

/** KEY=VALUE lines for string maps such as env or headers. */
function StringMapField({ id, label, help, value, busy, error, commit }: MapProps): React.JSX.Element {
  const entries = value && typeof value === 'object' ? Object.entries(value as Record<string, unknown>) : []
  const text = entries.map(([k, v]) => `${k}=${v === REDACTED ? '' : String(v)}`).join('\n')
  return (
    <TextField
      id={id}
      label={label}
      help={(help ? help + ' ' : '') + 'One KEY=value per line.'}
      value={text}
      busy={busy}
      error={error}
      multiline
      onCommit={(next) => {
        const map: Record<string, string> = {}
        for (const line of next.split('\n')) {
          const eq = line.indexOf('=')
          if (eq <= 0) continue
          map[line.slice(0, eq).trim()] = line.slice(eq + 1)
        }
        return commit(Object.keys(map).length === 0 ? null : map)
      }}
    />
  )
}

type JsonProps = {
  path: string[]
  value: unknown
  label: string
  help?: string
  onChange: (path: string[], value: unknown) => Promise<void>
}

/** Raw JSON for shapes the form does not model; still validated by the gateway. */
function JsonField({ path, value, label, help, onChange }: JsonProps): React.JSX.Element {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const pretty = value === undefined ? '' : JSON.stringify(value, null, 2)
  const [draft, setDraft] = useState(pretty)
  useEffect(() => setDraft(pretty), [pretty])

  const save = async (): Promise<void> => {
    if (draft === pretty) return
    setBusy(true)
    setError(null)
    try {
      const parsed = draft.trim() === '' ? null : JSON.parse(draft)
      await onChange(path, parsed)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <label className="sf-field field" htmlFor={`sf-${path.join('-')}`}>
      <span>
        {label} <em className="sf-dirty">JSON</em>
      </span>
      <textarea
        id={`sf-${path.join('-')}`}
        className="mono"
        rows={Math.min(12, Math.max(3, draft.split('\n').length))}
        value={draft}
        disabled={busy}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => void save()}
      />
      {help && <p className="field-hint">{help}</p>}
      {error && <p className="error-text">{error}</p>}
    </label>
  )
}

export { humanize }
