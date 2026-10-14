import { nextTick, ref, type ComponentPublicInstance } from 'vue'
import { VariableKind, type Variable } from '../../bindings/schemo/internal/domain'
import type { useEnvironmentsStore } from '../stores/environments'
import { useSheetNotice } from './useSheetNotice'
import { describeFailure, useMessages } from '../i18n'
import { isVariableName } from '../lib/vars'
import { CancelError } from '@wailsio/runtime'

/** The cell being written: the row, the field, the scope the row lives in, and what it opened on. */
type Editing = { scope: string | null; varId: string; field: 'name' | 'value'; seed: string }

// The cell editor of a variables table: Enter commits and moves on, Escape puts the old value back,
// Tab walks down the names. A refusal leaves the row as it was and is said in the table's notice.
export function useVariableCells(
  store: ReturnType<typeof useEnvironmentsStore>,
  opts: {
    vars: () => Variable[]
    scope: () => string | null
    locked: () => boolean
  }
) {
  const { setNotice, clearNotice } = useSheetNotice()
  const { t } = useMessages()

  const editing = ref<Editing | null>(null)
  const draft = ref('')
  const cellInput = ref<HTMLInputElement | null>(null)

  // The cell asked for last is the one that opens: a value an earlier click fetched must not
  // reopen a cell the pointer — or Escape — has since left.
  let opening = 0

  function setCellInput(el: Element | ComponentPublicInstance | null) {
    cellInput.value = (el as HTMLInputElement | null) ?? null
  }

  function isEditingCell(v: Variable, field: 'name' | 'value', scope: string | null): boolean {
    const ed = editing.value
    return Boolean(ed && ed.varId === v.id && ed.field === field && ed.scope === scope)
  }

  async function start(v: Variable, field: 'name' | 'value', scope: string | null = opts.scope()) {
    if (opts.locked()) return
    const mine = ++opening

    let seed: string
    if (field === 'name') {
      seed = v.name
    } else if (v.kind === VariableKind.VariableSecret && !store.isRevealed(v.id)) {
      // A masked secret's value is not in the row — the DTO withholds it — and a cell opened on
      // nothing would write that nothing back. So the cell opens only once the value is in hand.
      try {
        seed = await store.valueForEdit(v.id)
      } catch (error) {
        if (mine === opening && !(error instanceof CancelError)) setNotice(describeFailure(error))
        return
      }
      if (mine !== opening) return
    } else {
      seed = store.effectiveValue(v)
    }

    editing.value = { scope, varId: v.id, field, seed }
    draft.value = seed
    clearNotice()
    nextTick(() => cellInput.value?.focus())
  }

  function cancel() {
    opening++
    editing.value = null
    clearNotice()
  }

  // Only the row being written commits on blur; another row's input is a stale event by then.
  function commitFrom(v: Variable, field: 'name' | 'value') {
    const ed = editing.value
    if (!ed || ed.varId !== v.id || ed.field !== field) return
    commit()
  }

  function commit(): boolean {
    const ed = editing.value
    if (!ed) return false
    const scopeVars = store.varsOf(ed.scope)
    const v = scopeVars.find((x) => x.id === ed.varId)
    if (!v) {
      editing.value = null
      return false
    }
    // A cell left as it opened writes nothing: a click in and out of a secret's value must not
    // count as typing in it.
    if (draft.value === ed.seed) {
      editing.value = null
      clearNotice()
      return true
    }
    if (ed.field === 'name') {
      const name = draft.value.trim()
      if (!name) {
        setNotice(t('environments.nameRequired'))
        return false
      }
      if (!isVariableName(name)) {
        setNotice(t('environments.nameInvalid'))
        return false
      }
      if (scopeVars.some((x) => x.id !== v.id && x.name === name)) {
        setNotice(ed.scope === null ? t('environments.nameTakenGlobals') : t('environments.nameTakenHere'))
        return false
      }
      // A refusal from Go is shown like one of ours: the write is where a name the database already
      // holds is caught, and a rejected call would otherwise leave the row looking at its old name
      // with nothing to say about why.
      void store.updateVar(ed.scope, v.id, { name }).catch(refused)
    } else {
      void store.updateVar(ed.scope, v.id, { value: draft.value }).catch(refused)
    }
    editing.value = null
    clearNotice()
    return true
  }

  function refused(error: unknown) {
    if (error instanceof CancelError) return
    setNotice(describeFailure(error))
  }

  // Where Tab leaves the caret: down the names, and the last value ends the walk.
  function moveTo(v: Variable, field: 'name' | 'value') {
    const at = opts.vars().indexOf(v)
    const next = opts.vars()[at + 1]
    if (field === 'value' && next) void start(next, 'name')
    else if (field === 'value') cancel()
    else void start(v, 'value')
  }

  function onCellKeydown(e: KeyboardEvent, v: Variable, field: 'name' | 'value', scope: string | null) {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (commit() && field === 'name') void start(v, 'value', scope)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancel()
    } else if (e.key === 'Tab' && scope !== null) {
      e.preventDefault()
      if (commit()) moveTo(v, field)
    }
  }

  // Escape belongs to the cell being written before the window.
  function cancelTop(): boolean {
    if (editing.value) {
      cancel()
      return true
    }
    return false
  }

  return {
    draft,
    setCellInput,
    isEditingCell,
    start,
    commitFrom,
    onCellKeydown,
    cancelTop,
  }
}
