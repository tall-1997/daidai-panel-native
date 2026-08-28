import test from 'node:test'
import assert from 'node:assert/strict'
import { TerminalLogBuffer } from '../src/utils/terminalLogBuffer.mjs'

test('appends chunks while preserving line boundaries', () => {
  const buffer = new TerminalLogBuffer()
  buffer.append('first\nsec')
  buffer.append('ond\nthird')
  assert.equal(buffer.text, 'first\nsecond\nthird')
  assert.equal(buffer.lineCount, 3)
})

test('bare carriage returns overwrite the current terminal line across chunks', () => {
  const buffer = new TerminalLogBuffer()
  buffer.append('progress 10%\r')
  assert.equal(buffer.text, 'progress 10%')
  buffer.append('progress 20%\rprogress 30%\nfinished')
  assert.equal(buffer.text, 'progress 30%\nfinished')
})

test('a CRLF split across chunks commits one line', () => {
  const buffer = new TerminalLogBuffer()
  buffer.append('one\r')
  buffer.append('\ntwo')
  assert.equal(buffer.text, 'one\ntwo')
  assert.equal(buffer.lineCount, 2)
})

test('enforces line limit and adds one truncation marker', () => {
  const buffer = new TerminalLogBuffer({ maxLines: 3, maxChars: 100 })
  buffer.append('one\ntwo\nthree\nfour')
  assert.equal(buffer.text, '[... earlier output truncated ...]\nthree\nfour')
  assert.equal(buffer.lineCount, 3)
})

test('enforces character limit for a single long line', () => {
  const buffer = new TerminalLogBuffer({ maxChars: 40, maxLines: 10 })
  buffer.append('abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz')
  assert.equal(buffer.charCount, 40)
  assert.match(buffer.text, /^\[\.\.\. earlier output truncated \.\.\.\]\n/)
  const retained = buffer.text.split('\n').at(-1)
  assert.equal('abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz'.endsWith(retained), true)
})

test('character trimming never retains half of a UTF-16 surrogate pair', () => {
  const buffer = new TerminalLogBuffer({
    maxChars: 13,
    maxLines: 10,
    marker: '[cut]'
  })
  buffer.append(`prefix😀suffix`)
  assert.equal(buffer.text, '[cut]\nsuffix')
  assert.doesNotMatch(buffer.text, /[\uD800-\uDFFF]/u)
})

test('line trimming never starts a retained line with a low surrogate', () => {
  const buffer = new TerminalLogBuffer({
    maxChars: 8,
    maxLines: 10,
    marker: '[cut]'
  })
  buffer.append('old\na😀bc')
  assert.equal(buffer.text, '[cut]\nbc')
  assert.doesNotMatch(buffer.text, /[\uD800-\uDFFF]/u)
})
