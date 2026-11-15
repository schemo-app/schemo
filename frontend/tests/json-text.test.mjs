import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { prettyJson, prettyJsonText } = await load('/src/lib/json.ts')

test('an integer past 2^53 keeps the digits it was sent with', () => {
  assert.equal(prettyJsonText('{"ledger":9007199254740993,"n":-12345678901234567890}', 0),
    '{"ledger":9007199254740993,"n":-12345678901234567890}')
})

test('everything else is indented as the parsed value would be', () => {
  const text = '{"a":1.50,"b":[1e3,2],"c":"x","a":2}'
  assert.equal(prettyJsonText(text), prettyJson(JSON.parse(text)))
})

test('a text that is not JSON gives nothing', () => {
  assert.equal(prettyJsonText('<catalog/>'), null)
})
