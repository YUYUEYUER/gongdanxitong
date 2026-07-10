import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it, vi } from 'vitest'

const widgetSource = readFileSync(
  fileURLToPath(new URL('../../../../../static/widget.js', import.meta.url)),
  'utf8'
)

function loadWidgetConstructor () {
  const fakeWindow = {
    location: {
      href: 'https://shop.example.com/reset/secret?code=oauth#private',
      origin: 'https://shop.example.com',
      hostname: 'shop.example.com'
    }
  }
  const fakeDocument = { readyState: 'loading', addEventListener: vi.fn() }
  new Function('window', 'document', widgetSource)(fakeWindow, fakeDocument)
  return fakeWindow.Libredesk
}

describe('widget embed channel handshake', () => {
  afterEach(() => vi.useRealTimers())

  it('retries until a delayed child listener is ready and then stops', () => {
    vi.useFakeTimers()
    const Libredesk = loadWidgetConstructor()
    const widget = Object.create(Libredesk.prototype)
    const postMessage = vi.fn()

    Object.assign(widget, {
      iframe: { contentWindow: { postMessage } },
      widgetOrigin: 'https://support.example.com',
      channelNonce: 'a'.repeat(48),
      channelReady: false,
      _channelInitTimer: null,
      _channelInitAttempts: 0
    })

    widget.startChannelHandshake()
    expect(postMessage).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(750)
    expect(postMessage).toHaveBeenCalledTimes(4)

    widget.channelReady = true
    widget.stopChannelHandshake()
    vi.advanceTimersByTime(5000)
    expect(postMessage).toHaveBeenCalledTimes(4)
  })

  it('never forwards parent-readable bearer cookies to the iframe', () => {
    const Libredesk = loadWidgetConstructor()
    const widget = Object.create(Libredesk.prototype)
    const postToIframe = vi.fn()
    Object.assign(widget, {
      config: { inboxID: 'inbox-1', userJWT: '' },
      postToIframe,
      sendMobileState: vi.fn()
    })

    widget.handleVueAppReady()
    expect(postToIframe).toHaveBeenCalledWith({ type: 'SESSION_DATA' })
    expect(JSON.stringify(postToIframe.mock.calls)).not.toContain('sessionToken')
    expect(JSON.stringify(postToIframe.mock.calls)).not.toContain('visitorToken')

    postToIframe.mockClear()
    widget.config.userJWT = 'signed-jwt'
    widget.handleVueAppReady()
    expect(postToIframe).toHaveBeenCalledWith({ type: 'SET_JWT_TOKEN', jwt: 'signed-jwt' })
    expect(JSON.stringify(postToIframe.mock.calls)).not.toContain('visitorToken')
  })

  it('reports only the embedding origin and never the page title', () => {
    const Libredesk = loadWidgetConstructor()
    const widget = Object.create(Libredesk.prototype)
    widget.config = {}
    widget.postToIframe = vi.fn()

    expect(widget.getSanitizedPageURL()).toBe('https://shop.example.com/')
    widget.sendPageInfo()
    expect(widget.postToIframe).toHaveBeenCalledWith({
      type: 'PAGE_VISIT',
      url: 'https://shop.example.com/',
      title: ''
    })
  })
})
