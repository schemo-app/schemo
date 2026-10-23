import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { tabBadge } = await load('/src/lib/responseView.ts')

const record = (headers, cookies) => ({
  responseHeaders: new Array(headers).fill({ name: 'X', value: 'y' }),
  cookieWrites: new Array(cookies).fill({}),
})

const run = (...passed) => ({ tests: passed.map((one) => ({ passed: one })) })

test('headers and cookies badges count their rows', () => {
  assert.deepEqual(tabBadge('headers', record(3, 0), []), { count: '3', failed: false })
  assert.deepEqual(tabBadge('cookies', record(0, 2), []), { count: '2', failed: false })
})

test('an empty section has no count rather than a zero', () => {
  assert.deepEqual(tabBadge('headers', record(0, 0), []), { count: '', failed: false })
  assert.deepEqual(tabBadge('cookies', record(0, 0), []), { count: '', failed: false })
})

test('scripts count the passed tests out of all that ran', () => {
  const runs = [run(true, true), run(true)]
  assert.deepEqual(tabBadge('scripts', record(0, 0), runs), { count: '3/3', failed: false })
})

test('a failed test marks the scripts badge', () => {
  const runs = [run(true, false), run(true)]
  assert.deepEqual(tabBadge('scripts', record(0, 0), runs), { count: '2/3', failed: true })
})

test('scripts without tests have no badge, whether or not a run exists', () => {
  assert.deepEqual(tabBadge('scripts', record(0, 0), []), { count: '', failed: false })
  assert.deepEqual(tabBadge('scripts', record(0, 0), [{ tests: null }, {}]), { count: '', failed: false })
})

test('readings without a count carry no badge', () => {
  for (const tab of ['body', 'timings', 'request']) {
    assert.deepEqual(tabBadge(tab, record(4, 4), [run(false)]), { count: '', failed: false })
  }
})
