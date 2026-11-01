import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { buildJsonTree } = await load('/src/lib/jsonTree.ts')
const { tableColumns, tableCell, cellText, tableRows, tableValues } = await load('/src/lib/jsonTable.ts')

test('table columns include fields appearing later and distinguish literal value keys from primitive cells', () => {
    const array = buildJsonTree([{ id: 1, value: 'object' }, { extra: true }, null, 0, false, '']).get('$')
    const columns = tableColumns(array)
    assert.deepEqual(columns.map(column => column.key), [null, 'id', 'value', 'extra'])
    assert.equal(new Set(columns.map(column => column.id)).size, 4)
    assert.equal(tableCell(array.children[0], columns[0]), undefined)
    assert.equal(tableCell(array.children[0], columns[2]), 'object')
    assert.deepEqual(tableValues(array.children.slice(2), [columns[0]]), [['null'], ['0'], ['false'], ['']])
})

test('sorting is numeric, stable, reversible and keeps original paths', () => {
    const array = buildJsonTree({ data: [{ amount: 20 }, { amount: 3 }, { amount: 3 }, {}] }).get('$.data')
    const [column] = tableColumns(array)
    const sorted = tableRows(array, [column], [], { column, descending: false })
    assert.deepEqual(sorted.map(row => row.path), ['$.data[1]', '$.data[2]', '$.data[0]', '$.data[3]'])
    assert.deepEqual(tableRows(array, [column], [], { column, descending: true }).map(row => row.key), [3, 0, 1, 2])
    assert.deepEqual(array.children.map(row => row.key), [0, 1, 2, 3])
})

test('filtering searches visible columns and CSV values contain every matching row in display order', () => {
    const array = buildJsonTree(Array.from({ length: 230 }, (_, id) => ({ id, name: `User ${id}`, hidden: 'secret' }))).get('$')
    const columns = tableColumns(array).slice(0, 2).reverse()
    assert.equal(tableRows(array, columns, ['secret'], null).length, 0)
    const filtered = tableRows(array, columns, ['user', '2'], { column: columns[1], descending: true })
    const values = tableValues(filtered, columns)
    assert.deepEqual(values[0], ['User 229', '229'])
    assert.equal(values.length, 68)
    assert.equal(tableValues(tableRows(array, columns, [], null), columns).length, 230)
})

test('nested values, unusual keys, empty objects and empty arrays remain readable', () => {
    const array = buildJsonTree(JSON.parse('[{"__proto__":"own","a,b":"line 1\\nline 2","nested":{"x":1}},{},[]]')).get('$')
    const columns = tableColumns(array)
    assert.equal(tableCell(array.children[0], columns.find(column => column.key === '__proto__')), 'own')
    assert.equal(tableCell(array.children[1], columns.find(column => column.key === '__proto__')), undefined)
    assert.equal(cellText(tableCell(array.children[0], columns.find(column => column.key === 'nested'))), '{"x":1}')
    assert.deepEqual(tableColumns(buildJsonTree([]).get('$')), [])
})
