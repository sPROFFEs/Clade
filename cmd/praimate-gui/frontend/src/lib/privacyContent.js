export const privacyIntroduction =
  'PrAImate has no product telemetry and does not generate application, query, or terminal log files. It stores chats, agents, settings, and MCP configuration locally.'

export const privacyDisclosures = [
  {
    title: 'Encrypted local database',
    body: 'The SQLite database is encrypted at rest with AES-256-XTS. A password-protected envelope contains its random key; the raw key is held only in process memory while PrAImate is unlocked. XTS provides confidentiality rather than tamper authentication. Losing the database password makes local data and encrypted backups unrecoverable.',
  },
  {
    title: 'AI providers receive what you send',
    body: 'Prompts, selected files, and tool output go to the CLI and model provider you choose. Built-in redaction catches common secrets but cannot guarantee that every sensitive value is removed.',
  },
  {
    title: 'Agents can change files',
    body: 'Tool-enabled sessions may read, create, edit, or execute files in the working folder according to the permission level you select. Review the folder and permissions before starting.',
  },
  {
    title: 'Terminal output is memory-only',
    body: 'Live Code-terminal scrollback is retained only while its process is running. It is not written to a diagnostic or history log and cannot be recovered after the terminal or application closes.',
  },
  {
    title: 'Backups are your responsibility',
    body: 'Git backup is off by default. The database snapshot and its key envelope are encrypted with the same database password, so another Windows or Linux installation can restore them with that password. A copied repository permits offline password guessing, so use a strong unique password. Workspace files, transcripts, and per-chat MEMORY.md files remain normal Git files and may contain sensitive content. Use a private remote you trust.',
  },
]

export const privacyCompatibility =
  'PrAImate supports Linux and Windows. You can review this information later on the About page.'
