import test from 'node:test'
import assert from 'node:assert/strict'
import { commandForMCPForm, envForMCPForm } from './mcpForm.js'

test('MCP edit form reconstructs command arguments without losing spaces', () => {
  assert.equal(
    commandForMCPForm({
      command: '/opt/MCP tools/python',
      args: ['/opt/MCP tools/server.py', '--port', '9000'],
    }),
    "'/opt/MCP tools/python' '/opt/MCP tools/server.py' --port 9000",
  )
})

test('MCP edit form renders environment variables deterministically', () => {
  assert.equal(envForMCPForm({ env: { TOKEN: 'secret', HOST: 'local' } }), 'HOST=local\nTOKEN=secret')
})
