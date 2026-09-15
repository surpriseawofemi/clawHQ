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

/** A message's identity for merging: the gateway id when it has one, else its shape. */
const messageId = (m: ChatMessage): string =>
  m.__openclaw?.id ?? `${m.timestamp ?? 0}:${m.role}:${typeof m.content === 'string' ? m.content.length : m.content.length}`

/**
 * Puts a freshly fetched window on top of what was already held: older messages
 * the window does not reach stay, anything the window also carries is taken from
 * the window (the gateway's copy wins), and nothing appears twice.
 */
const mergeOlder = (have: ChatMessage[], fetched: ChatMessage[]): ChatMessage[] => {
  if (have.length === 0) return fetched
  if (fetched.length === 0) return have
  const ids = new Set(fetched.map(messageId))
  const firstTs = fetched[0].timestamp ?? 0
  const older = have.filter((m) => !ids.has(messageId(m)) && (m.timestamp ?? 0) < firstTs)
  return [...older, ...fetched]
}

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
  // A session other than the agent's main thread, chosen from the picker or just created.
  const [selectedSessionKey, setSelectedSessionKey] = useState<string | null>(null)
  const [messages, setMessages] = useState<Record<string, ChatMessage[]>>({})
  const [streaming, setStreaming] = useState<StreamingReply | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  /** Sessions we've already subscribed to on this connection. */
  const subscribed = useRef<Set<string>>(new Set())

  const connected = status.phase === 'connected'
  const selectedKey = selectedAgentId
    ? selectedSessionKey && selectedSessionKey.startsWith(`agent:${selectedAgentId}:`)
      ? selectedSessionKey
      : mainSessionKey(selectedAgentId)
    : null

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

  // Nodes come and go without any session changing, so poll node.list on its own.
  // Cheap: it is one small RPC, and it is what flips a desktop between online and
  // offline in the sidebar.
  const refreshDesktops = useCallback(async () => {
    if (!api()) return
    try {
      const nodes = await api().rpc.request<{ paired?: RemoteNode[]; nodes?: RemoteNode[] }>(
        'node.list'
      )
      const list = nodes?.paired ?? nodes?.nodes ?? []
      setDesktops(list.filter((n) => (n.commands ?? []).includes('screen.snapshot')))
    } catch {
      // Keep the last known list; a transient failure should not blank the sidebar.
    }
  }, [])

  useEffect(() => {
    if (!connected) return
    const tick = setInterval(() => void refreshDesktops(), 10_000)
    return () => clearInterval(tick)
  }, [connected, refreshDesktops])

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

  // ---- per-session history ---------------------------------------------
  // A thread opens with its recent tail. Over a tunnel every message costs bytes on
  // the wire and again crossing into the webview, so the first load stays small and
  // the rest comes on request.
  const HISTORY_TAIL = 60
  const HISTORY_FULL = 1000
  const [historyLoading, setHistoryLoading] = useState<string | null>(null)
  const [historyFull, setHistoryFull] = useState<Record<string, boolean>>({})

  // ---- the on-disk cache -------------------------------------------------
  // Every thread this window fetches is written to SQLite on this machine, keyed by
  // gateway and session. Opening a thread shows the cached copy at once; the gateway
  // is asked only when the session's last-updated stamp has moved past what the
  // cache was fetched at, and then only the recent window, merged by message id.
  const gatewayId = status.gatewayId
  const messagesRef = useRef(messages)
  messagesRef.current = messages
  const sessionsRef = useRef(sessions)
  sessionsRef.current = sessions
  const historyFullRef = useRef(historyFull)
  historyFullRef.current = historyFull
  /** The session stamp each in-memory thread was fetched (or cached) at. */
  const fetchedAt = useRef<Record<string, number>>({})
  const persistTimers = useRef<Record<string, number>>({})

  const sessionStamp = (sessionKey: string): number => {
    const s = sessionsRef.current.find((x) => x.key === sessionKey)
    return s?.updatedAt ?? s?.lastActivityAt ?? 0
  }

  const persist = useCallback(
    (sessionKey: string, list: ChatMessage[], stamp: number, full: boolean) => {
      if (!gatewayId) return
      if (persistTimers.current[sessionKey]) window.clearTimeout(persistTimers.current[sessionKey])
      persistTimers.current[sessionKey] = window.setTimeout(() => {
        delete persistTimers.current[sessionKey]
        api()
          .cache.put(gatewayId, sessionKey, JSON.stringify(list), stamp, full, list.length)
          .catch(() => undefined)
      }, 400)
    },
    [gatewayId]
  )

  const subscribe = async (sessionKey: string): Promise<void> => {
    if (subscribed.current.has(sessionKey)) return
    // The subscribe param is `key`, not `sessionKey` — the two RPCs disagree.
    await api().rpc.request('sessions.messages.subscribe', { key: sessionKey })
    subscribed.current.add(sessionKey)
  }

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
          const next = [...list, msg]
          // The live copy is as fresh as the session itself now; keep the disk copy up.
          const at = Math.max(msg.timestamp ?? 0, Date.now())
          fetchedAt.current[key] = at
          persist(key, next, at, historyFullRef.current[key] ?? false)
          return { ...prev, [key]: next }
        })
        return
      }

      if (event === 'sessions.changed') {
        void refreshFleet()
      }
      // Any node lifecycle event (pairing, connect, disconnect) is a reason to re-read
      // the list rather than wait for the next poll.
      if (event.startsWith('node.') && !event.startsWith('node.invoke')) {
        void refreshDesktops()
      }
    })
    return () => off()
  }, [refreshFleet, refreshDesktops, persist])

  const loadHistory = useCallback(
    async (sessionKey: string, limit: number) => {
      if (!connected) return
      let have = messagesRef.current[sessionKey]

      // Nothing in memory: the disk copy is the first thing on screen.
      if (!have && gatewayId) {
        try {
          const cached = await api().cache.get(gatewayId, sessionKey)
          if (cached?.json) {
            const list = JSON.parse(cached.json) as ChatMessage[]
            if (Array.isArray(list) && list.length > 0 && !messagesRef.current[sessionKey]) {
              have = list
              fetchedAt.current[sessionKey] = cached.updatedAtMs
              setMessages((prev) => (prev[sessionKey] ? prev : { ...prev, [sessionKey]: list }))
              setHistoryFull((prev) => ({ ...prev, [sessionKey]: cached.full }))
            }
          }
        } catch {
          // A bad cache row is just a slower open.
        }
      }

      // Fresh enough: the session has not changed since this copy was fetched, and
      // the copy covers what was asked for.
      const stamp = sessionStamp(sessionKey)
      const fetchedStamp = fetchedAt.current[sessionKey] ?? 0
      const covers = limit <= HISTORY_TAIL || historyFullRef.current[sessionKey]
      if (have && stamp > 0 && fetchedStamp >= stamp && covers) {
        try {
          await subscribe(sessionKey)
        } catch {
          /* the next load retries */
        }
        return
      }

      if (!have || have.length === 0) setHistoryLoading(sessionKey)
      try {
        const history = await api().rpc.request<{ messages?: ChatMessage[] }>('chat.history', {
          sessionKey,
          limit
        })
        const fetched = history?.messages ?? []
        // Fewer than asked for means the gateway sent the whole thread.
        const full = limit >= HISTORY_FULL || fetched.length < limit
        const merged = full ? fetched : mergeOlder(have ?? [], fetched)
        const at = Math.max(stamp, Date.now())
        fetchedAt.current[sessionKey] = at
        setMessages((prev) => ({ ...prev, [sessionKey]: merged }))
        setHistoryFull((prev) => ({ ...prev, [sessionKey]: full || (have ? historyFullRef.current[sessionKey] ?? false : false) }))
        persist(sessionKey, merged, at, full)
        await subscribe(sessionKey)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setHistoryLoading((cur) => (cur === sessionKey ? null : cur))
      }
    },
    [connected, gatewayId, persist]
  )

  // Warm every agent's main thread after connecting, one at a time, so the first
  // click on any agent finds its thread already here. The cache makes most of these
  // a disk read; only threads that moved cost a gateway call.
  const prefetched = useRef<string | null>(null)
  useEffect(() => {
    if (!connected || !gatewayId || agents.length === 0 || sessions.length === 0) return
    if (prefetched.current === gatewayId) return
    prefetched.current = gatewayId
    let live = true
    void (async () => {
      for (const a of agents) {
        if (!live) return
        const key = mainSessionKey(a.id)
        if (key === selectedKey) continue
        await loadHistory(key, HISTORY_TAIL)
        await new Promise((r) => setTimeout(r, 150))
      }
    })()
    return () => {
      live = false
    }
    // selectedKey is read once at start on purpose; the open thread loads itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, gatewayId, agents, sessions.length > 0, loadHistory])

  useEffect(() => {
    if (!connected) prefetched.current = null
  }, [connected])

  const openSession = useCallback((sessionKey: string) => loadHistory(sessionKey, HISTORY_TAIL), [loadHistory])

  const loadFullHistory = useCallback(async () => {
    if (selectedKey) await loadHistory(selectedKey, HISTORY_FULL)
  }, [selectedKey, loadHistory])

  useEffect(() => {
    if (selectedKey && connected) void openSession(selectedKey)
  }, [selectedKey, connected, openSession])

  // ---- actions ----------------------------------------------------------
  const sendMessage = useCallback(
    async (text: string, paths: string[] = []) => {
      if (!selectedKey || (!text.trim() && paths.length === 0)) return
      setBusy(true)
      try {
        if (paths.length > 0) {
          const reply = await api().rpc.sendChatWithFiles(selectedKey, text, paths)
          if (reply.skipped?.length) setError(`Not sent: ${reply.skipped.join('; ')}`)
          else setError(null)
        } else {
          await api().rpc.sendChat(selectedKey, text)
          setError(null)
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(false)
      }
    },
    [selectedKey]
  )

  // Sessions the picker offers for the selected agent: main first, then the rest by
  // recency. Cron-driven automation threads are left out; they are not conversations.
  const agentSessions = useMemo(() => {
    if (!selectedAgentId) return []
    const prefix = `agent:${selectedAgentId}:`
    const list = sessions
      .filter((s) => s.key.startsWith(prefix) && !s.key.startsWith(`${prefix}cron:`))
      .sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0))
    const main = mainSessionKey(selectedAgentId)
    const rest = list.filter((s) => s.key !== main)
    const mainInfo = list.find((s) => s.key === main) ?? { key: main, agentId: selectedAgentId, isMain: true }
    return [mainInfo, ...rest]
  }, [sessions, selectedAgentId])

  const selectSession = useCallback((key: string | null) => {
    setSelectedSessionKey(key)
  }, [])

  // A fresh thread with the agent. The gateway picks the key and files it under the
  // agent's main session as parent.
  const newSession = useCallback(
    async (label?: string) => {
      if (!selectedAgentId || !connected) return
      try {
        const created = await api().rpc.request<{ key?: string }>('sessions.create', {
          agentId: selectedAgentId,
          label: label?.trim() || `Chat ${new Date().toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}`
        })
        if (created?.key) {
          setSelectedSessionKey(created.key)
          void refreshFleet()
        }
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      }
    },
    [selectedAgentId, connected, refreshFleet]
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
    selectedKey,
    agentSessions,
    selectSession,
    newSession,
    currentMessages,
    messages,
    currentStream,
    historyLoading: selectedKey !== null && historyLoading === selectedKey,
    historyComplete: selectedKey !== null && historyFull[selectedKey] === true,
    loadFullHistory,
    busy,
    error,
    setError,
    connected,
    refreshFleet,
    sendMessage,
    abortRun
  }
}
