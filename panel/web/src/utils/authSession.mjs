let sessionGeneration = 0
let refreshFlight = null
const generationListeners = new Set()

export function getSessionGeneration() {
  return sessionGeneration
}

export function advanceSessionGeneration() {
  sessionGeneration += 1
  generationListeners.forEach((listener) => listener())
  return sessionGeneration
}

export function isCurrentSession(generation) {
  return generation === sessionGeneration
}

export function isRequestSessionCurrent(generation) {
  return generation !== undefined && isCurrentSession(generation)
}

export function onSessionGenerationChange(listener) {
  generationListeners.add(listener)
  return () => generationListeners.delete(listener)
}

export function refreshSessionSingleFlight(refresh, generation = sessionGeneration) {
  if (!isCurrentSession(generation)) {
    return Promise.reject(new Error('认证会话已变更'))
  }

  if (refreshFlight?.generation === generation) {
    return refreshFlight.promise
  }

  const promise = refresh().finally(() => {
    if (refreshFlight?.promise === promise) {
      refreshFlight = null
    }
  })
  refreshFlight = { generation, promise }
  return promise
}
