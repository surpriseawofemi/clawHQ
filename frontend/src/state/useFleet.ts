import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type {
  Agent,
  ChatMessage,
  ClawHQConfig,
  ConnectionStatus,
  DaemonStatus,
  RemoteNode,
  SessionInfo,
  StreamingReply
} from '../types'

import { api as clawhqApi } from '../api'

/** Kept as a function so existing `api().x` call sites read unchanged. */
const api = (): typeof clawhqApi => clawhqApi

/** The main session key for an agent — the conversation ClawHQ opens on click. */
export const mainSessionKey = (agentId: string): string => `agent:${agentId}:main`

export type Fleet = ReturnType<typeof useFleet>

export function useFleet() {
  const [status, setStatus] = useState<ConnectionStatus>({
    phase: 'idle',
    gatewayId: '',
    url: null,
    scopes: [],
    deviceId: null,
    serverVersion: null,
    error: null,
    paired: false
  })
  const [daemon, setDaemon] = useState<DaemonStatus | null>(null)
  const [config, setConfig] = useState<ClawHQConfig | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [desktops, setDesktops] = useState<RemoteNode[]>([])
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Record<string, ChatMessage[]>>({})
  const [streaming, setStreaming] = useState<StreamingReply | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  /** Sessions we've already subscribed to on this connection. */
  const subscribed = useRef<Set<string>>(new Set())

  const connected = status.phase === 'connected'
  const selectedKey = selectedAgentId ? mainSessionKey(selectedAgentId) : null

  // ---- bootstrap --------------------------------------------------------
  useEffect(() => {
    api().config.get().then(setConfig).catch(() => undefined)
    api().connection.status().then(setStatus).catch(() => undefined)
    api().daemon.status().then(setDaemon).catch(() => undefined)

    const offStatus = api().onConnectionStatus((s) => setStatus(s))
    return () => offStatus()
  }, [])

  // Poll the gateway service so the Settings panel reflects external start/stop too.
  useEffect(() => {
    const tick = setInterval(() => {
      api().daemon.status().then(setDaemon).catch(() => undefined)
    }, 15_000)
    return () => clearInterval(tick)
  }, [])

  const refreshFleet = useCallback(async () => {
    if (!api()) return
    try {
      const [agentList, sessionList] = await Promise.all([
        api().rpc.request<{ agents: Agent[] }>('agents.list'),
        api().rpc.request<{ items?: SessionInfo[]; sessions?: SessionInfo[] }>('sessions.list')
      ])
      setAgents(agentList?.agents ?? [])
      setSessions(sessionList?.items ?? sessionList?.sessions ?? [])

      // Nodes that can hand us a screenshot become viewable desktops.
      try {
        const nodes = await api().rpc.request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>(
          'node.list'
        )
        const list = nodes?.paired ?? nodes?.nodes ?? []
        setDesktops(list.filter((n) => (n.commands ?? []).includes('screen.snapshot')))
      } catch {
        setDesktops([])
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    if (connected) {
      subscribed.current.clear()
      void refreshFleet()
    } else {
      setAgents([])
      setSessions([])
      setDesktops([])
    }
  }, [connected, refreshFleet])

  // Default to the gateway's own default agent once the roster arrives.
  useEffect(() => {
    if (!selectedAgentId && agents.length > 0) setSelectedAgentId(agents[0].id)
  }, [agents, selectedAgentId])

  // First run only: drop agents into departments whose name their id already hints at
  // (a "marketing" agent into Marketing), so the sidebar isn't one flat Unassigned list.
  // Runs once, and never touches an agent the user has already filed somewhere.
  const seeded = useRef(false)
  useEffect(() => {
    if (seeded.current || !config || agents.length === 0) return
    if (Object.keys(config.assignments).length > 0) {
      seeded.current = true
      return
    }
    seeded.current = true

    const guess = (agent: Agent): string | null => {
      const haystack = `${agent.id} ${agent.name ?? ''}`.toLowerCase()
      const hit = config.departments.find((d) => {
        const needle = d.id.toLowerCase()
        return haystack.includes(needle) || haystack.includes(d.name.toLowerCase())
      })
      if (hit) return hit.id
      // A CEO-ish id belongs with the executives even though the words differ.
      if (/(^|[-_])(ceo|founder|chief|exec)([-_]|$)/.test(haystack)) {
        return config.departments.find((d) => d.id === 'executive')?.id ?? null
      }
      return null
    }

    void (async () => {
      let latest = config
      for (const agent of agents) {
        const deptId = guess(agent)
        if (deptId) latest = await api().config.assignAgent(agent.id, deptId)
      }
      setConfig(latest)
    })()
  }, [agents, config])

  // ---- live events ------------------------------------------------------
  useEffect(() => {
    const off = api().onGatewayEvent(({ event, payload }) => {
      if (!payload) return

      if (event === 'chat') {
        const key: string | undefined = payload.sessionKey
        if (!key) return
        if (payload.state === 'delta' || payload.state === 'status') {
          setStreaming((prev) => {
            const carried = prev && prev.runId === payload.runId ? prev.text : ''
            const text =
              payload.state === 'delta' ? carried + (payload.deltaText ?? '') : carried
            return {
              sessionKey: key,
              runId: payload.runId,
              text,
              phase: payload.phase ?? (payload.state === 'delta' ? 'streaming' : null)
            }
          })
        } else if (payload.state === 'final') {
          // The durable record arrives separately on session.message; clearing here
          // avoids briefly rendering the reply twice.
          setStreaming((prev) => (prev?.runId === payload.runId ? null : prev))
        }
        return
      }

      if (event === 'session.message') {
        const key: string | undefined = payload.sessionKey
        const msg: ChatMessage | undefined = payload.message
        if (!key || !msg) return
        setMessages((prev) => {
          const list = prev[key] ?? []
          const id = msg.__openclaw?.id
          if (id && list.some((m) => m.__openclaw?.id === id)) return prev
          return { ...prev, [key]: [...list, msg] }
        })
        return
      }

      if (event === 'sessions.changed') {
        void refreshFleet()
      }
    })
    return () => off()
  }, [refreshFleet])

  // ---- per-session history ---------------------------------------------
  const openSession = useCallback(
    async (sessionKey: string) => {
      if (!connected) return
      try {
        const history = await api().rpc.request<{ messages?: ChatMessage[] }>('chat.history', {
          sessionKey,
          limit: 200
        })
        setMessages((prev) => ({ ...prev, [sessionKey]: history?.messages ?? [] }))
        if (!subscribed.current.has(sessionKey)) {
          // The subscribe param is `key`, not `sessionKey` — the two RPCs disagree.
          await api().rpc.request('sessions.messages.subscribe', { key: sessionKey })
          subscribed.current.add(sessionKey)
        }
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      }
    },
    [connected]
  )

  useEffect(() => {
    if (selectedKey && connected) void openSession(selectedKey)
  }, [selectedKey, connected, openSession])

  // ---- actions ----------------------------------------------------------
  const sendMessage = useCallback(
    async (text: string) => {
      if (!selectedKey || !text.trim()) return
      setBusy(true)
      try {
        await api().rpc.sendChat(selectedKey, text)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(false)
      }
    },
    [selectedKey]
  )

  const abortRun = useCallback(async () => {
    if (!selectedKey) return
    try {
      await api().rpc.request('chat.abort', { sessionKey: selectedKey })
      setStreaming(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [selectedKey])

  const selectedAgent = useMemo(
    () => agents.find((a) => a.id === selectedAgentId) ?? null,
    [agents, selectedAgentId]
  )

  const currentMessages = selectedKey ? (messages[selectedKey] ?? []) : []
  const currentStream = streaming && streaming.sessionKey === selectedKey ? streaming : null

  return {
    status,
    daemon,
    setDaemon,
    config,
    setConfig,
    agents,
    sessions,
    desktops,
    selectedAgent,
    selectedAgentId,
    setSelectedAgentId,
    currentMessages,
    currentStream,
    busy,
    error,
    setError,
    connected,
    refreshFleet,
    sendMessage,
    abortRun
  }
}
