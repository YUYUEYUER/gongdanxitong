import { describe, expect, test } from 'vitest'
import { isTrustedParentOrigin, isValidChildMessage, isValidParentMessage } from './parentBridge.js'

const base = {
  bridge: 'libredesk-widget-bridge',
  version: 1,
  channelNonce: 'abcdefghijklmnopqrstuvwxyz123456',
}

describe('widget parent bridge validation', () => {
  test('matches exact and wildcard trusted domains without suffix confusion', () => {
    expect(isTrustedParentOrigin('https://shop.example.com', ['*.example.com'])).toBe(true)
    expect(isTrustedParentOrigin('https://example.com', ['*.example.com'])).toBe(false)
    expect(isTrustedParentOrigin('https://example.com.evil.test', ['*.example.com'])).toBe(false)
    expect(isTrustedParentOrigin('https://example.com', ['example.com:443'])).toBe(true)
    expect(isTrustedParentOrigin('https://example.com:8443', ['example.com:443'])).toBe(false)
    expect(isTrustedParentOrigin('javascript://example.com', ['example.com'])).toBe(false)
  })

  test('requires the protocol envelope and exact message schema', () => {
    expect(isValidParentMessage({ ...base, type: 'WIDGET_OPENED' })).toBe(true)
    expect(isValidParentMessage({ ...base, type: 'SESSION_DATA' })).toBe(true)
    expect(isValidParentMessage({ ...base, type: 'SESSION_DATA', sessionToken: 'smuggled' })).toBe(false)
    expect(isValidParentMessage({ ...base, type: 'SET_JWT_TOKEN', jwt: 'signed', visitorToken: 'smuggled' })).toBe(false)
    expect(isValidParentMessage({ ...base, type: 'WIDGET_OPENED', token: 'smuggled' })).toBe(false)
    expect(isValidParentMessage({ ...base, type: 'SET_MOBILE_STATE', isMobile: 'yes' })).toBe(false)
    expect(isValidParentMessage({ ...base, type: 'UNKNOWN' })).toBe(false)
  })

  test('rejects malformed child messages and oversized tokens', () => {
    expect(isValidChildMessage({ type: 'UPDATE_UNREAD_COUNT', count: 2 })).toBe(true)
    expect(isValidChildMessage({ type: 'SESSION_CLEARED' })).toBe(true)
    expect(isValidChildMessage({ type: 'SESSION_CLEAR_FAILED' })).toBe(true)
    expect(isValidChildMessage({ type: 'SESSION_CLEAR_FAILED', error: 'detail' })).toBe(false)
    expect(isValidChildMessage({ type: 'UPDATE_UNREAD_COUNT', count: -1 })).toBe(false)
    expect(isValidChildMessage({ type: 'STORE_SESSION', token: 'legacy-token' })).toBe(false)
  })
})
