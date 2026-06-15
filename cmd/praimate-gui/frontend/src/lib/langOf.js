// Extension → CodeEditor language id. Kept in one place so the studio
// editor and the New-Agent studio agree on which highlighter to load.
// Unknown extensions fall back to 'plain' (no highlighting, still
// editable as text).

const EXT_LANG = {
  md: 'markdown', markdown: 'markdown',
  yml: 'yaml', yaml: 'yaml',
  json: 'json',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript',
  ts: 'javascript', tsx: 'javascript', jsx: 'javascript',
  html: 'html', htm: 'html',
  css: 'css', scss: 'css', less: 'css',
  py: 'python',
  sh: 'shell', bash: 'shell', zsh: 'shell',
  go: 'go',
  rs: 'rust',
  toml: 'toml',
  sql: 'sql',
  ini: 'ini', conf: 'ini', cfg: 'ini', env: 'ini',
  dockerfile: 'dockerfile',
}

export function langOf(path) {
  if (!path) return 'plain'
  const base = String(path).split(/[\\/]/).pop().toLowerCase()
  if (base === 'dockerfile' || base.startsWith('dockerfile.')) return 'dockerfile'
  const dot = base.lastIndexOf('.')
  if (dot < 0) return 'plain'
  return EXT_LANG[base.slice(dot + 1)] || 'plain'
}
