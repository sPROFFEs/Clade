function quoteCommandPart(value) {
  if (/^[A-Za-z0-9_./:@%+=,-]+$/.test(value)) return value
  return `'${value.replaceAll("'", "'\\''")}'`
}

export function commandForMCPForm(server) {
  return [server.command || '', ...(server.args || [])]
    .filter(Boolean)
    .map(quoteCommandPart)
    .join(' ')
}

export function envForMCPForm(server) {
  return Object.entries(server.env || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => `${key}=${value}`)
    .join('\n')
}
