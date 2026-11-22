import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { CancelError, type CancellablePromise } from '@wailsio/runtime/cancellable'
import { NO_VAR_CONTEXT, type VarContext, type VarResolution, type VarSource } from '../lib/vars'
import { Environments, type EnvironmentPatch, type VariablePatch } from '../../bindings/schemo/internal/workspace'
import { Transfer } from '../../bindings/schemo/internal/convert'
import { VarScope, VariableKind } from '../../bindings/schemo/internal/domain'
import type { EnvScope, EnvState, Environment, Variable } from '../../bindings/schemo/internal/domain'
import { markStep } from '../composables/useGettingStarted'
import { callOrToast } from '../lib/calls'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'
import { useChangedEvent } from '../composables/useWailsEvent'
import { useToast } from '../composables/useToast'
import { t } from '../i18n'

/** An environment with Go's nil lists made empty. */
export type Env = Omit<Environment, 'vars' | 'collections'> & {
  vars: Variable[]
  collections: Record<string, Variable[]>
}

/** Where the sheet opens: an environment (null for the globals) and a name to show. */
export type SheetFocus = { envId: string | null; varName: string }

function scopeOf(envId: string | null, collection = ''): EnvScope {
  return envId === null ? {} : { environment: envId, collection }
}

/** The first name the pattern makes that no environment holds yet, ignoring case. */
function freeName(taken: string[], make: (n: number) => string, from = 1): string {
  const names = new Set(taken.map((name) => name.trim().toLowerCase()))
  for (let n = from; ; n++) {
    const name = make(n)
    if (!names.has(name.toLowerCase())) return name
  }
}

