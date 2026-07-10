const HTTP_PROTOCOLS = new Set(['http:', 'https:'])
const LINK_PROTOCOLS = new Set(['http:', 'https:', 'mailto:'])
const MAX_URL_INPUT_LENGTH = 4096

function hasControlCharacter (value) {
  return Array.from(value).some((character) => {
    const code = character.charCodeAt(0)
    return code <= 31 || code === 127
  })
}

function baseOrigin () {
  return typeof window !== 'undefined' ? window.location.origin : 'https://localhost'
}

function parseURL (value, base = baseOrigin()) {
  if (typeof value !== 'string') return null
  const input = value.trim()
  if (!input || input.length > MAX_URL_INPUT_LENGTH || hasControlCharacter(input)) return null
  try {
    const parsed = new URL(input, base)
    if (parsed.username || parsed.password) return null
    return parsed
  } catch {
    return null
  }
}

export function sanitizeHttpUrl (value, { sameOrigin = false, base = baseOrigin() } = {}) {
  const parsed = parseURL(value, base)
  if (!parsed || !HTTP_PROTOCOLS.has(parsed.protocol)) return ''
  if (sameOrigin && parsed.origin !== new URL(base).origin) return ''
  return parsed.href
}

export function sanitizeLinkUrl (value, { allowMailto = true, base = baseOrigin() } = {}) {
  const parsed = parseURL(value, base)
  if (!parsed || !(allowMailto ? LINK_PROTOCOLS : HTTP_PROTOCOLS).has(parsed.protocol)) return '#'
  return parsed.href
}

export function sanitizeResourceUrl (value, { allowCid = true, base = baseOrigin() } = {}) {
  if (allowCid && typeof value === 'string' && /^cid:[^\s]+$/i.test(value.trim())) return value.trim()
  return sanitizeHttpUrl(value, { base })
}

export function sanitizeImageUrl (value, { base = baseOrigin() } = {}) {
  const parsed = parseURL(value, base)
  if (!parsed) return ''
  if (HTTP_PROTOCOLS.has(parsed.protocol)) return parsed.href
  if (parsed.protocol === 'blob:' && parsed.origin === new URL(base).origin) return parsed.href
  return ''
}

export function sanitizePageUrl (value, queryAllowlist = []) {
  const parsed = parseURL(value)
  if (!parsed || !HTTP_PROTOCOLS.has(parsed.protocol)) return ''

  const output = new URL(parsed.pathname, parsed.origin)
  const allowed = new Set(
    (Array.isArray(queryAllowlist) ? queryAllowlist : [])
      .filter((key) => typeof key === 'string' && /^[A-Za-z0-9_.-]{1,64}$/.test(key))
  )
  for (const [key, val] of parsed.searchParams) {
    if (allowed.has(key) && val.length <= 256) output.searchParams.append(key, val)
  }
  // URL fragments are intentionally never copied: they commonly contain OAuth or reset tokens.
  return output.href.slice(0, 2048)
}

export function sanitizeInternalPath (value, base = baseOrigin()) {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//')) return ''
  const parsed = parseURL(value, base)
  if (!parsed || parsed.origin !== new URL(base).origin) return ''
  return `${parsed.pathname}${parsed.search}`
}

export function consumeSensitiveFragmentToken (legacyQueryToken = '') {
  if (typeof window === 'undefined') return ''
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const token = String(fragment.get('token') || legacyQueryToken || '').trim()
  window.history.replaceState({}, '', window.location.pathname)
  return /^[A-Za-z0-9_-]{20,512}$/.test(token) ? token : ''
}

export function openSafeExternalUrl (value) {
  const safe = sanitizeHttpUrl(value)
  if (!safe || typeof window === 'undefined') return false
  window.open(safe, '_blank', 'noopener,noreferrer')
  return true
}
