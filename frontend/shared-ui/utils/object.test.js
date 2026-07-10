import { describe, expect, test } from 'vitest'
import { deepMerge } from './object.js'

describe('deepMerge security', () => {
  test('does not pollute object prototypes from parsed JSON', () => {
    const payload = JSON.parse('{"safe":{"value":1},"__proto__":{"polluted":"yes"},"constructor":{"prototype":{"alsoPolluted":true}}}')
    const target = {}

    deepMerge(target, payload)

    expect(target).toEqual({ safe: { value: 1 } })
    expect({}.polluted).toBeUndefined()
    expect({}.alsoPolluted).toBeUndefined()
  })

  test('does not recurse through inherited target properties', () => {
    const inherited = { shared: { untouched: true } }
    const target = Object.create(inherited)
    expect(deepMerge(target, { shared: { value: 1 } })).toBe(target)
    expect(inherited.shared).toEqual({ untouched: true })
  })
})
