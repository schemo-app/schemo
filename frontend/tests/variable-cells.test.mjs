import assert from 'node:assert/strict'
import { test } from 'node:test'
import { effectScope, reactive } from 'vue'
import { vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
    '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => globalThis.cellSecrets',
})
const { useVariableCells } = await load('/src/composables/useVariableCells.ts')
const { VariableKind } = await load('/bindings/schemo/internal/domain/models.ts')

function editor(context, rows, secrets = {}) {
    const lifetime = effectScope()
    context.after(() => lifetime.stop())
    const writes = []
    globalThis.cellSecrets = {effectiveValue: variable => variable.value, isRevealed: () => false, ...secrets}
    const cells = lifetime.run(() => useVariableCells({
        rows: () => rows,
        vars: scope => rows.filter(row => row.scope === scope).map(row => row.variable),
        locked: () => false,
        async save(scope, id, patch) {
            writes.push({scope, id, patch})
            Object.assign(rows.find(row => row.scope === scope && row.variable.id === id).variable, patch)
        },
    }))
    return {cells, writes, lifetime}
}

for (const scopeId of [null, 'environment']) {
    for (const field of ['name', 'value']) {
        test(`clicking the active ${field} input preserves and saves its draft in ${scopeId ?? 'globals'}`, async context => {
            const variable = reactive({id: 'variable', name: 'base_url', value: 'https://before.test', kind: VariableKind.VariableText})
            const {cells, writes} = editor(context, [{scope: scopeId, variable}])
            await cells.start(variable, field, scopeId)
            const changed = field === 'name' ? 'api_url' : 'https://after.test'
            cells.draft.value = changed
            await cells.start(variable, field, scopeId)
            await cells.start({...variable}, field, scopeId)
            assert.equal(cells.draft.value, changed)
            assert.equal(cells.isEditingCell(variable, field, scopeId), true)
            assert.deepEqual(writes, [])
            cells.commitFrom(variable, field)
            assert.deepEqual(writes, [{scope: scopeId, id: variable.id, patch: {[field]: changed}}])
            assert.equal(variable[field], changed)
        })
    }
}

test('clicking an active masked-secret input does not fetch its original value again', async context => {
    const variable = {id: 'secret', name: 'token', kind: VariableKind.VariableSecret}
    let reads = 0
    const {cells, writes} = editor(context, [{scope: 'environment', variable}], {
        async valueForEdit() {reads++; return 'original test secret'},
    })
    await cells.start(variable, 'value', 'environment')
    cells.draft.value = 'replacement test secret'
    await cells.start(variable, 'value', 'environment')
    assert.equal(reads, 1)
    assert.equal(cells.draft.value, 'replacement test secret')
    cells.commitFrom(variable, 'value')
    assert.deepEqual(writes.map(write => write.patch), [{value: 'replacement test secret'}])
})

test('Tab keeps each collection row in its own environment scope', async context => {
    const own = {id: 'own', name: 'baseUrl', value: 'collection', kind: VariableKind.VariableText}
    const block = {id: 'block', name: 'baseUrl', value: 'local', kind: VariableKind.VariableText}
    const {cells, writes} = editor(context, [{scope: '', variable: own}, {scope: 'local', variable: block}])
    await cells.start(own, 'name', '')
    cells.onCellKeydown({key: 'Tab', preventDefault() {}}, own, 'name', '')
    assert.equal(cells.isEditingCell(own, 'value', ''), true)
    cells.onCellKeydown({key: 'Tab', preventDefault() {}}, own, 'value', '')
    assert.equal(cells.isEditingCell(block, 'name', 'local'), true)
    cells.draft.value = 'apiUrl'
    cells.commitFrom(block, 'name')
    assert.deepEqual(writes, [{scope: 'local', id: 'block', patch: {name: 'apiUrl'}}])
})

test('disposing an editor prevents a pending secret read from reopening a cell', async context => {
    const variable = {id: 'secret', name: 'token', kind: VariableKind.VariableSecret}
    let resolve
    const read = new Promise(done => { resolve = done })
    const {cells, lifetime} = editor(context, [{scope: 'local', variable}], {valueForEdit: () => read})
    const opening = cells.start(variable, 'value', 'local')
    lifetime.stop()
    resolve('test-only-value')
    await opening
    assert.equal(cells.isEditingCell(variable, 'value', 'local'), false)
    assert.equal(cells.draft.value, '')
})
