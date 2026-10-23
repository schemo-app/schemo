import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const rows = await load('/src/lib/draftRows.ts')

const row = (name, value, extra = {}) => ({ id: '', name, value, enabled: true, ...extra })

test('the query reads as rows in order, decoded, with repeated names kept', () => {
  assert.deepEqual(rows.paramsFromUrl('/a?x=1&x=2&q=one+two&e=%C3%A9&flag').map(r => [r.name, r.value]),
    [['x', '1'], ['x', '2'], ['q', 'one two'], ['e', 'é'], ['flag', '']])
  assert.deepEqual(rows.paramsFromUrl('/a#frag?no=1'), [])
  assert.deepEqual(rows.paramsFromUrl('/a?bad=%E0%A4%A').map(r => r.value), ['%E0%A4%A'])
})

// A URL that has been through the rows comes back byte for byte: sorting the names, escaping the
// braces of a token or encoding the comma of `include=author,comments` would all show up here.
test('an address survives a round trip through its rows', () => {
  for (const raw of [
    '/a?x={{b}}&c=%C3%A9',
    '/a?b=2&a=1',
    '/a?q=one+two',
    'https://api.example.com/articles?include=author,comments&page[number]=2',
    '/a?filter[id][in]=1,2',
    '/a#frag?no=1',
  ]) {
    assert.equal(rows.urlWithParams(rows.splitQuery(raw).base, rows.paramsFromUrl(raw)), raw)
  }
})

test('the query is encoded the way this app writes addresses', () => {
  const cases = {
    '1,2': '1,2', 'page[number]': 'page[number]', '{{token}}': '{{token}}', 'one two': 'one+two',
    'é': '%C3%A9', 'a&b=c': 'a%26b%3Dc', '100%': '100%25', 'a/b?c': 'a%2Fb%3Fc',
    'a-b_c.d*e': 'a-b_c.d*e', "tilde~and'quote": 'tilde~and%27quote', 'a\nb': 'a%0Ab', '': '',
  }
  for (const [input, want] of Object.entries(cases)) assert.equal(rows.encodeQuery(input), want, input)
})

test('only enabled, named rows are written, before the fragment', () => {
  const list = [row('a', '1'), row('off', '2', { enabled: false }), row('  ', '3'), row('empty', '')]
  assert.equal(rows.urlWithParams('/x', list), '/x?a=1&empty=')
  assert.equal(rows.urlWithParams('/x', []), '/x')
  assert.equal(rows.urlWithParams('/x#top', [row('a', '1')]), '/x?a=1#top')
})

test('a row still in the address keeps its id, and a parked row stays', () => {
  const existing = [
    row('a', '1', { id: 'p1' }), row('b', '2', { id: 'p2' }), row('parked', '3', { id: 'p3', enabled: false }),
  ]
  const got = rows.keepRowIdentities([row('a', '1'), row('c', '3')], existing)
  assert.deepEqual(got.map(r => r.id), ['p1', '', 'p3'])
  assert.equal(got[2].enabled, false)
})

test('the path rows follow the address and keep their values', () => {
  assert.deepEqual(rows.pathNames('https://h:8080/users/:id/posts/:post.json?x=:no#:no'), ['id', 'post'])
  const first = rows.pathRows('/users/:id', [])
  assert.equal(first[0].name, 'id')
  assert.ok(first[0].id)
  const filled = [{ ...first[0], value: '42' }]
  const again = rows.pathRows('/users/:id/posts/:post', filled)
  assert.equal(again[0].value, '42')
  assert.equal(again[0].id, first[0].id)
  assert.deepEqual(rows.pathRows('/users', filled), [])
})

test('following an address gives every new row an id', () => {
  const followed = rows.followUrl({ url: '/a/:x?b=1', params: [], pathParams: [] })
  assert.equal(followed.params[0].name, 'b')
  assert.ok(followed.params[0].id)
  assert.equal(followed.pathParams[0].name, 'x')
})
