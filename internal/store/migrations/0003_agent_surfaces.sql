-- Launch-surface gating: which GUI surfaces an agent may be opened
-- from. JSON array of "chat" | "terminal" | "editor"; '[]' (the
-- default) means ALL surfaces are allowed, so existing agents keep
-- today's behavior without a data migration.
ALTER TABLE agents ADD COLUMN surfaces_json TEXT NOT NULL DEFAULT '[]';
