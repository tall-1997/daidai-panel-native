import test from 'node:test'
import assert from 'node:assert/strict'
import {
  advanceSessionGeneration,
  getSessionGeneration,
  isCurrentSession,
  isRequestSessionCurrent,
  onSessionGenerationChange,
  refreshSessionSingleFlight,
} from '../src/utils/authSession.mjs'

test('refreshSessionSingleFlight shares one refresh within a session', async () => {
  let calls = 0
  let release
  const refresh = () => {
    calls += 1
    return new Promise((resolve) => {
      release = resolve
    })
  }

  const first = refreshSessionSingleFlight(refresh)
  const second = refreshSessionSingleFlight(refresh)

  assert.equal(first, second)
  assert.equal(calls, 1)
  release('token')
  assert.deepEqual(await Promise.all([first, second]), ['token', 'token'])
})

test('session generation rejects stale async ownership after session changes', () => {
  const generation = getSessionGeneration()
  assert.equal(isCurrentSession(generation), true)

  advanceSessionGeneration()

  assert.equal(isCurrentSession(generation), false)
  assert.equal(isCurrentSession(getSessionGeneration()), true)
})

test('session generation change notifies active transports once', () => {
  let notifications = 0
  const unsubscribe = onSessionGenerationChange(() => {
    notifications += 1
  })

  advanceSessionGeneration()
  unsubscribe()
  advanceSessionGeneration()

  assert.equal(notifications, 1)
})

test('a late async response cannot commit into a newer session', async () => {
  const generation = getSessionGeneration()
  let release
  const response = new Promise((resolve) => {
    release = resolve
  })
  let committed = ''

  const commit = response.then((value) => {
    if (isCurrentSession(generation)) committed = value
  })
  advanceSessionGeneration()
  release('stale-value')
  await commit

  assert.equal(committed, '')
})

test('a new session starts an independent refresh flight', async () => {
  let releaseOld
  const oldFlight = refreshSessionSingleFlight(() => new Promise((resolve) => {
    releaseOld = resolve
  }))

  advanceSessionGeneration()
  let calls = 0
  const newFlight = refreshSessionSingleFlight(async () => {
    calls += 1
    return 'new-token'
  })

  assert.notEqual(oldFlight, newFlight)
  assert.equal(await newFlight, 'new-token')
  assert.equal(calls, 1)
  releaseOld('old-token')
  assert.equal(await oldFlight, 'old-token')
})

test('a late 401 from an old request cannot use the current session', () => {
  const originalRequestGeneration = getSessionGeneration()

  advanceSessionGeneration()

  assert.equal(isRequestSessionCurrent(originalRequestGeneration), false)
  assert.equal(isRequestSessionCurrent(getSessionGeneration()), true)
  assert.equal(isRequestSessionCurrent(undefined), false)
})

test('a stale request generation cannot start or join a refresh', async () => {
  const staleGeneration = getSessionGeneration()
  advanceSessionGeneration()
  let calls = 0

  await assert.rejects(
    refreshSessionSingleFlight(async () => {
      calls += 1
      return 'new-session-token'
    }, staleGeneration),
    /认证会话已变更/,
  )

  assert.equal(calls, 0)
})

test('a refresh remains owned by the generation that started it', async () => {
  const requestGeneration = getSessionGeneration()
  let release
  const refresh = refreshSessionSingleFlight(() => new Promise((resolve) => {
    release = resolve
  }), requestGeneration)

  advanceSessionGeneration()
  release('old-session-token')

  assert.equal(await refresh, 'old-session-token')
  assert.equal(isRequestSessionCurrent(requestGeneration), false)
})
