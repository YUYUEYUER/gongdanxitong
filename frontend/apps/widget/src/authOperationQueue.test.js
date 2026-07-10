import { describe, expect, test } from 'vitest'
import { createOrderedAuthHandler } from './authOperationQueue.js'

const deferred = () => {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}

describe('widget authentication operation queue', () => {
  test('preserves arrival order and never lets a late exchange resurrect logout', async () => {
    const exchange = deferred()
    const events = []
    const dispatch = createOrderedAuthHandler(async ({ type }) => {
      events.push(`${type}:start`)
      if (type === 'SET_JWT_TOKEN') await exchange.promise
      events.push(`${type}:end`)
    })

    const login = dispatch({ type: 'SET_JWT_TOKEN' })
    const logout = dispatch({ type: 'CLEAR_SESSION' })
    await Promise.resolve()
    expect(events).toEqual(['SET_JWT_TOKEN:start'])

    exchange.resolve()
    await Promise.all([login, logout])
    expect(events).toEqual([
      'SET_JWT_TOKEN:start',
      'SET_JWT_TOKEN:end',
      'CLEAR_SESSION:start',
      'CLEAR_SESSION:end'
    ])
  })

  test('continues after a failed auth operation', async () => {
    const events = []
    const dispatch = createOrderedAuthHandler(async ({ type }) => {
      events.push(type)
      if (type === 'SESSION_DATA') throw new Error('expected')
    })

    await expect(dispatch({ type: 'SESSION_DATA' })).rejects.toThrow('expected')
    await dispatch({ type: 'CLEAR_SESSION' })
    expect(events).toEqual(['SESSION_DATA', 'CLEAR_SESSION'])
  })
})
