import { useEffect, useState } from 'react'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AgentSettingsDialog } from './components/AgentSettingsDialog'
import { SettingsPage } from './components/settings/SettingsPage'
import { DesktopView } from './components/DesktopView'
import { Onboarding } from './components/Onboarding'
import { ApprovalBanners } from './components/ApprovalBanners'
import { GatewaySwitcher } from './components/GatewaySwitcher'
import type { SettingsSection } from './components/settings/SettingsPage'
import { api } from './api'
import { useFleet } from './state/useFleet'
import type { NodeNotification } from './types'

function App(): React.JSX.Element {
  const fleet = useFleet()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('gateways')
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const openSettings = (section?: SettingsSection): void => {
    if (section) setSettingsSection(section)
    setSwitcherOpen(false)
    setSettingsOpen(true)
  }
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
    selectedKey,
    agentSessions,
    selectSession,
    newSession,
    currentMessages,
    currentStream,
    historyLoading,
    historyComplete,
    loadFullHistory,
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
          selectSession(null)
          setSelectedAgentId(id)
        }}
        desktops={desktops}
        selectedNodeId={selectedNodeId}
        onSelectDesktop={setSelectedNodeId}
        onRemoveDesktop={(node) => {
          // A node is a device with the node role, so the gateway keeps its pairing in
          // the node queue; the device queue is the fallback for older gateways.
          void (async () => {
            try {
              await api.rpc.request('node.pair.remove', { nodeId: node.nodeId })
            } catch {
              try {
                await api.rpc.request('device.pair.remove', { deviceId: node.nodeId })
              } catch (err) {
                setError(err instanceof Error ? err.message : String(err))
                return
              }
            }
            if (selectedNodeId === node.nodeId) setSelectedNodeId(null)
            void refreshFleet()
          })()
        }}
        onAgentSettings={setAgentSettingsId}
        onOpenSettings={() => openSettings()}
        onOpenSwitcher={() => setSwitcherOpen(true)}
        status={status}
        settingsOpen={settingsOpen}
      />

      {settingsOpen ? (
        <SettingsPage
          status={status}
          daemon={daemon}
          config={config}
          pendingCount={0}
          initialSection={settingsSection}
          onClose={() => setSettingsOpen(false)}
          onConfigChanged={setConfig}
          onDaemonChanged={setDaemon}
          onReconnected={refreshFleet}
        />
      ) : selectedNodeId ? (
        <DesktopView node={desktops.find((d) => d.nodeId === selectedNodeId)!} />
      ) : (
      <ChatView
        agent={selectedAgent}
        sessionKey={selectedKey}
        sessions={agentSessions}
        onSelectSession={selectSession}
        onNewSession={newSession}
        messages={currentMessages}
        stream={currentStream}
        loadingHistory={historyLoading}
        historyComplete={historyComplete}
        onLoadFullHistory={() => void loadFullHistory()}
        busy={busy}
        connected={connected}
        onSend={sendMessage}
        onAbort={abortRun}
        onSettings={() => selectedAgentId && setAgentSettingsId(selectedAgentId)}
      />
      )}

      <ApprovalBanners connected={connected} />

      {switcherOpen && (
        <GatewaySwitcher
          status={status}
          onClose={() => setSwitcherOpen(false)}
          onConnected={refreshFleet}
          onConfigChanged={setConfig}
          onOpenSettings={(section) => openSettings(section)}
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

    </div>
  )
}

export default App
