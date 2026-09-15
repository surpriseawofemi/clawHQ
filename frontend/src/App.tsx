import { useEffect, useState } from 'react'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AgentSettingsDialog } from './components/AgentSettingsDialog'
import { SettingsPage } from './components/settings/SettingsPage'
import { Onboarding } from './components/Onboarding'
import { ApprovalBanners } from './components/ApprovalBanners'
import { AgentDocuments } from './components/AgentDocuments'
import { AgentActivity } from './components/AgentActivity'
import { AgentCharter } from './components/AgentCharter'
import type { AgentTab } from './components/AgentHeader'
import { GatewaySwitcher } from './components/GatewaySwitcher'
import { SearchPalette } from './components/SearchPalette'
import type { SettingsSection } from './components/settings/SettingsPage'
import { api } from './api'
import { useFleet } from './state/useFleet'
import type { NodeNotification } from './types'

function App(): React.JSX.Element {
  const fleet = useFleet()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('gateways')
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)

  // Cmd-K (Ctrl-K elsewhere) opens the search palette from anywhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  const openSettings = (section?: SettingsSection): void => {
    if (section) setSettingsSection(section)
    if (section === 'notifications') setUnread(0)
    setSwitcherOpen(false)
    setSettingsOpen(true)
  }
  const [agentSettingsId, setAgentSettingsId] = useState<string | null>(null)
  const [notice, setNotice] = useState<NodeNotification | null>(null)
  const [unread, setUnread] = useState(0)
  // Chat, Documents or Charter for the selected agent.
  const [agentTab, setAgentTab] = useState<AgentTab>('chat')

  // An agent asked for a human through system.notify on this machine's node role.
  useEffect(() => {
    api.inbox
      .unread()
      .then(setUnread)
      .catch(() => undefined)
    return api.onNodeNotification((n) => {
      setNotice(n)
      setUnread((u) => u + 1)
    })
  }, [])

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
    messages,
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
        onOpenSearch={() => setSearchOpen(true)}
        selectedAgentId={selectedAgentId}
        onSelect={(id) => {
          selectSession(null)
          setSelectedAgentId(id)
        }}
        desktops={desktops}
        desktopsOpen={false}
        onToggleDesktops={() => void api.window.openDesktop(desktops.find((d) => d.connected)?.nodeId ?? '')}
        unreadNotices={unread}
        onOpenNotifications={() => openSettings('notifications')}
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
          agents={agents}
          pendingCount={0}
          initialSection={settingsSection}
          onOpenAgent={(agentId) => {
            selectSession(null)
            setSelectedAgentId(agentId)
            setSettingsOpen(false)
          }}
          onClose={() => setSettingsOpen(false)}
          onConfigChanged={setConfig}
          onDaemonChanged={setDaemon}
          onReconnected={refreshFleet}
        />
      ) : (
      selectedAgent && agentTab === 'documents' ? (
        <AgentDocuments agent={selectedAgent} tab={agentTab} onTab={setAgentTab} connected={connected} />
      ) : selectedAgent && agentTab === 'activity' ? (
        <AgentActivity
          agent={selectedAgent}
          tab={agentTab}
          onTab={setAgentTab}
          sessions={sessions}
          connected={connected}
          onOpenSession={(key) => {
            selectSession(key)
            setAgentTab('chat')
          }}
        />
      ) : selectedAgent && agentTab === 'charter' ? (
        <AgentCharter
          agent={selectedAgent}
          tab={agentTab}
          onTab={setAgentTab}
          connected={connected}
          onSaved={refreshFleet}
        />
      ) : (
      <ChatView
        agent={selectedAgent}
        sessionKey={selectedKey}
        tab={agentTab}
        onTab={setAgentTab}
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
      ))}

      <ApprovalBanners connected={connected} />

      {searchOpen && (
        <SearchPalette
          agents={agents}
          sessions={sessions}
          messages={messages}
          onClose={() => setSearchOpen(false)}
          onPick={(agentId, key) => {
            setSearchOpen(false)
            setSettingsOpen(false)
            setSelectedAgentId(agentId)
            selectSession(key)
            setAgentTab('chat')
          }}
        />
      )}
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
            {notice.origin && <span className="plugin-desc">via {notice.origin}</span>}
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
