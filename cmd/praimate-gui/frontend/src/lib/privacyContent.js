export const privacyIntroduction =
  'PrAImate has no product telemetry and does not generate application, query, or terminal log files. It stores chats, agents, settings, and MCP configuration locally.'

export const privacyDisclosures = [
  {
    title: 'Encrypted local database',
    body: 'The SQLite database is encrypted at rest with AES-256-XTS. Its random key is stored separately with user-only permissions. This protects a copied database, but not someone controlling your OS account, and XTS does not authenticate against tampering. Keep the database and key together when backing up: losing the key makes the database unreadable.',
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
    body: 'Git backup is off by default. If enabled, it includes workspace files, per-chat MEMORY.md files, and a portable plaintext database snapshot. Use a private remote you trust and protect its credentials.',
  },
]

export const privacyCompatibility =
  'PrAImate supports Linux and Windows. You can review this information later on the About page.'
