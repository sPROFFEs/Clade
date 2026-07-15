import test from 'node:test'
import assert from 'node:assert/strict'
import { encodeTerminalInput, findTerminalForChat } from './terminal.js'

test('terminal input is encoded as lossless UTF-8', () => {
  const input = 'Canción, pingüino, ¿qué tal? — € 😀 e\u0301\t\r\x1b[A'
  const decoded = Buffer.from(encodeTerminalInput(input), 'base64').toString('utf8')
  assert.equal(decoded, input)
})

test('terminal input supports a large paste without overflowing argument limits', () => {
  const input = 'á漢😀'.repeat(30_000)
  const decoded = Buffer.from(encodeTerminalInput(input), 'base64').toString('utf8')
  assert.equal(decoded, input)
})

test('a code chat recovers its unique old unbound terminal', () => {
  const chat = { ID: 'chat-1', CLIAgent: 'praimate-code', WorkspacePath: '/tmp/demo/' }
  const terms = [
    { id: 'wrong-cli', name: 'claude', cwd: '/tmp/demo' },
    { id: 'right', name: 'praimate-code', cwd: '/tmp/demo' },
  ]
  assert.equal(findTerminalForChat(terms, chat)?.id, 'right')
})

test('terminal recovery refuses ambiguous sessions', () => {
  const chat = { ID: 'chat-1', CLIAgent: 'praimate-code', WorkspacePath: '/tmp/demo' }
  const terms = [
    { id: 'one', name: 'praimate-code', cwd: '/tmp/demo' },
    { id: 'two', name: 'praimate-code', cwd: '/tmp/demo/' },
  ]
  assert.equal(findTerminalForChat(terms, chat), null)
})
