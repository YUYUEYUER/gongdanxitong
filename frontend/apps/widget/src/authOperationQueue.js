const AUTH_MESSAGE_TYPES = new Set(['SESSION_DATA', 'SET_JWT_TOKEN', 'CLEAR_SESSION'])

// Authentication messages mutate the same cookie and in-memory state. Keep
// their arrival order even when an earlier network request is slow.
export function createOrderedAuthHandler (handler) {
  let tail = Promise.resolve()

  return function dispatch (data) {
    if (!AUTH_MESSAGE_TYPES.has(data?.type)) return handler(data)

    const operation = tail.then(() => handler(data))
    tail = operation.catch(() => undefined)
    return operation
  }
}
