import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { CancelError, type CancellablePromise } from '@wailsio/runtime'
import type { VarResolution } from '../lib/vars'
import { EnvironmentsService } from '../../bindings/schemo/internal/transport/wails'
import { VarScope, VariableKind } from '../../bindings/schemo/internal/domain'
import type { EnvScope, EnvState, Environment, Variable } from '../../bindings/schemo/internal/domain'
import type { Entry } from '../../bindings/schemo/internal/dotenv'
import type { Draft } from '../../bindings/schemo/internal/usecase/environment'
import { markStep } from '../composables/useGettingStarted'
import { callOrToast } from './calls'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'

function scopeOf(envId: string | null): EnvScope {
  return envId === null ? {} : { environment: envId }
}

// Normalize Go's nil slice to empty array
export type Env = Omit<Environment, 'vars'> & { vars: Variable[] }

export const useEnvironmentsStore = defineStore('environments', () => {
  let secretLifetime = new AbortController()
  const snapshot = useWorkspaceSnapshot<EnvState | null>(null)
  const envState = snapshot.state
  const sheetOpen = ref(false)
  const sheetFocus = ref<{ envId: string | null; varName: string } | null>(null)
  const editedEnvId = ref<string | null>(null)
  // Making an environment is the sheet's own screen, and the environment menu asks for it from
  // outside: the request rides here and the sheet answers it, the way the tree's does on its store.
  const pendingCreate = ref(false)
  // Secret values shown in the sheet, by variable id; dropped when the sheet closes.
  const revealed = ref<Record<string, string>>({})

  const environments = computed<Env[]>(() =>
    (envState.value?.environments ?? []).map((e) => ({ ...e, vars: e.vars ?? [] }))
  )
  const globals = computed(() => envState.value?.globals ?? [])
  const activeId = computed(() => envState.value?.activeId || null)
  const activeEnvironment = computed(
    () => environments.value.find((e) => e.id === activeId.value) ?? null
  )

  // Every call answers with the whole state: the mirror is replaced, never patched.
  async function take<T extends EnvState>(call: CancellablePromise<T>): Promise<T> {
    const answer = await snapshot.take(call)
    forgetRevealed()
    return answer
  }

  const load = () => callOrToast(take(EnvironmentsService.Snapshot()), 'environments.readFailed')

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
    await take(EnvironmentsService.EnsureDefaults())
  }

  // Switching to an environment is the step the checklist watches; standing on none of them is not.
  async function setActive(id: string | null) {
    const answer = await take(EnvironmentsService.ActivateEnvironment(id ?? ''))
    if (id) markStep('env')
    return answer
  }

  async function createEnv(draft: Draft): Promise<string> {
    const signal = snapshot.signal()
    const created = await take(EnvironmentsService.CreateEnvironment(draft))
    signal.throwIfAborted()
    editedEnvId.value = created.id
    sheetFocus.value = null
    return created.id
  }

  async function removeEnv(id: string) {
    await take(EnvironmentsService.DeleteEnvironment(id))
  }

  // A revealed value outlives nothing: once its variable is gone, so is the value.
  function forgetRevealed() {
    const live = new Set([...environments.value.flatMap(env => env.vars), ...globals.value].map(v => v.id))
    for (const key of Object.keys(revealed.value)) {
      if (!live.has(key)) delete revealed.value[key]
    }
  }

  const renameEnv = (id: string, name: string) => take(EnvironmentsService.UpdateEnvironment(id, { name }))
  const setEnvColor = (id: string, color: string) =>
    take(EnvironmentsService.UpdateEnvironment(id, { color }))
  const setEnvReadonly = (id: string, readonly: boolean) =>
    take(EnvironmentsService.UpdateEnvironment(id, { readonly }))

  async function clearGlobals() {
    await take(EnvironmentsService.ClearGlobals())
  }

  async function addVar(envId: string | null, init: Partial<Variable> = {}): Promise<string> {
    await take(
      EnvironmentsService.AddVariable(scopeOf(envId), {
        name: init.name ?? '',
        kind: init.kind ?? VariableKind.VariableText,
        value: init.value ?? '',
      })
    )
    return varsOf(envId).at(-1)?.id ?? ''
  }

  /** Writes a response value into a variable, adding it if new. */
  async function capture(where: 'environment' | 'globals', name: string, value: string, secret: boolean) {
    const scope = where === 'globals' ? VarScope.ScopeGlobals : VarScope.ScopeEnvironment
    await take(EnvironmentsService.Capture(scope, name, value, secret))
  }

  async function updateVar(envId: string | null, varId: string, patch: Partial<Variable>) {
    const secrets = secretLifetime.signal
    const current = varsOf(envId).find((v) => v.id === varId)
    if (!current) return
    await take(
      EnvironmentsService.UpdateVariable(scopeOf(envId), {
        id: varId,
        name: patch.name ?? current.name,
        kind: patch.kind ?? current.kind,
        enabled: patch.enabled ?? current.enabled,
        setValue: patch.value !== undefined,
        value: patch.value ?? '',
      })
    )
    // A secret the user has just typed stays visible to them.
    if (!secrets.aborted && patch.value !== undefined && (patch.kind ?? current.kind) === VariableKind.VariableSecret) {
      revealed.value[varId] = patch.value
    }
  }

  async function removeVar(envId: string | null, varId: string) {
    await take(EnvironmentsService.RemoveVariable(scopeOf(envId), varId))
    delete revealed.value[varId]
  }

  async function importDotenv(envId: string | null, entries: Entry[]) {
    const kept = entries.filter((e) => e.name.trim() !== '')
    if (kept.length > 0) await take(EnvironmentsService.ImportEntries(scopeOf(envId), kept))
  }

  async function reveal(varId: string): Promise<string> {
    const signal = secretLifetime.signal
    const value = await readSecret(varId)
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

  /** A secret's value for a cell editor: asked for without being remembered — opening a cell on it
   *  must not turn the eye on. */
  async function valueForEdit(varId: string): Promise<string> {
    const signal = secretLifetime.signal
    const value = revealed.value[varId] ?? (await readSecret(varId))
    checkSecret(varId, signal)
    return value
  }

  function checkSecret(varId: string, signal: AbortSignal) {
    signal.throwIfAborted()
    if (!globals.value.some(variable => variable.id === varId)
      && !environments.value.some(env => env.vars.some(variable => variable.id === varId))) {
      throw new CancelError()
    }
  }

  async function readSecret(varId: string): Promise<string> {
    const signal = AbortSignal.any([snapshot.signal(), secretLifetime.signal])
    const value = await EnvironmentsService.Reveal(varId).cancelOn(signal)
    signal.throwIfAborted()
    return value
  }

  function forgetSecrets() {
    secretLifetime.abort(new CancelError())
    secretLifetime = new AbortController()
    revealed.value = {}
  }

  // A name resolves in the named (or active) environment first, then in the globals.
  function resolveVariable(name: string, envId?: string): VarResolution | null {
    const env = envId ? (environments.value.find((e) => e.id === envId) ?? null) : activeEnvironment.value
    const own = env?.vars.find((x) => x.name === name && x.enabled)
    if (own) return { value: effectiveValue(own), source: 'env', kind: own.kind }
    const global = globals.value.find((x) => x.name === name && x.enabled)
    if (global) return { value: effectiveValue(global), source: 'global', kind: global.kind }
    return null
  }

  function rowsFor(envId: string | null): {
    own: Variable[]
    inherited: (Variable & { overridden: boolean })[]
  } {
    const own = varsOf(envId)
    if (envId === null) return { own, inherited: [] }
    const ownNames = new Set(own.map((v) => v.name))
    return { own, inherited: globals.value.map((g) => ({ ...g, overridden: ownNames.has(g.name) })) }
  }

  function varsOf(envId: string | null): Variable[] {
    if (envId === null) return globals.value
    return environments.value.find((e) => e.id === envId)?.vars ?? []
  }

  function editEnv(id: string | null) {
    editedEnvId.value = id
  }

  function openSheet(focus: { envId: string | null; varName: string } | null = null) {
    sheetFocus.value = focus
    editedEnvId.value = focus ? focus.envId : activeId.value
    pendingCreate.value = false
    sheetOpen.value = true
  }

  /** Opens the sheet on the screen that makes an environment. */
  function openSheetCreating() {
    openSheet()
    pendingCreate.value = true
  }

  function closeSheet() {
    sheetOpen.value = false
    sheetFocus.value = null
    forgetSecrets()
  }

  // The focus is taken once: the sheet opens on it and then forgets it.
  function takeFocus(): { envId: string | null; varName: string } | null {
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
    pendingCreate,
    revealed,
    environments,
    globals,
    activeId,
    activeEnvironment,
    load,
    reset,
    ensureDefaults,
    setActive,
    createEnv,
    removeEnv,
    renameEnv,
    setEnvColor,
    setEnvReadonly,
    clearGlobals,
    addVar,
    capture,
    updateVar,
    removeVar,
    importDotenv,
    reveal,
    hide,
    isRevealed,
    effectiveValue,
    valueForEdit,
    resolveVariable,
    rowsFor,
    varsOf,
    editEnv,
    openSheet,
    openSheetCreating,
    closeSheet,
    takeFocus,
  }
})
