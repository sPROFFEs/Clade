-- Durable, encrypted state for the public `praimate agent run` API.
-- Prompt/workflow payloads are represented by request_hash; the final
-- protocol envelope is stored in result_json for idempotent replay.

CREATE TABLE external_agent_runs (
  id TEXT PRIMARY KEY,
  request_hash TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  cli TEXT NOT NULL,
  runtime TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 1,
  result_json TEXT,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX idx_external_agent_runs_updated_at
  ON external_agent_runs(updated_at DESC);
