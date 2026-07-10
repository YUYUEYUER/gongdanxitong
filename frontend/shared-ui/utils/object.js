const BLOCKED_OBJECT_KEYS = new Set(['__proto__', 'constructor', 'prototype'])

function isPlainObject (value) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false
  const prototype = Object.getPrototypeOf(value)
  return prototype === Object.prototype || prototype === null
}

export function deepMerge (target, source) {
  if (!isPlainObject(target) || !isPlainObject(source)) return target
  for (const key of Object.keys(source)) {
    if (BLOCKED_OBJECT_KEYS.has(key)) continue
    const val = source[key]
    if (isPlainObject(val)) {
      const current = Object.hasOwn(target, key) && isPlainObject(target[key]) ? target[key] : {}
      target[key] = deepMerge(current, val)
    } else {
      target[key] = val
    }
  }
  return target
}
