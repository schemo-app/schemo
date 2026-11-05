import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { mergeRows, parseRows, rowsToText, toggleRows, parsePastedRows, insertRows } = await load('/src/lib/bulkRows.ts')
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

test('commented malformed rows and plain comments do not produce validation errors', () => {
    for (const prefix of ['#', '//']) {
        for (const text of ['missing separator', '"unclosed: value', 'Y: @json "unclosed', 'Y: @json 123', '']) {
            const commented = `Accept: application/json\n  ${prefix} ${text}`
            assert.deepEqual(parseRows(commented), {
                rows: [row('', 'Accept', 'application/json')], errorLine: null,
            })
            assert.equal(parseRows(`${commented}\ninvalid`).errorLine, 3)
        }
    }
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

test('the text editor accepts slash comments without confusing quoted names with disabled rows', () => {
    const original = [row('a', '//name', 'value'), row('b', 'X-Debug', '1', { enabled: false })]
    assert.deepEqual(mergeRows(parseRows(rowsToText(original)).rows, original), original)
    assert.deepEqual(parseRows('// X-Debug: 1').rows.map(({name, enabled}) => [name, enabled]), [['X-Debug', false]])
})

test('the row shortcut toggles selected complete lines and preserves surrounding content', () => {
    const text = 'a: 1\nb: 2\n\nc: 3\nd: 4'
    const changed = toggleRows(text, 6, 15)
    assert.equal(changed.text, 'a: 1\n# b: 2\n\n# c: 3\nd: 4')
    assert.equal(changed.text.slice(changed.start, changed.end), '# b: 2\n\n# c: 3')
    assert.equal(toggleRows(changed.text, changed.start, changed.end).text, text)
    assert.equal(toggleRows('a: 1\nb: 2', 0, 5).text, '# a: 1\nb: 2')
    assert.equal(toggleRows('\na: 1', 0, 0).text, '\na: 1')
    assert.equal(toggleRows('a: 1\nb: 2', 9, 9).text, 'a: 1\n# b: 2')
    assert.equal(toggleRows('// a: 1\n# b: 2', 0, 14).text, 'a: 1\nb: 2')
})

test('table paste reads URLs, queries, name-value lists and spreadsheet columns', () => {
    for (const text of ['?q=hello+world&tag=a&tag=b', 'q=hello+world&tag=a&tag=b', 'https://example.test/?q=hello+world&tag=a&tag=b#ignored']) {
        assert.deepEqual(parsePastedRows(text, 'params').rows.map(row => [row.name, row.value]), [
            ['q', 'hello world'], ['tag', 'a'], ['tag', 'b'],
        ])
    }
    assert.deepEqual(parsePastedRows('Accept: application/json\nX-Tag: a=b', 'headers').rows.map(row => [row.name, row.value]), [
        ['Accept', 'application/json'], ['X-Tag', 'a=b'],
    ])
    assert.deepEqual(parsePastedRows('page\t1\r\nlimit\t20', 'params').rows.map(row => [row.name, row.value]), [['page', '1'], ['limit', '20']])
    assert.deepEqual(parsePastedRows('X: valid\ninvalid', 'headers'), {rows: [], errorLine: 2})
    assert.deepEqual(parsePastedRows('page\t1\ninvalid', 'params'), {rows: [], errorLine: 2})
})

test('table paste appends parameters and replaces header names without case-sensitive duplicates', () => {
    const existing = [row('a', 'Accept', 'application/json'), row('b', 'X-Tag', 'first', {description: 'Keep the note'})]
    const pasted = parseRows('accept: text/plain\nX-Tag: second').rows
    const headers = insertRows(existing, pasted, 'headers')
    assert.deepEqual(headers.map(row => [row.name, row.value]), [['accept', 'text/plain'], ['X-Tag', 'second']])
    assert.equal(headers[1].id, 'b')
    assert.equal(headers[1].description, 'Keep the note')
    const params = insertRows(existing, pasted, 'params')
    assert.deepEqual(params.slice(0, 2), existing)
    assert.equal(params.length, 4)
    assert.ok(params.slice(2).every(row => row.id && !existing.some(old => old.id === row.id)))
})
