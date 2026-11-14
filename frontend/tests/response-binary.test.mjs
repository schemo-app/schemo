import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { bodyMissing, recordView } = await load('/src/lib/requestRecord.ts')

const record = (responseBody) => ({ id: 'rec', status: 200, responseBody })

test('a binary answer is shown as binary and is never fetched as text', () => {
  const binary = { size: 111091, binary: true }
  assert.equal(bodyMissing(binary), false)
  const view = recordView(record(binary))
  assert.equal(view.responseBinary, true)
  assert.equal(view.responseBody, '')
})

test('a body fetched later says whether it is binary', () => {
  const view = recordView(record({ size: 12 }), { response: { size: 12, binary: true } })
  assert.equal(view.responseBinary, true)
})

test('a text answer still reads as text, and one left out is fetched', () => {
  assert.equal(bodyMissing({ size: 12 }), true)
  const view = recordView(record({ size: 11, inline: '{"data":[]}' }))
  assert.equal(view.responseBinary, false)
  assert.equal(view.responseBody, '{"data":[]}')
})
