import { nextTick, onScopeDispose, ref, type ComponentPublicInstance } from 'vue'
import { CancelError } from '@wailsio/runtime/cancellable'
import { VariableKind, type Variable } from '../../bindings/schemo/internal/domain'
import { useEnvironmentsStore } from '../stores/environments'
import { useSheetNotice } from './useSheetNotice'
import { describeFailure, t } from '../i18n'
import { isVariableName } from '../lib/vars'

type Field = 'name' | 'value'
export type VariableEntry = { scope: string | null; variable: Variable }
type Editing = { scope: string | null; id: string; field: Field; seed: string }

export function useVariableCells(options: {
    rows: () => VariableEntry[]
    vars: (scope: string | null) => Variable[]
    locked: (scope: string | null) => boolean
    save: (scope: string | null, id: string, patch: Partial<Variable>) => Promise<unknown>
}) {
    const environments = useEnvironmentsStore()
    const { setNotice, clearNotice } = useSheetNotice()
    const editing = ref<Editing | null>(null)
    const draft = ref('')
    const input = ref<HTMLInputElement | null>(null)
    // A delayed secret read must not reopen a cell after another click or disposal.
    let opening = 0

    function setCellInput(element: Element | ComponentPublicInstance | null) {
        input.value = element as HTMLInputElement | null
    }

    function isEditingCell(variable: Variable, field: Field, scope: string | null): boolean {
        const cell = editing.value
        return cell?.id === variable.id && cell.field === field && cell.scope === scope
    }

    async function start(variable: Variable, field: Field, scope: string | null) {
        if (options.locked(scope) || isEditingCell(variable, field, scope)) return
        const turn = ++opening
        let seed = variable.name
        if (field === 'value') {
            if (variable.kind === VariableKind.VariableSecret && !environments.isRevealed(variable.id)) {
                try {
                    seed = await environments.valueForEdit(variable.id)
                } catch (error) {
                    if (turn === opening) refused(error)
                    return
                }
                if (turn !== opening) return
            } else {
                seed = environments.effectiveValue(variable)
            }
        }
        editing.value = { scope, id: variable.id, field, seed }
        draft.value = seed
        clearNotice()
        void nextTick(() => input.value?.focus())
    }

    function cancel() {
        opening++
        editing.value = null
        clearNotice()
    }

    onScopeDispose(cancel)

    function commit(): boolean {
        const cell = editing.value
        if (!cell) return false
        if (options.locked(cell.scope)) {
            cancel()
            return false
        }
        const variables = options.vars(cell.scope)
        if (!variables.some(variable => variable.id === cell.id)) {
            cancel()
            return false
        }
        // Focusing and leaving a masked secret must not count as replacing its value.
        if (draft.value === cell.seed) {
            editing.value = null
            clearNotice()
            return true
        }
        const value = cell.field === 'name' ? draft.value.trim() : draft.value
        if (cell.field === 'name') {
            let error = ''
            if (!value) error = t('environments.nameRequired')
            else if (!isVariableName(value)) error = t('environments.nameInvalid')
            // A collection row can receive its environment after its name has been entered.
            else if (cell.scope !== '' && variables.some(variable => variable.id !== cell.id && variable.name === value)) {
                error = t(cell.scope === null ? 'environments.nameTakenGlobals' : 'environments.nameTakenHere')
            }
            if (error) {
                setNotice(error)
                return false
            }
        }
        void options.save(cell.scope, cell.id, { [cell.field]: value }).catch(refused)
        editing.value = null
        clearNotice()
        return true
    }

    function commitFrom(variable: Variable, field: Field) {
        const cell = editing.value
        if (cell?.id === variable.id && cell.field === field) commit()
    }

    function refused(error: unknown) {
        if (!(error instanceof CancelError)) setNotice(describeFailure(error))
    }

    function onCellKeydown(event: KeyboardEvent, variable: Variable, field: Field, scope: string | null) {
        switch (event.key) {
            case 'Escape':
                event.preventDefault()
                cancel()
                break
            case 'Enter':
                event.preventDefault()
                if (commit() && field === 'name') void start(variable, 'value', scope)
                break
            case 'Tab': {
                if (event.shiftKey) return
                event.preventDefault()
                if (!commit()) return
                if (field === 'name') {
                    void start(variable, 'value', scope)
                    return
                }
                const rows = options.rows()
                const index = rows.findIndex(row => row.scope === scope && row.variable.id === variable.id)
                const next = rows[index + 1]
                if (next) void start(next.variable, 'name', next.scope)
                break
            }
        }
    }

    function cancelTop(): boolean {
        if (!editing.value) return false
        cancel()
        return true
    }

    return { draft, setCellInput, isEditingCell, start, commitFrom, onCellKeydown, cancelTop, cancel }
}
