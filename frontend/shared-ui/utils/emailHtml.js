function isCssWhitespace (charCode) {
  return charCode === 0x09 || charCode === 0x0a || charCode === 0x0c ||
    charCode === 0x0d || charCode === 0x20
}

function isUrlStart (value, index) {
  if (index + 4 > value.length || value.charCodeAt(index + 3) !== 0x28) return false
  const u = value.charCodeAt(index) | 0x20
  const r = value.charCodeAt(index + 1) | 0x20
  const l = value.charCodeAt(index + 2) | 0x20
  return u === 0x75 && r === 0x72 && l === 0x6c
}

// Parse a single CSS url() token without backtracking. A nested url( boundary
// makes the current token malformed and lets the outer scanner continue there.
function scanCssUrl (value, start) {
  let cursor = start + 4
  while (cursor < value.length && isCssWhitespace(value.charCodeAt(cursor))) cursor++

  const quote = value[cursor] === '"' || value[cursor] === "'" ? value[cursor++] : ''
  const valueStart = cursor

  while (cursor < value.length) {
    if (isUrlStart(value, cursor)) return { malformed: true, resumeAt: cursor }

    const char = value[cursor]
    if (quote) {
      if (char === '\\' && cursor + 1 < value.length) {
        cursor += 2
        continue
      }
      if (char !== quote) {
        cursor++
        continue
      }

      const valueEnd = cursor++
      while (cursor < value.length && isCssWhitespace(value.charCodeAt(cursor))) cursor++
      if (value[cursor] !== ')') return { malformed: true, resumeAt: cursor }
      return { end: cursor + 1, value: value.slice(valueStart, valueEnd).trim() }
    }

    if (char === ')') {
      return { end: cursor + 1, value: value.slice(valueStart, cursor).trim() }
    }
    cursor++
  }

  return { malformed: true, resumeAt: value.length }
}

// lettersanitizer decodes CSS URL values internally. Remove malformed tokens
// before they reach that parser, in linear time even for repeated unclosed url(.
export function stripMalformedCssUrls (html) {
  if (typeof html !== 'string' || !html) return html || ''

  const output = []
  let copyFrom = 0
  let cursor = 0

  while (cursor < html.length) {
    if (!isUrlStart(html, cursor)) {
      cursor++
      continue
    }

    const start = cursor
    const token = scanCssUrl(html, start)
    output.push(html.slice(copyFrom, start))

    if (token.malformed) {
      output.push('url()')
      copyFrom = start + 4
      cursor = Math.max(token.resumeAt, copyFrom)
      continue
    }

    try {
      decodeURI(token.value)
      output.push(html.slice(start, token.end))
    } catch {
      output.push('url()')
    }
    copyFrom = token.end
    cursor = token.end
  }

  output.push(html.slice(copyFrom))
  return output.join('')
}
