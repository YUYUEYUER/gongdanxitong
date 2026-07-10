const BRIDGE_MARKER = 'libredesk-widget-bridge'
const BRIDGE_VERSION = 1
const MAX_TOKEN_LENGTH = 16 * 1024
const MAX_URL_LENGTH = 2048
const MAX_TITLE_LENGTH = 512
const MAX_QUEUE_LENGTH = 100
const NONCE_RE = /^[A-Za-z0-9_-]{20,128}$/

let parentOrigin = ''
let trustedDomains = []
let started = false
let channelNonce = ''
let messageHandler = null
let expectedParentOrigin = ''
const pendingMessages = []

const isRecord = (value) => value !== null && typeof value === 'object' && !Array.isArray(value)
const isStringWithin = (value, max) => typeof value === 'string' && value.length <= max
const hasOnlyKeys = (value, allowed) => Object.keys(value).every((key) => allowed.includes(key))

function getChannelNonce () {
  if (channelNonce) return channelNonce
  const params = new URLSearchParams(window.location.hash.slice(1))
  const value = params.get('ld_channel') || ''
  channelNonce = NONCE_RE.test(value) ? value : ''
  return channelNonce
}

function parseTrustedDomain (entry) {
  const value = String(entry || '').trim().toLowerCase()
  if (!value || value.includes('://') || value.includes('/') || value.includes(' ')) return null

  const wildcard = value.startsWith('*.')
  const hostAndPort = wildcard ? value.slice(2) : value
  const separator = hostAndPort.lastIndexOf(':')
  const hasPort = separator > -1 && hostAndPort.indexOf(':') === separator
  const host = (hasPort ? hostAndPort.slice(0, separator) : hostAndPort).replace(/\.$/, '')
  const port = hasPort ? hostAndPort.slice(separator + 1) : ''
  if (!host || (port && !/^\d{1,5}$/.test(port))) return null
  return { host, port, wildcard }
}

export function isTrustedParentOrigin (origin, domains = []) {
  let parsed
  try {
    parsed = new URL(origin)
  } catch {
    return false
  }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) return false

  const configured = Array.isArray(domains) ? domains.map(parseTrustedDomain).filter(Boolean) : []
  // The server remains authoritative when no public allowlist is returned.
  if (configured.length === 0) return true

  const hostname = parsed.hostname.toLowerCase().replace(/\.$/, '')
  const port = parsed.port || (parsed.protocol === 'https:' ? '443' : '80')
  return configured.some((domain) => {
    if (domain.port && domain.port !== port) return false
    if (domain.wildcard) return hostname.endsWith(`.${domain.host}`)
    return hostname === domain.host
  })
}

export function isValidParentMessage (data) {
  if (!isRecord(data) || data.bridge !== BRIDGE_MARKER || data.version !== BRIDGE_VERSION) return false
  if (!NONCE_RE.test(data.channelNonce || '')) return false

  const baseKeys = ['bridge', 'version', 'channelNonce', 'type']
  switch (data.type) {
    case 'WIDGET_CHANNEL_INIT':
    case 'WIDGET_OPENED':
    case 'WIDGET_CLOSED':
    case 'CLEAR_SESSION':
      return hasOnlyKeys(data, baseKeys)
    case 'SET_MOBILE_STATE':
      return hasOnlyKeys(data, [...baseKeys, 'isMobile']) && typeof data.isMobile === 'boolean'
    case 'WIDGET_EXPANDED':
      return hasOnlyKeys(data, [...baseKeys, 'isExpanded']) && typeof data.isExpanded === 'boolean'
    case 'SESSION_DATA':
      return hasOnlyKeys(data, baseKeys)
    case 'SET_JWT_TOKEN':
      return hasOnlyKeys(data, [...baseKeys, 'jwt']) &&
        isStringWithin(data.jwt, MAX_TOKEN_LENGTH)
    case 'PAGE_VISIT':
      return hasOnlyKeys(data, [...baseKeys, 'url', 'title']) &&
        isStringWithin(data.url, MAX_URL_LENGTH) &&
        isStringWithin(data.title, MAX_TITLE_LENGTH)
    default:
      return false
  }
}

export function isValidChildMessage (data) {
  if (!isRecord(data) || typeof data.type !== 'string') return false
  switch (data.type) {
    case 'WIDGET_CHANNEL_READY':
    case 'CLOSE_WIDGET':
    case 'WIDGET_LOADED':
    case 'EXPAND_WIDGET':
    case 'COLLAPSE_WIDGET':
    case 'REQUEST_PAGE_INFO':
    case 'CLEAR_VISITOR_TOKEN':
    case 'CLEAR_SESSION_TOKEN':
    case 'SESSION_CLEARED':
    case 'SESSION_CLEAR_FAILED':
      return hasOnlyKeys(data, ['type'])
    case 'UPDATE_UNREAD_COUNT':
      return hasOnlyKeys(data, ['type', 'count']) &&
        Number.isSafeInteger(data.count) && data.count >= 0 && data.count <= 999999
    default:
      return false
  }
}

function envelope (data) {
  return {
    ...data,
    bridge: BRIDGE_MARKER,
    version: BRIDGE_VERSION,
    channelNonce: getChannelNonce()
  }
}

function sendNow (data) {
  if (!parentOrigin || !window.parent || window.parent === window) return false
  window.parent.postMessage(envelope(data), parentOrigin)
  return true
}

function flushPendingMessages () {
  while (pendingMessages.length > 0) {
    const data = pendingMessages.shift()
    sendNow(data)
  }
}

function handleWindowMessage (event) {
  if (event.source !== window.parent || !isValidParentMessage(event.data)) return
  if (event.data.channelNonce !== getChannelNonce()) return

  if (event.data.type === 'WIDGET_CHANNEL_INIT') {
    if (!expectedParentOrigin || event.origin !== expectedParentOrigin) return
    if (!isTrustedParentOrigin(event.origin, trustedDomains)) return
    if (parentOrigin && event.origin !== parentOrigin) return
    parentOrigin = event.origin
    sendNow({ type: 'WIDGET_CHANNEL_READY' })
    flushPendingMessages()
    return
  }

  if (!parentOrigin || event.origin !== parentOrigin) return
  messageHandler?.(event.data)
}

export function startParentBridge ({ domains = [], expectedOrigin = '', onMessage } = {}) {
  trustedDomains = Array.isArray(domains) ? [...domains] : []
  try {
    const parsed = new URL(expectedOrigin)
    expectedParentOrigin = ['http:', 'https:'].includes(parsed.protocol) &&
      !parsed.username && !parsed.password && expectedOrigin === parsed.origin
      ? parsed.origin
      : ''
  } catch {
    expectedParentOrigin = ''
  }
  messageHandler = typeof onMessage === 'function' ? onMessage : null
  if (started) return
  started = true
  window.addEventListener('message', handleWindowMessage)
}

export function stopParentBridge () {
  if (started) window.removeEventListener('message', handleWindowMessage)
  started = false
  parentOrigin = ''
  expectedParentOrigin = ''
  messageHandler = null
  pendingMessages.length = 0
}

export function postToParent (data) {
  if (!isValidChildMessage(data) || !getChannelNonce()) return false
  if (sendNow(data)) return true
  if (pendingMessages.length >= MAX_QUEUE_LENGTH) pendingMessages.shift()
  pendingMessages.push(data)
  return false
}
