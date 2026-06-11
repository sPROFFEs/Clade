// Thin wrapper over the Wails-injected window.go.main.App bindings.
// Every call returns a promise; rejections carry the Go error string.
//
// In a plain browser (vite dev without Wails) window.go is absent —
// calls reject with a clear message so pages render their error state
// instead of crashing.

function app() {
  if (typeof window !== 'undefined' && window.go?.main?.App) {
    return window.go.main.App
  }
  return null
}

function call(method, ...args) {
  const a = app()
  if (!a) return Promise.reject(new Error('PrAImate backend not available (running outside Wails?)'))
  return a[method](...args)
}

export const api = {
  health: () => call('Health'),

  listChats: () => call('ListChats'),
  chatMessages: (id) => call('ChatMessages', id),
  deleteChat: (id) => call('DeleteChat', id),
  startChat: (agentID, cli, cwd) => call('StartChat', agentID, cli, cwd),
  sendChat: (chatID, message) => call('SendChat', chatID, message),

  listAgents: () => call('ListAgents'),
  importAgentDialog: () => call('ImportAgentDialog'),
  exportAgentDialog: (id) => call('ExportAgentDialog', id),
  deleteAgent: (id) => call('DeleteAgent', id),

  pickFolder: () => call('PickFolder'),
  runWorkflow: (agentID, workflow, cli, cwd, inputs) =>
    call('RunWorkflow', agentID, workflow, cli, cwd, inputs),
  privacyPreview: (text) => call('PrivacyPreview', text),

  getMemory: () => call('GetMemory'),
  setMemoryEnabled: (on) => call('SetMemoryEnabled', on),
  setIdentity: (k, v) => call('SetIdentity', k, v),
  deleteIdentity: (k) => call('DeleteIdentity', k),
  pinFact: (text) => call('PinFact', text),
  deletePinned: (id) => call('DeletePinned', id),
  deleteEpisode: (id) => call('DeleteEpisode', id),

  mcpCatalogue: () => call('MCPCatalogue'),
  mcpServers: () => call('MCPServers'),
  connectMCP: (key, apiKey) => call('ConnectMCP', key, apiKey),
  addCustomMCP: (name, transport, command, url, envText) =>
    call('AddCustomMCP', name, transport, command, url, envText),
  setMCPEnabled: (id, on) => call('SetMCPEnabled', id, on),
  deleteMCPServer: (id) => call('DeleteMCPServer', id),

  listWatchers: () => call('ListWatchers'),
  addWatcher: (agentID, path, workflow, patterns) =>
    call('AddWatcher', agentID, path, workflow, patterns),
  setWatcherEnabled: (id, on) => call('SetWatcherEnabled', id, on),
  deleteWatcher: (id) => call('DeleteWatcher', id),

  listSchedules: () => call('ListSchedules'),
  addCronSchedule: (agentID, cron, workflow) => call('AddCronSchedule', agentID, cron, workflow),
  setScheduleEnabled: (id, on) => call('SetScheduleEnabled', id, on),
  deleteSchedule: (id) => call('DeleteSchedule', id),

  listPrivacyPatterns: () => call('ListPrivacyPatterns'),
  addPrivacyPattern: (p) => call('AddPrivacyPattern', p),
  deletePrivacyPattern: (i) => call('DeletePrivacyPattern', i),

  getGUISetting: (k) => call('GetGUISetting', k),
  setGUISetting: (k, v) => call('SetGUISetting', k, v),

  praimateCodeInstalled: () => call('PraimateCodeInstalled'),
  installPraimateCode: () => call('InstallPraimateCode'),
}

// onTurn subscribes to streamed workflow turns. Returns an unsubscribe
// function. No-op outside Wails.
export function onTurn(handler) {
  if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
    window.runtime.EventsOn('praimate:turn', handler)
    return () => window.runtime.EventsOff('praimate:turn')
  }
  return () => {}
}
