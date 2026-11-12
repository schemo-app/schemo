import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { Indent } = await load('/bindings/schemo/internal/domain/models.ts')
const { formatResponse, responseParser } = await load('/src/lib/responseFormat.ts')

test('formatting JSON keeps number literals, escape sequences and key order', async () => {
  const parser = responseParser('Application/Problem+JSON; charset=utf-8', null)
  const source = String.raw`{"z":9007199254740993,"s":"\u0061","a":1.0,"b":1e+1000}`
  const formatted = await formatResponse(source, parser, Indent.IndentFour)

  assert.equal(formatted, [
    '{',
    '    "z": 9007199254740993,',
    String.raw`    "s": "\u0061",`,
    '    "a": 1.0,',
    '    "b": 1e+1000',
    '}',
    '',
  ].join('\n'))
})

test('the response formatter uses the declared markup and document formats', async () => {
  const fixtures = [
    ['text/html; charset=utf-8', '<main><p>one</p><p>two</p></main>', /\n  <p>one<\/p>/],
    ['application/soap+xml', '<root><item>one</item></root>', /\n  <item>one<\/item>/],
    ['application/yaml', 'items: [1,2]', /items: \[1, 2\]/],
    ['text/css', 'p{color:red}', /\n  color: red;/],
    ['text/javascript', 'const values=[1,2]', /const values = \[1, 2\]/],
    ['application/typescript', 'const value:number=1', /const value: number = 1/],
    ['application/graphql', '{viewer{id}}', /\n  viewer \{/],
    ['text/markdown', '#Title\n\n-  One', /\n- One/],
  ]

  for (const [media, source, expected] of fixtures) {
    const parser = responseParser(media, null)
    assert.match(await formatResponse(source, parser, Indent.IndentTwo), expected, media)
  }
  assert.equal(responseParser('application/octet-stream', null), undefined)
  assert.equal(responseParser('text/plain', null), undefined)
})
