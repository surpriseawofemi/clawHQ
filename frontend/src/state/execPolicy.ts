import { api } from '../api'

/**
 * A machine's exec mode, as three choices rather than a policy file.
 *
 *   auto    agents run anything on that machine without asking
 *   semi    a short list of read-only programs runs freely; the rest asks you
 *   manual  every command asks
 *
 * The file the gateway pushes to a node (`exec.approvals.node.set`) is the same
 * shape the node host reads from ~/.openclaw/exec-approvals.json. A ClawHQ desktop
 * maps it onto its own off / ask / allow switch.
 */
export type MachineMode = 'auto' | 'semi' | 'manual'

/** Programs a semi-auto machine runs without asking: reads, status, logs. */
export const SAFE_PROGRAMS = [
  'cat', 'head', 'tail', 'less', 'grep', 'wc', 'ls', 'find', 'stat', 'du', 'df', 'free', 'uptime', 'uname',
  'hostname', 'whoami', 'id', 'env', 'ps', 'top', 'pgrep', 'lsof', 'ss', 'netstat', 'dig', 'nslookup', 'curl',
  'journalctl', 'systemctl', 'docker', 'git', 'node', 'npm', 'nginx'
]

export function policyFile(mode: MachineMode): Record<string, unknown> {
  if (mode === 'auto') {
    return { version: 1, defaults: { security: 'full', ask: 'off' }, agents: {} }
  }
  const defaults = { security: 'allowlist', ask: 'on-miss', askFallback: 'deny' }
  if (mode === 'manual') return { version: 1, defaults, agents: {} }
  const allowlist = SAFE_PROGRAMS.flatMap((p) => [{ pattern: `/usr/bin/${p}` }, { pattern: `/bin/${p}` }, { pattern: `/usr/local/bin/${p}` }, { pattern: `/opt/homebrew/bin/${p}` }])
  return { version: 1, defaults, agents: { '*': { ...defaults, allowlist } } }
}

/** Reads a mode back out of whatever file the node reports. */
export function modeOf(file: unknown): MachineMode | 'custom' {
  const f = (file ?? {}) as { defaults?: { security?: string; ask?: string }; agents?: Record<string, { security?: string; allowlist?: unknown[] }> }
  const security = f.defaults?.security ?? Object.values(f.agents ?? {})[0]?.security
  if (security === 'full') return 'auto'
  if (security === 'allowlist' || security === undefined) {
    const lists = Object.values(f.agents ?? {}).map((a) => a.allowlist?.length ?? 0)
    if (lists.every((n) => n === 0)) return 'manual'
    const star = f.agents?.['*']?.allowlist?.length ?? 0
    return star > 0 ? 'semi' : 'custom'
  }
  return 'custom'
}

export async function readMode(nodeId: string): Promise<MachineMode | 'custom' | 'unknown'> {
  try {
    const res = await api.rpc.request<{ file?: unknown; exists?: boolean }>('exec.approvals.node.get', { nodeId })
    if (!res?.exists && !res?.file) return 'manual'
    return modeOf(res.file)
  } catch {
    return 'unknown'
  }
}

export async function setMode(nodeId: string, mode: MachineMode): Promise<void> {
  await api.rpc.request('exec.approvals.node.set', { nodeId, file: policyFile(mode) })
}