export const useEnvironmentsStore = defineStore('environments', () => {
  const toast = useToast()
  let secretLifetime = new AbortController()
  const snapshot = useWorkspaceSnapshot<EnvState | null>(null)
  const envState = snapshot.state
  const sheetOpen = ref(false)
  const sheetFocus = ref<SheetFocus | null>(null)
  const editedEnvId = ref<string | null>(null)
  // An environment just made opens with its name being edited.
  const renaming = ref<string | null>(null)
  // Secret values shown on screen, by variable id; dropped when the sheet closes.
  const revealed = ref<Record<string, string>>({})

  const environments = computed<Env[]>(() =>
    (envState.value?.environments ?? []).map((env) => ({
      ...env,
      vars: env.vars ?? [],
      collections: Object.fromEntries(Object.entries(env.collections ?? {}).map(([id, vars]) => [id, vars ?? []])),
    }))
  )
  const globals = computed(() => envState.value?.globals ?? [])
  const activeId = computed(() => envState.value?.activeId || null)
  const activeEnvironment = computed(() => environments.value.find((env) => env.id === activeId.value) ?? null)

  const envOf = (id: string | null) => environments.value.find((env) => env.id === id) ?? null

  // The mirror is replaced by Go's answer, never patched.
  async function take<T extends EnvState>(call: CancellablePromise<T>): Promise<T> {
    const answer = await snapshot.take(call)
    forgetRevealed()
    return answer
  }

  const reload = () => take(Environments.State())
  const load = () => callOrToast(reload(), 'environments.readFailed')

  async function write<T>(call: CancellablePromise<T>): Promise<T> {
    const signal = snapshot.signal()
    const result = await call.cancelOn(signal)
    signal.throwIfAborted()
    await reload()
    return result
  }

  useChangedEvent(['environment', 'variable'], () => {
    if (envState.value !== null) void reload().catch(() => {})
  })

  function saveScope(envId: string | null, vars: Variable[], collection = '') {
    return write(Environments.SaveVariables(scopeOf(envId, collection), vars))
  }

  function updateEnv(id: string, patch: EnvironmentPatch) {
    return write(Environments.Update(id, patch))
  }

  function moveVariable(collection: string, variable: string, from: string, into: string) {
    return write(Environments.MoveVariable(collection, variable, from, into))
  }

  async function applyState(state: EnvState) {
    await snapshot.replace(state)
    forgetRevealed()
  }

  function reset() {
    snapshot.reset()
    forgetSecrets()
    editedEnvId.value = null
    sheetFocus.value = null
  }

  onScopeDispose(reset)

  async function ensureDefaults() {
    if (environments.value.length > 0 || globals.value.length > 0) return
    await write(Environments.EnsureDefaults())
  }

  async function setActive(id: string | null) {
    await write(Environments.Activate(id ?? ''))
    if (id) markStep('env')
  }

  /** Makes "Environment N" and opens it with its name being edited. */
  async function createEnv(): Promise<string> {
    const signal = snapshot.signal()
    const taken = environments.value.map((env) => env.name)
    const name = freeName(taken, (n) => t('environments.untitled', { n }), environments.value.length + 1)
    const id = await write(Environments.Create(name, ''))
    signal.throwIfAborted()
    editedEnvId.value = id
    renaming.value = id
    return id
  }

  async function duplicateEnv(id: string): Promise<string> {
    const env = envOf(id)
    if (!env) return ''
    const copied = await write(Environments.Duplicate(id, t('environments.copyName', { name: env.name })))
    editedEnvId.value = copied
    return copied
  }

  async function mergeEnv(from: string, into: string) {
    await write(Environments.Merge(from, into))
    if (editedEnvId.value === from) editedEnvId.value = into
  }

  function reorder(ids: string[]) {
    return write(Environments.Reorder(ids))
  }

  async function removeEnv(id: string) {
    await write(Environments.Delete(id))
  }

  function allVars(): Variable[] {
    return [
      ...globals.value,
      ...environments.value.flatMap((env) => [...env.vars, ...Object.values(env.collections).flat()]),
    ]
  }

  // A revealed value outlives nothing: once its variable is gone, so is the value.
  function forgetRevealed() {
    const live = new Set(allVars().map((variable) => variable.id))
    for (const key of Object.keys(revealed.value)) {
      if (!live.has(key)) delete revealed.value[key]
    }
  }

  function blankVar(name = ''): Variable {
    return {
      id: crypto.randomUUID(),
      name,
      kind: VariableKind.VariableText,
      value: '',
      enabled: true,
      position: 0,
      hasValue: false,
    }
  }

  async function addVar(envId: string | null, name: string, value = ''): Promise<string> {
    const added = { ...blankVar(name), value }
    await saveScope(envId, [...varsOf(envId), added])
    return added.id
  }

  /** Writes a response value into a variable, adding it if new. */
  async function capture(where: 'environment' | 'globals', name: string, value: string, secret: boolean) {
    const scope = where === 'globals' ? VarScope.ScopeGlobals : VarScope.ScopeEnvironment
    await write(Environments.Capture(scope, name, value, secret))
  }

  async function updateVar(envId: string | null, id: string, patch: VariablePatch, collection = '') {
    const signal = secretLifetime.signal
    await write(Environments.UpdateVariable(scopeOf(envId, collection), id, patch))
    const variable = varsOf(envId, collection).find(variable => variable.id === id)
    if (!signal.aborted && typeof patch.value === 'string' && variable?.kind === VariableKind.VariableSecret) {
      revealed.value[id] = patch.value
    }
  }

  /** Removes a variable without asking; the toast offers it back unless this is itself an undo. */
  async function removeVar(envId: string | null, varId: string, undoable = true) {
    const before = varsOf(envId)
    const at = before.findIndex((variable) => variable.id === varId)
    if (at < 0) return
    let gone = before[at]
    // The window holds no secret's value, so undo needs it read before the delete.
    if (undoable && gone.kind === VariableKind.VariableSecret) gone = { ...gone, value: await valueForEdit(varId) }
    await saveScope(envId, before.filter((variable) => variable.id !== varId))
    delete revealed.value[varId]
    if (!undoable) return
    toast.show(t('environments.removed', { name: gone.name || t('environments.unnamed') }), 'info', {
      label: t('environments.undo'),
      run: () => {
        const now = varsOf(envId)
        void saveScope(envId, [...now.slice(0, at), gone, ...now.slice(at)]).catch(() => {})
      },
    })
  }

  /** Imports a picked file into an environment or the globals: only missing names are added. */
  async function importFile(envId: string | null) {
    const report = await Transfer.ImportVariablesFile(t('environments.importTitle'), scopeOf(envId))
    if (report.cancelled) return
    await reload()
    toast.show(t('environments.importedFile', { file: report.file, added: report.added, kept: report.kept }))
  }

  async function exportFile(envId: string | null) {
    const file = await Transfer.ExportVariablesFile(t('environments.exportTitle'), scopeOf(envId))
    if (file) toast.show(t('environments.exported', { file }))
  }

  async function reveal(varId: string, lifetime = secretLifetime.signal): Promise<string> {
    const signal = AbortSignal.any([secretLifetime.signal, lifetime])
    const value = await readSecret(varId, signal)
    checkSecret(varId, signal)
    revealed.value[varId] = value
    return value
  }

  function hide(varId: string) {
    delete revealed.value[varId]
  }

  const isRevealed = (varId: string) => revealed.value[varId] !== undefined

  function effectiveValue(v: Variable): string {
    if (v.kind !== VariableKind.VariableSecret) return v.value ?? ''
    return revealed.value[v.id] ?? ''
  }

  /** A secret's value for a cell editor, read without turning the eye on. */
  async function valueForEdit(varId: string): Promise<string> {
    const signal = secretLifetime.signal
    const value = revealed.value[varId] ?? (await readSecret(varId))
    checkSecret(varId, signal)
    return value
  }

  function checkSecret(varId: string, signal: AbortSignal) {
    signal.throwIfAborted()
    if (!allVars().some((variable) => variable.id === varId)) throw new CancelError()
  }

  async function readSecret(varId: string, lifetime = secretLifetime.signal): Promise<string> {
    const signal = AbortSignal.any([snapshot.signal(), secretLifetime.signal, lifetime])
    const value = await Environments.Reveal(varId).cancelOn(signal)
    signal.throwIfAborted()
    return value
  }

  function forgetSecrets() {
    secretLifetime.abort(new CancelError())
    secretLifetime = new AbortController()
    revealed.value = {}
  }

  /** Resolves a name as Go does: globals, collection, active environment, its block; last wins. */
  function resolveVariable(name: string, context: VarContext = NO_VAR_CONTEXT): VarResolution | null {
    const env = activeEnvironment.value
    const layers: [Variable[], VarSource][] = [
      [globals.value, 'global'],
      [context.variables, 'collection'],
      [env?.vars ?? [], 'env'],
      [env?.collections[context.collection] ?? [], 'block'],
    ]
    let found: VarResolution | null = null
    for (const [vars, source] of layers) {
      const hit = vars.findLast((variable) => variable.name === name && variable.enabled)
      if (hit) found = { value: effectiveValue(hit), source, kind: hit.kind }
    }
    return found
  }

  function varsOf(envId: string | null, collection = ''): Variable[] {
    if (envId === null) return globals.value
    const env = envOf(envId)
    return collection ? env?.collections[collection] ?? [] : env?.vars ?? []
  }

  function editEnv(id: string | null) {
    editedEnvId.value = id
  }

  function openSheet(focus: SheetFocus | null = null) {
    sheetFocus.value = focus
    editedEnvId.value = focus ? focus.envId : activeId.value
    sheetOpen.value = true
  }

  function closeSheet() {
    sheetOpen.value = false
    sheetFocus.value = null
    renaming.value = null
    forgetSecrets()
  }

  // The focus is taken once: the sheet opens on it and then forgets it.
  function takeFocus(): SheetFocus | null {
    const focus = sheetFocus.value
    sheetFocus.value = null
    return focus
  }

  return {
    applyState,
    envState,
    sheetOpen,
    sheetFocus,
    editedEnvId,
    renaming,
    revealed,
    environments,
    globals,
    activeId,
    activeEnvironment,
    envOf,
    load,
    reset,
    ensureDefaults,
    setActive,
    createEnv,
    duplicateEnv,
    mergeEnv,
    reorder,
    removeEnv,
    updateEnv,
    saveScope,
    moveVariable,
    blankVar,
    addVar,
    capture,
    updateVar,
    removeVar,
    importFile,
    exportFile,
    reveal,
    hide,
    isRevealed,
    effectiveValue,
    valueForEdit,
    resolveVariable,
    varsOf,
    editEnv,
    openSheet,
    closeSheet,
    takeFocus,
  }
})
