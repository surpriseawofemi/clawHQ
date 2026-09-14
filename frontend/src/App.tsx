import { useEffect, useState } from 'react'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AgentSettingsDialog } from './components/AgentSettingsDialog'
import { SettingsDialog } from './components/SettingsDialog'
import { DesktopView } from './components/DesktopView'
import { Onboarding } from './components/Onboarding'
import { api } from './api'
import { useFleet } from './state/useFleet'
import type { NodeNotification } from './types'

function App(): React.JSX.Element {
  const fleet = useFleet()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [agentSettingsId, setAgentSettingsId] = useState<string | null>(null)
  // Selecting a desktop takes over the main pane; selecting an agent gives it back.
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [notice, setNotice] = useState<NodeNotification | null>(null)

  // An agent asked for a human through system.notify on this machine's node role.
  useEffect(() => api.onNodeNotification(setNotice), [])

  const {
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
  } = fleet

  // Nothing works before a connection lands, so onboarding owns the whole window
  // until it does — including while a device is waiting to be approved.
  if (status.phase === 'pending' || (!status.paired && status.phase !== 'connected')) {
    return <Onboarding status={status} onConnected={refreshFleet} />
  }

  const agentForSettings = agents.find((a) => a.id === agentSettingsId) ?? null

  return (
    <div className="app">
      <Sidebar
        agents={agents}
        sessions={sessions}
        config={config}
        selectedAgentId={selectedAgentId}
        onSelect={(id) => {
          setSelectedNodeId(null)
          setSelectedAgentId(id)
        }}
        desktops={desktops}
        selectedNodeId={selectedNodeId}
        onSelectDesktop={setSelectedNodeId}
        onAgentSettings={setAgentSettingsId}
        onOpenSettings={() => setSettingsOpen(true)}
        connected={connected}
        serverVersion={status.serverVersion}
      />

      {selectedNodeId ? (
        <DesktopView node={desktops.find((d) => d.nodeId === selectedNodeId)!} />
      ) : (
      <ChatView
        agent={selectedAgent}
        messages={currentMessages}
        stream={currentStream}
        busy={busy}
        connected={connected}
        onSend={sendMessage}
        onAbort={abortRun}
        onSettings={() => selectedAgentId && setAgentSettingsId(selectedAgentId)}
      />
      )}

      {notice && (
        <div className="notice" role="status">
          <span className="notice-icon">🔔</span>
          <span className="notice-text">
            <strong>{notice.title}</strong>
            {notice.body && <span>{notice.body}</span>}
          </span>
          {desktops.some((d) => d.connected) && (
            <button
              className="btn btn-sm btn-primary"
              onClick={() => {
                const live = desktops.find((d) => d.connected)
                if (live) setSelectedNodeId(live.nodeId)
                setNotice(null)
              }}
            >
              Open desktop
            </button>
          )}
          <button className="icon-btn" onClick={() => setNotice(null)}>
            ✕
          </button>
        </div>
      )}

      {error && (
        <div className="toast" role="alert">
          <span>{error}</span>
          <button className="icon-btn" onClick={() => setError(null)}>
            ✕
          </button>
        </div>
      )}

      {agentForSettings && (
        <AgentSettingsDialog
          agent={agentForSettings}
          allAgents={agents}
          config={config}
          onClose={() => setAgentSettingsId(null)}
          onSaved={refreshFleet}
          onConfigChanged={setConfig}
        />
      )}

      {settingsOpen && (
        <SettingsDialog
          status={status}
          daemon={daemon}
          config={config}
          onClose={() => setSettingsOpen(false)}
          onConfigChanged={setConfig}
          onDaemonChanged={setDaemon}
          onReconnected={refreshFleet}
        />
      )}
    </div>
  )
}

export default App
