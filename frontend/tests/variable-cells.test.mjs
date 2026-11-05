import assert from 'node:assert/strict'
import { test } from 'node:test'
import { effectScope, reactive } from 'vue'
import { vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
})
const { useVariableCells } = await load('/src/composables/useVariableCells.ts')
const { VariableKind } = await load('/bindings/schemo/internal/domain/models.ts')

for (const scopeId of [null, 'environment']) {
    for (const field of ['name', 'value']) {
        test(`clicking the active ${field} input preserves and saves its draft in ${scopeId ?? 'globals'}`, async context => {
            const variable = reactive({id: 'variable', name: 'base_url', value: 'https://before.test', kind: VariableKind.VariableText})
            const writes = []
            const environments = {
                effectiveValue: variable => variable.value,
                varsOf: () => [variable],
                async updateVar(scope, id, patch) {writes.push({scope, id, patch}); Object.assign(variable, patch)},
            }
            const scope = effectScope()
            context.after(() => scope.stop())
            const cells = scope.run(() => useVariableCells(environments, {
                vars: () => [variable], scope: () => scopeId, locked: () => false,
            }))
            await cells.start(variable, field)
            const changed = field === 'name' ? 'api_url' : 'https://after.test'
            cells.draft.value = changed
            await cells.start(variable, field)
            await cells.start({...variable}, field)
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
    const writes = []
    const scope = effectScope()
    context.after(() => scope.stop())
    const cells = scope.run(() => useVariableCells({
        isRevealed: () => false,
        async valueForEdit() {reads++; return 'original test secret'},
        varsOf: () => [variable],
        async updateVar(scope, id, patch) {writes.push(patch)},
    }, {vars: () => [variable], scope: () => 'environment', locked: () => false}))
    await cells.start(variable, 'value')
    cells.draft.value = 'replacement test secret'
    await cells.start(variable, 'value')
    assert.equal(reads, 1)
    assert.equal(cells.draft.value, 'replacement test secret')
    cells.commitFrom(variable, 'value')
    assert.deepEqual(writes, [{value: 'replacement test secret'}])
})
