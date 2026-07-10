// @vitest-environment jsdom

import { describe, expect, test, vi } from 'vitest'
import {
  consumeSensitiveFragmentToken,
  openSafeExternalUrl,
  sanitizeHttpUrl,
  sanitizeImageUrl,
  sanitizeInternalPath,
  sanitizeLinkUrl,
  sanitizePageUrl,
  sanitizeResourceUrl
} from './url.js'

describe('URL security helpers', () => {
  test('rejects executable and credential-bearing URLs', () => {
    expect(sanitizeHttpUrl('javascript:alert(1)')).toBe('')
    expect(sanitizeHttpUrl('data:text/html,<script>alert(1)</script>')).toBe('')
    expect(sanitizeHttpUrl('https://user:secret@example.com/file')).toBe('')
    expect(sanitizeLinkUrl('vbscript:msgbox(1)')).toBe('#')
    expect(sanitizeResourceUrl('data:image/svg+xml,<svg onload=alert(1)>')).toBe('')
  })

  test('keeps normal HTTP, HTTPS, mailto and CID URLs', () => {
    expect(sanitizeHttpUrl('/uploads/file')).toBe('http://localhost:3000/uploads/file')
    expect(sanitizeHttpUrl('https://cdn.example.com/a.png')).toBe('https://cdn.example.com/a.png')
    expect(sanitizeLinkUrl('mailto:support@example.com')).toBe('mailto:support@example.com')
    expect(sanitizeResourceUrl('cid:logo@example.com')).toBe('cid:logo@example.com')
    expect(sanitizeImageUrl('blob:http://localhost:3000/avatar-id')).toBe('blob:http://localhost:3000/avatar-id')
    expect(sanitizeImageUrl('data:image/svg+xml,<svg onload=alert(1)>')).toBe('')
  })

  test('strips fragments and non-allowlisted query parameters from page visits', () => {
    expect(
      sanitizePageUrl('https://shop.example.com/orders/42?token=secret&lang=zh#reset-token', ['lang'])
    ).toBe('https://shop.example.com/orders/42?lang=zh')
  })

  test('accepts only same-origin redirect paths', () => {
    expect(sanitizeInternalPath('/portal/tickets?tab=open')).toBe('/portal/tickets?tab=open')
    expect(sanitizeInternalPath('//evil.example/path')).toBe('')
    expect(sanitizeInternalPath('https://evil.example/path')).toBe('')
  })

  test('does not open a rejected URL', () => {
    const spy = vi.spyOn(window, 'open').mockImplementation(() => null)
    expect(openSafeExternalUrl('javascript:alert(1)')).toBe(false)
    expect(spy).not.toHaveBeenCalled()
    spy.mockRestore()
  })

  test('consumes a secret from the fragment and scrubs browser history', () => {
    const token = 'A'.repeat(48)
    window.history.replaceState({}, '', `/set-password#token=${token}`)

    expect(consumeSensitiveFragmentToken()).toBe(token)
    expect(window.location.pathname).toBe('/set-password')
    expect(window.location.search).toBe('')
    expect(window.location.hash).toBe('')
  })
})
