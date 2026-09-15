import { api } from '../api'

/**
 * The gateway's config and the JSON schema that describes it.
 *
 * The gateway ships its full schema with UI hints, and its Control UI is generated
 * from it. ClawHQ does the same: settings pages for plugins, MCP servers and channels
 * are rendered from this schema, so they keep up with the gateway without a release.
 *
 * Reads go through `config.get` (redacted secrets arrive as a marker); writes go
 * through `config.patch`, a JSON merge patch checked against the hash we last read.
 */

export type JsonSchema = {
  type?: string | string[]
  title?: string
  description?: string
  properties?: Record<string, JsonSchema>
  additionalProperties?: boolean | JsonSchema
  propertyNames?: JsonSchema
  items?: JsonSchema
  enum?: unknown[]
  const?: unknown
  anyOf?: JsonSchema[]
  oneOf?: JsonSchema[]
  default?: unknown
  minimum?: number
  maximum?: number
  exclusiveMinimum?: number
  required?: string[]
}

export type UiHint = {
  label?: string
  help?: string
  advanced?: boolean
  sensitive?: boolean
  placeholder?: string
  docsUrl?: string
  tags?: string[]
}

/** What the gateway sends in place of a secret. */
export const REDACTED = '__OPENCLAW_REDACTED__'

type Snapshot = {
  config: Record<string, unknown>
  hash: string
}

class GatewayConfig {
  private schema: JsonSchema | null = null
  private hints: Record<string, UiHint> = {}
  private snapshot: Snapshot | null = null
  private schemaPromise: Promise<void> | null = null

  /** Loads the schema once per session and the config on every call. */
  async load(): Promise<Snapshot> {
    if (!this.schema) {
      this.schemaPromise ??= (async () => {
        const res = await api.rpc.request<{ schema: JsonSchema; uiHints?: Record<string, UiHint> }>(
          'config.schema'
        )
        this.schema = res.schema
        this.hints = res.uiHints ?? {}
      })()
      await this.schemaPromise
    }
    const res = await api.rpc.request<{ config?: Record<string, unknown>; hash?: string }>('config.get')
    this.snapshot = { config: res.config ?? {}, hash: res.hash ?? '' }
    return this.snapshot
  }

  /** The schema node for a config path, walking maps and arrays. */
  lookup(path: string[]): JsonSchema | undefined {
    let node: JsonSchema | undefined = this.schema ?? undefined
    for (const seg of path) {
      if (!node) return undefined
      if (node.properties && seg in node.properties) {
        node = node.properties[seg]
      } else if (typeof node.additionalProperties === 'object') {
        node = node.additionalProperties
      } else if (node.items && /^\d+$/.test(seg)) {
        node = node.items
      } else {
        return undefined
      }
    }
    return node
  }

  /**
   * The UI hint for a path. Hints are keyed with `*` for map entries, so
   * `mcp.servers.docs.command` is answered by `mcp.servers.*.command`.
   */
  hint(path: string[]): UiHint | undefined {
    const exact = this.hints[path.join('.')]
    if (exact) return exact
    // Try every combination of wildcarded segments, most specific first.
    const n = path.length
    for (let mask = 1; mask < 1 << n; mask++) {
      const key = path.map((seg, i) => ((mask >> i) & 1 ? '*' : seg)).join('.')
      const hit = this.hints[key]
      if (hit) return hit
    }
    return undefined
  }

  /** Reads a value out of the last loaded config. */
  get(path: string[]): unknown {
    let cur: unknown = this.snapshot?.config
    for (const seg of path) {
      if (cur === null || typeof cur !== 'object') return undefined
      cur = (cur as Record<string, unknown>)[seg]
    }
    return cur
  }

  /**
   * Writes one value. `null` removes the key, as JSON merge patch defines it. The
   * patch is checked against the hash from the last read, so an edit made elsewhere
   * in between fails loudly instead of being overwritten.
   */
  async set(path: string[], value: unknown, note?: string): Promise<Snapshot> {
    if (!this.snapshot) await this.load()
    if (path.length === 0) throw new Error('a config path is required')
    const patch: Record<string, unknown> = {}
    let cursor = patch
    path.forEach((seg, i) => {
      if (i === path.length - 1) {
        cursor[seg] = value
      } else {
        const next: Record<string, unknown> = {}
        cursor[seg] = next
        cursor = next
      }
    })
    await api.rpc.request('config.patch', {
      raw: JSON.stringify(patch),
      baseHash: this.snapshot?.hash ?? '',
      note: note ?? `ClawHQ: ${path.join('.')}`
    })
    return this.load()
  }

  /** Label for a path: the hint's label, then the schema title, then the last segment. */
  label(path: string[]): string {
    return this.hint(path)?.label ?? this.lookup(path)?.title ?? humanize(path[path.length - 1] ?? '')
  }
}

export const gatewayConfig = new GatewayConfig()

/** camelCase or kebab-case key to a readable label. */
export function humanize(key: string): string {
  const spaced = key
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[-_]+/g, ' ')
    .trim()
  return spaced ? spaced[0].toUpperCase() + spaced.slice(1) : key
}

/** The effective simple type of a schema node, resolving const/enum unions. */
export function schemaKind(
  s: JsonSchema | undefined
): 'boolean' | 'string' | 'number' | 'enum' | 'string-list' | 'string-map' | 'object' | 'json' {
  if (!s) return 'json'
  const options = enumOptions(s)
  if (options) return 'enum'
  const t = Array.isArray(s.type) ? s.type.find((x) => x !== 'null') : s.type
  switch (t) {
    case 'boolean':
      return 'boolean'
    case 'string':
      return 'string'
    case 'number':
    case 'integer':
      return 'number'
    case 'array':
      return s.items?.type === 'string' ? 'string-list' : 'json'
    case 'object':
      if (s.properties) return 'object'
      if (typeof s.additionalProperties === 'object' && s.additionalProperties.type === 'string') {
        return 'string-map'
      }
      return 'json'
    default:
      return 'json'
  }
}

/** Enum values, whether declared as `enum` or as a union of consts. */
export function enumOptions(s: JsonSchema | undefined): unknown[] | null {
  if (!s) return null
  if (Array.isArray(s.enum)) return s.enum
  const union = s.anyOf ?? s.oneOf
  if (union && union.length > 0 && union.every((u) => 'const' in u)) {
    return union.map((u) => u.const)
  }
  return null
}
