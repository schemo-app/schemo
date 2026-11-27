import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { sideBySide, fold } = await load('/src/lib/lineDiff.ts')

const drawn = (rows) => rows.map((row) => [row.left?.text ?? null, row.right?.text ?? null, row.changed])

test('changed lines share a row and added ones stand alone', () => {
  assert.deepEqual(drawn(sideBySide('{\n  "title": "Lamp"\n}', '{\n  "price": 0,\n  "title": "string"\n}')), [
    ['{', '{', false],
    ['  "title": "Lamp"', '  "price": 0,', true],
    [null, '  "title": "string"', true],
    ['}', '}', false],
  ])
})

test('an empty side leaves the other one alone', () => {
  assert.deepEqual(drawn(sideBySide('', 'a\nb')), [
    [null, 'a', true],
    [null, 'b', true],
  ])
  assert.deepEqual(drawn(sideBySide('a', '')), [['a', null, true]])
})

test('line numbers follow each side', () => {
  const rows = sideBySide('a\nb\nc', 'a\nc')
  assert.deepEqual(rows.map((row) => [row.left?.n ?? null, row.right?.n ?? null]), [
    [1, 1],
    [2, null],
    [3, 2],
  ])
})

test('unchanged lines far from a change are counted, not shown', () => {
  const before = Array.from({ length: 12 }, (_, at) => `line ${at}`).join('\n')
  const rows = fold(sideBySide(before, before.replace('line 11', 'changed')), 2)
  assert.deepEqual(rows[0], { skipped: 9 })
  assert.equal(rows.length, 4)
})
