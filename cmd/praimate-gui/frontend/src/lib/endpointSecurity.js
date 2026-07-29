function endpointHostname(endpoint) {
  const value = endpoint.trim()
  if (!value) return ''

  try {
    const url = new URL(value.includes('://') ? value : `http://${value}`)
    return url.hostname.toLowerCase()
  } catch {
    return ''
  }
}

export function endpointTransport(endpoint) {
  const value = endpoint.trim()
  if (!value) return { insecure: false, loopback: false }
  if (value.toLowerCase().startsWith('https://')) {
    return { insecure: false, loopback: false }
  }

  const hostname = endpointHostname(value)
  const loopback =
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname === '::1' ||
    hostname === '[::1]'

  return { insecure: true, loopback }
}
