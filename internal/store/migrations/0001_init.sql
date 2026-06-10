-- PrAImate schema v1.
-- Migrations are append-only — never edit a shipped file; add 0002_*.sql etc.

CREATE TABLE chats (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  agent_id TEXT,
  cli_agent TEXT NOT NULL,
  workspace_path TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  ended_at TEXT,
  exit_kind TEXT,
  settings_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_chats_updated_at ON chats(updated_at DESC);
CREATE INDEX idx_chats_agent_id ON chats(agent_id);

CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  ts TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  tokens INTEGER,
  meta_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_messages_chat_id_ts ON messages(chat_id, ts);

CREATE TABLE agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  icon TEXT,
  instructions TEXT NOT NULL DEFAULT '',
  tools_json TEXT NOT NULL DEFAULT '[]',
  mcp_servers_json TEXT NOT NULL DEFAULT '[]',
  workflows_json TEXT NOT NULL DEFAULT '[]',
  supports_json TEXT NOT NULL DEFAULT '[]',
  default_workflow TEXT NOT NULL DEFAULT '',
  source_path TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE memory_identity (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  source TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE memory_pinned (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  text TEXT NOT NULL,
  salience REAL NOT NULL DEFAULT 0.5,
  source_count INTEGER NOT NULL DEFAULT 1,
  use_count INTEGER NOT NULL DEFAULT 0,
  last_used TEXT,
  created_at TEXT NOT NULL,
  last_decayed_at TEXT NOT NULL
);

CREATE INDEX idx_memory_pinned_salience ON memory_pinned(salience DESC);

CREATE TABLE memory_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id TEXT REFERENCES chats(id) ON DELETE SET NULL,
  summary TEXT NOT NULL,
  topics_json TEXT NOT NULL DEFAULT '[]',
  entities_json TEXT NOT NULL DEFAULT '[]',
  decisions_json TEXT NOT NULL DEFAULT '[]',
  actions_json TEXT NOT NULL DEFAULT '[]',
  salience REAL NOT NULL DEFAULT 0.5,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_memory_episodes_created_at ON memory_episodes(created_at DESC);

CREATE TABLE mcp_servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  transport TEXT NOT NULL,
  command TEXT,
  url TEXT,
  args_json TEXT NOT NULL DEFAULT '[]',
  env_json TEXT NOT NULL DEFAULT '{}',
  auth_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  catalogue_key TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE schedules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id TEXT REFERENCES chats(id) ON DELETE CASCADE,
  agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
  cron TEXT,
  at TEXT,
  workflow TEXT,
  inputs_json TEXT NOT NULL DEFAULT '{}',
  on_miss TEXT NOT NULL DEFAULT 'skip',
  priority TEXT NOT NULL DEFAULT 'normal',
  last_run_at TEXT,
  next_run_at TEXT,
  enabled INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_schedules_next_run_at ON schedules(next_run_at) WHERE enabled = 1;

CREATE TABLE watchers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id TEXT REFERENCES chats(id) ON DELETE CASCADE,
  agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  patterns_json TEXT NOT NULL DEFAULT '[]',
  workflow TEXT,
  inputs_json TEXT NOT NULL DEFAULT '{}',
  debounce_ms INTEGER NOT NULL DEFAULT 1000,
  enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE settings_cli (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE settings_gui (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
