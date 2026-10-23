import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { addressOf, splitAddress } = await load('/src/lib/address.ts')

test('the authority ends at the first slash, question mark or hash', () => {
  assert.deepEqual(splitAddress('https://api.example.com/users?id=1'), {
    host: 'api.example.com',
    path: '/users?id=1',
  })
  assert.deepEqual(splitAddress('https://api.example.com?id=1'), { host: 'api.example.com', path: '?id=1' })
  assert.deepEqual(splitAddress('https://api.example.com#top'), { host: 'api.example.com', path: '#top' })
  assert.deepEqual(splitAddress('https://api.example.com'), { host: 'api.example.com', path: '' })
  assert.deepEqual(splitAddress('localhost:8443/x'), { host: 'localhost:8443', path: '/x' })
})

test('a template stays as it was typed', () => {
  assert.deepEqual(splitAddress('{{base}}/users/{{id}}'), { host: '{{base}}', path: '/users/{{id}}' })
  assert.equal(addressOf('https://{{host}}/x'), '{{host}}/x')
})
