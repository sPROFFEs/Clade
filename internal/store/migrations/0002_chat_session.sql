-- Interactive chat support: persist the CLI adapter's session id on the
-- chat row so follow-up messages can resume the same conversation
-- (claude --resume <id>, codex resume <id>, …) instead of starting cold.
ALTER TABLE chats ADD COLUMN session_id TEXT;
