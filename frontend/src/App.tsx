import { useEffect, useState } from 'react'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AgentSettingsDialog } from './components/AgentSettingsDialog'
import { SettingsPage } from './components/settings/SettingsPage'
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
          selectSession(null)
          setSelectedAgentId(id)
        }}
        desktops={desktops}
        desktopsOpen={false}
        onToggleDesktops={() => void api.window.openDesktop(desktops.find((d) => d.connected)?.nodeId ?? '')}
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
          <span className="notice-icon">{notice.agentEmoji || '🔔'}</span>
          <span className="notice-text">
            <strong>
              {notice.agentName || notice.agentId
                ? `${notice.agentName || notice.agentId}: ${notice.title}`
                : notice.title}
            </strong>
            {notice.body && <span>{notice.body}</span>}
          </span>
          {notice.agentId && (
            <button
              className="btn btn-sm"
              onClick={() => {
                selectSession(null)
                setSelectedAgentId(notice.agentId!)
                setSettingsOpen(false)
                setNotice(null)
              }}
            >
              Open chat
            </button>
          )}
          {desktops.some((d) => d.connected) && (
            <button
              className="btn btn-sm btn-primary"
              onClick={() => {
                const live = desktops.find((d) => d.connected)
                void api.window.openDesktop(live?.nodeId ?? '')
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
