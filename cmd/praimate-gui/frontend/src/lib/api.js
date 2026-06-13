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
  firstRun: () => call('FirstRun'),
  completeFirstRun: (root, samples, agents, cloneURL) =>
    call('CompleteFirstRun', root, samples, agents, cloneURL || ''),

  listChats: () => call('ListChats'),
  chatMessages: (id) => call('ChatMessages', id),
  deleteChat: (id) => call('DeleteChat', id),
  startChat: (agentID, cli, cwd) => call('StartChat', agentID, cli, cwd),
  startCleanChat: (cli, model, cwd) => call('StartCleanChat', cli, model, cwd),
  sendChat: (chatID, message) => call('SendChat', chatID, message),
  sendChatWithAttachments: (chatID, message, paths) =>
    call('SendChatWithAttachments', chatID, message, paths),
  sendChatStream: (chatID, message, paths) =>
    call('SendChatStream', chatID, message, paths || []),
  cancelChatTurn: (chatID) => call('CancelChatTurn', chatID),
  resolveApproval: (id, allow, always) => call('ResolveApproval', id, allow, always),
  runChatCommand: (chatID, command) => call('RunChatCommand', chatID, command),
  setChatTools: (chatID, tools) => call('SetChatTools', chatID, tools),
  pickChatAttachments: (chatID) => call('PickChatAttachments', chatID),
  attachmentDataURL: (path) => call('AttachmentDataURL', path),

  listCLIs: () => call('ListCLIs'),
  listCLIModels: (cli) => call('ListCLIModels', cli),
  listWorkspaceChats: () => call('ListWorkspaceChats'),
  openWorkspaceChat: (id) => call('OpenWorkspaceChat', id),

  listAgents: () => call('ListAgents'),
  importAgentDialog: () => call('ImportAgentDialog'),
  exportAgentDialog: (id) => call('ExportAgentDialog', id),
  deleteAgent: (id) => call('DeleteAgent', id),
  agentYAML: (id) => call('AgentYAML', id),
  saveAgentYAML: (yaml) => call('SaveAgentYAML', yaml),
  newAgentTemplateYAML: () => call('NewAgentTemplateYAML'),
  getAgentKnowledge: (id) => call('GetAgentKnowledge', id),
  setAgentKnowledgeMode: (id, mode) => call('SetAgentKnowledgeMode', id, mode),
  pickAgentKnowledgeFiles: (id) => call('PickAgentKnowledgeFiles', id),
  pickAgentKnowledgeFolder: (id) => call('PickAgentKnowledgeFolder', id),
  deleteAgentKnowledgeFile: (id, rel) => call('DeleteAgentKnowledgeFile', id, rel),
  buildAgentRAG: (id, backend, apiKey, model) => call('BuildAgentRAG', id, backend || '', apiKey || '', model || ''),
  installBundledGraphify: () => call('InstallBundledGraphify'),
  exportAgentPackDialog: (id) => call('ExportAgentPackDialog', id),
  importWorkpathTemplateDialog: () => call('ImportWorkpathTemplateDialog'),

  updateChatConfig: (chatID, cli, model, tools, localEndpoint, localApiKey, localModel) =>
    call('UpdateChatConfig', chatID, cli, model, tools, localEndpoint || '', localApiKey || '', localModel || ''),
  searchChats: (q) => call('SearchChats', q),

  listCLIBackends: () => call('ListCLIBackends'),
  listInstallMethods: (cli) => call('ListInstallMethods', cli),
  installCLI: (cli, methodID) => call('InstallCLI', cli, methodID),
  listManagedTools: () => call('ListManagedTools'),
  listToolInstallMethods: (tool) => call('ListToolInstallMethods', tool),
  installManagedTool: (tool, methodID) => call('InstallManagedTool', tool, methodID),
  buildRequirements: (tool) => call('BuildRequirements', tool),
  buildToolFromSource: (tool) => call('BuildToolFromSource', tool),
  checkUpdate: () => call('CheckUpdate'),

  getLocalLLM: () => call('GetLocalLLM'),
  setLocalLLM: (d) => call('SetLocalLLM', d),
  testLocalLLM: (endpoint, apiKey) => call('TestLocalLLM', endpoint, apiKey),

  editorMode: () => call('EditorMode'),
  editorListFiles: () => call('EditorListFiles'),
  editorReadFile: (rel) => call('EditorReadFile', rel),
  editorWriteFile: (rel, content) => call('EditorWriteFile', rel, content),
  editorCreateFile: (rel) => call('EditorCreateFile', rel),
  openEditorWindow: (folder, agentID, cli, model, chatID, localEndpoint, localApiKey, localModel) =>
    call('OpenEditorWindow', folder, agentID, cli, model || '', chatID, localEndpoint || '', localApiKey || '', localModel || ''),
  startTerminal: (agentID, cli, model, cwd, localEndpoint, localApiKey, localModel) =>
    call('StartTerminal', agentID, cli, model || '', cwd, localEndpoint || '', localApiKey || '', localModel || ''),
  localLLMModels: () => call('LocalLLMModels'),
  recordCodeSession: (cli, model, cwd, localEndpoint, localApiKey, localModel) =>
    call('RecordCodeSession', cli, model || '', cwd, localEndpoint || '', localApiKey || '', localModel || ''),
  localCLIStatus: () => call('LocalCLIStatusNow'),
  applyLocalToCLI: (cli, model) => call('ApplyLocalToCLI', cli, model || ''),
  disableLocalForCLI: (cli) => call('DisableLocalForCLI', cli),

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

  backupStatus: () => call('BackupStatus'),
  setBackupEnabled: (on) => call('SetBackupEnabled', on),
  setBackupRemote: (url) => call('SetBackupRemote', url),
  testBackupRemote: (url) => call('TestBackupRemote', url),
  backupSyncNow: () => call('BackupSyncNow'),
  resolveBackupDivergence: (strategy) => call('ResolveBackupDivergence', strategy),
  backupForcePush: () => call('BackupForcePush'),
  backupResetFromRemote: () => call('BackupResetFromRemote'),
  backupDisconnect: () => call('BackupDisconnect'),
  setBackupAutoSync: (on) => call('SetBackupAutoSync', on),
  setBackupForceLocal: (on) => call('SetBackupForceLocal', on),
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

// onChatStream subscribes to live chat turn events (text deltas, tool
// activity) emitted while SendChatStream runs. Returns an unsubscribe
// function. No-op outside Wails.
export function onChatStream(handler) {
  if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
    window.runtime.EventsOn('praimate:chat-stream', handler)
    return () => window.runtime.EventsOff('praimate:chat-stream')
  }
  return () => {}
}

// onApproval subscribes to mid-turn permission requests ("ask" Tools
// level). Answer with api.resolveApproval(id, allow, always). Returns
// an unsubscribe function. No-op outside Wails.
export function onApproval(handler) {
  if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
    window.runtime.EventsOn('praimate:approval', handler)
    return () => window.runtime.EventsOff('praimate:approval')
  }
  return () => {}
}
