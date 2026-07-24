-- Optional metadata for an explicitly user-run, platform-specific setup
-- script carried by a .praimate-agent pack.
ALTER TABLE agents ADD COLUMN requirements_json TEXT NOT NULL DEFAULT '{}';
