import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { mergeRows, parseRows, rowsToText } = await load('/src/lib/bulkRows.ts')
const row = (id, name, value, extra = {}) => ({ id, name, value, enabled: true, ...extra })

test('bulk editing round trips disabled rows, duplicate names, notes, empty values and escaped text', () => {
    const original = [
        row('a', 'Accept', 'application/json', { description: 'The response format' }),
        row('b', 'X-Tag', 'first'),
        row('c', 'X-Tag', 'second', { enabled: false, description: 'Keep this one' }),
        row('d', '', ''),
        row('e', '#special:key=', ' line one\nline two\r\n"quoted" '),
        row('f', 'Unicode', '日本語'),
        row('g', 'If-Match', '"abc"'),
        row('h', 'Literal', '@json "keep the marker"'),
    ]
    const parsed = parseRows(rowsToText(original))
    assert.equal(parsed.errorLine, null)
    assert.deepEqual(mergeRows(parsed.rows, original), original)
})

test('headers split on the first separator and query text accepts equals without decoding values', () => {
    const parsed = parseRows('Location: https://example.test:8443/a?x=1\r\nq=a=b:c\r\n# off: {{token}}\r\nempty:\r\n')
    assert.equal(parsed.errorLine, null)
    assert.deepEqual(parsed.rows.map(({ name, value, enabled }) => [name, value, enabled]), [
        ['Location', 'https://example.test:8443/a?x=1', true], ['q', 'a=b:c', true],
        ['off', '{{token}}', false], ['empty', '', true],
    ])
    assert.ok(parsed.rows.every(row => row.id === ''))
    assert.ok(mergeRows(parsed.rows, []).every(row => row.id))
})

test('editing one duplicate preserves the exact duplicate and both descriptions', () => {
    const original = [row('one', 'tag', 'a', { description: 'First' }), row('two', 'tag', 'b', { description: 'Second' })]
    const rows = mergeRows(parseRows('tag: changed\ntag: a').rows, original)
    assert.deepEqual(rows.map(row => [row.id, row.description]), [['two', 'Second'], ['one', 'First']])
    assert.deepEqual(original.map(row => row.value), ['a', 'b'])
})

test('malformed lines report their original line number and never return a partial replacement', () => {
    for (const text of ['X: valid\n\nmissing separator', 'X: valid\n\n"unclosed: value', 'X: valid\n\nY: @json "unclosed']) {
        assert.deepEqual(parseRows(text), { rows: [], errorLine: 3 })
    }
    assert.deepEqual(parseRows(''), { rows: [], errorLine: null })
})

test('quoted header values pasted from network tools stay literal', () => {
    const parsed = parseRows('If-Match: "abc"\nETag: W/"version-2"')
    assert.deepEqual(parsed.rows.map(row => row.value), ['"abc"', 'W/"version-2"'])
})

test('removing and reordering rows is explicit in bulk text', () => {
    const original = [row('a', 'a', '1'), row('b', 'b', '2', { enabled: false }), row('c', 'c', '3')]
    const rows = mergeRows(parseRows('c: 3\na: 1').rows, original)
    assert.deepEqual(rows.map(row => row.id), ['c', 'a'])
})
