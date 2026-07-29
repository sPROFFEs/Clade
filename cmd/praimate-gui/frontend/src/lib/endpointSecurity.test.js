import test from 'node:test'
import assert from 'node:assert/strict'
import { endpointTransport } from './endpointSecurity.js'

test('empty and HTTPS endpoints do not show an HTTP warning', () => {
  assert.deepEqual(endpointTransport(''), { insecure: false, loopback: false })
  assert.deepEqual(endpointTransport('HTTPS://llm.example.com/v1'), {
    insecure: false,
    loopback: false,
  })
})

test('explicit and implied HTTP loopback endpoints are identified', () => {
  assert.deepEqual(endpointTransport('http://localhost:11434'), {
    insecure: true,
    loopback: true,
  })
  assert.deepEqual(endpointTransport('127.0.0.1:8000/v1'), {
    insecure: true,
    loopback: true,
  })
  assert.deepEqual(endpointTransport('http://[::1]:11434'), {
    insecure: true,
    loopback: true,
  })
})

test('explicit and implied remote HTTP endpoints are identified', () => {
  assert.deepEqual(endpointTransport('http://llm.example.com/v1'), {
    insecure: true,
    loopback: false,
  })
  assert.deepEqual(endpointTransport('192.168.1.20:11434'), {
    insecure: true,
    loopback: false,
  })
})
