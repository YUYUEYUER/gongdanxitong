import { describe, expect, test } from 'vitest'
import { stripMalformedCssUrls } from './emailHtml.js'

describe('HTML mail CSS hardening', () => {
  test('removes malformed percent-encoded CSS URLs without throwing', () => {
    const input = '<div style="background:url(%E0%A4%A)">message</div>'
    expect(() => stripMalformedCssUrls(input)).not.toThrow()
    expect(stripMalformedCssUrls(input)).toBe('<div style="background:url()">message</div>')
  })

  test('preserves valid resource URLs for the normal sanitizer policy', () => {
    const input = '<div style="background:url(https://cdn.example.com/image.png)">message</div>'
    expect(stripMalformedCssUrls(input)).toBe(input)
  })

  test('sanitizes a large run of unclosed CSS URLs in linear time', () => {
    const tokenCount = 120_000
    const input = 'url('.repeat(tokenCount)

    expect(stripMalformedCssUrls(input)).toBe('url()'.repeat(tokenCount))
  }, 5000)
})
