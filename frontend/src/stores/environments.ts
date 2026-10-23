import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { CancelError, type CancellablePromise } from '@wailsio/runtime/cancellable'
import type { VarResolution } from '../lib/vars'
import { Events } from '@wailsio/runtime'
import { Environments, type EnvironmentInput } from '../../bindings/schemo/internal/workspace'
import { VarScope, VariableKind } from '../../bindings/schemo/internal/domain'
import type { EnvScope, EnvState, Environment, Variable } from '../../bindings/schemo/internal/domain'
import type { Entry } from '../../bindings/schemo/internal/convert/dotenv'
import { markStep } from '../composables/useGettingStarted'
import { callOrToast } from '../lib/calls'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'

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
    (envState.value?.environments ?? []).map((environment) => ({ ...environment, vars: environment.vars ?? [] }))
  )
  const globals = computed(() => envState.value?.globals ?? [])
  const activeId = computed(() => envState.value?.activeId || null)
  const activeEnvironment = computed(
    () => environments.value.find((environment) => environment.id === activeId.value) ?? null
  )

  // The mirror is replaced by Go's answer, never patched.
  async function take<T extends EnvState>(call: CancellablePromise<T>): Promise<T> {
    const answer = await snapshot.take(call)
    forgetRevealed()
    return answer
  }

  const reload = () => take(Environments.State())
  const load = () => callOrToast(reload(), 'environments.readFailed')

  // Writes answer with nothing: the state is read again once Go has it. While a write is in flight
  // the window shows its own edit, and changes announced meanwhile wait for the read after it.
  let writing = 0
  async function write(call: Promise<unknown>): Promise<EnvState> {
    writing++
    try {
      await call
    } finally {
      writing--
    }
    return await reload()
  }

  // A change made anywhere else (another window, an agent, sync, a script) reads the state again.
  let pending: ReturnType<typeof setTimeout> | null = null
  const offChanged = Events.On('changed', (event) => {
    const kind = event.data?.kind
    if ((kind !== 'environment' && kind !== 'variable') || envState.value === null || pending) return
    pending = setTimeout(() => {
      pending = null
      if (writing === 0 && envState.value !== null) void reload().catch(() => {})
    }, 50)
  })
  onScopeDispose(() => offChanged?.())

  /** Shows an edit at once and saves the scope it belongs to whole. */
  function saveScope(envId: string | null, vars: Variable[], env?: Env): Promise<EnvState> {
    const state = envState.value
    if (state) {
      envState.value = envId === null
        ? { ...state, globals: vars }
        : { ...state, environments: (state.environments ?? []).map((one) => one.id === envId ? { ...(env ?? one), vars } : one) }
    }
    if (envId === null) return write(Environments.SaveGlobals(vars))
    const current = env ?? environments.value.find((one) => one.id === envId)
    if (!current) return reload()
    return write(Environments.Save({ ...current, vars }))
  }

  function saveEnv(id: string, change: Partial<Env>): Promise<EnvState> {
    const current = environments.value.find((one) => one.id === id)
    if (!current) return reload()
    const next = { ...current, ...change }
    return saveScope(id, next.vars, next)
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

  // Switching to an environment is the step the checklist watches; standing on none of them is not.
  async function setActive(id: string | null) {
    const answer = await write(Environments.Activate(id ?? ''))
    if (id) markStep('env')
    return answer
  }

  async function createEnv(draft: EnvironmentInput): Promise<string> {
    const signal = snapshot.signal()
    const id = await Environments.Create(draft)
    await write(Promise.resolve())
    signal.throwIfAborted()
    editedEnvId.value = id
    sheetFocus.value = null
    return id
  }

  async function removeEnv(id: string) {
    await write(Environments.Delete(id))
  }

  // A revealed value outlives nothing: once its variable is gone, so is the value.
  function forgetRevealed() {
    const live = new Set([...environments.value.flatMap(environment => environment.vars), ...globals.value].map(variable => variable.id))
    for (const key of Object.keys(revealed.value)) {
      if (!live.has(key)) delete revealed.value[key]
    }
  }

  const renameEnv = (id: string, name: string) => saveEnv(id, { name })
  const setEnvColor = (id: string, color: string) => saveEnv(id, { color })
  const setEnvReadonly = (id: string, readonly: boolean) => saveEnv(id, { readonly })

  async function clearGlobals() {
    await saveScope(null, [])
  }

  async function addVar(envId: string | null, init: Partial<Variable> = {}): Promise<string> {
    const id = crypto.randomUUID()
    await saveScope(envId, [...varsOf(envId), {
      id,
      name: init.name ?? '',
      kind: init.kind ?? VariableKind.VariableText,
      value: init.value ?? '',
      enabled: true,
      position: 0,
      hasValue: false,
    }])
    return id
  }

  /** Writes a response value into a variable, adding it if new. */
  async function capture(where: 'environment' | 'globals', name: string, value: string, secret: boolean) {
    const scope = where === 'globals' ? VarScope.ScopeGlobals : VarScope.ScopeEnvironment
    await write(Environments.Capture(scope, name, value, secret))
  }

  // A secret's value is never in the mirror: sent without one, Go keeps what it holds (hasValue);
  // a typed value replaces it, and an empty typed value clears it.
  async function updateVar(envId: string | null, varId: string, patch: Partial<Variable>) {
    const secrets = secretLifetime.signal
    const current = varsOf(envId).find((variable) => variable.id === varId)
    if (!current) return
    const next: Variable = { ...current, ...patch }
    if (patch.value !== undefined) next.hasValue = false
    await saveScope(envId, varsOf(envId).map((variable) => variable.id === varId ? next : variable))
    // A secret the user has just typed stays visible to them.
    if (!secrets.aborted && patch.value !== undefined && (patch.kind ?? current.kind) === VariableKind.VariableSecret) {
      revealed.value[varId] = patch.value
    }
  }

  async function removeVar(envId: string | null, varId: string) {
    await saveScope(envId, varsOf(envId).filter((variable) => variable.id !== varId))
    delete revealed.value[varId]
  }

  async function importDotenv(envId: string | null, entries: Entry[]) {
    const kept = entries.filter((entry) => entry.name.trim() !== '')
    if (kept.length > 0) await write(Environments.ImportEntries(scopeOf(envId), kept))
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
    const value = await Environments.Reveal(varId).cancelOn(signal)
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
    const env = envId ? (environments.value.find((environment) => environment.id === envId) ?? null) : activeEnvironment.value
    const own = env?.vars.find((variable) => variable.name === name && variable.enabled)
    if (own) return { value: effectiveValue(own), source: 'env', kind: own.kind }
    const global = globals.value.find((variable) => variable.name === name && variable.enabled)
    if (global) return { value: effectiveValue(global), source: 'global', kind: global.kind }
    return null
  }

  function rowsFor(envId: string | null): {
    own: Variable[]
    inherited: (Variable & { overridden: boolean })[]
  } {
    const own = varsOf(envId)
    if (envId === null) return { own, inherited: [] }
    const ownNames = new Set(own.map((variable) => variable.name))
    return { own, inherited: globals.value.map((variable) => ({ ...variable, overridden: ownNames.has(variable.name) })) }
  }

  function varsOf(envId: string | null): Variable[] {
    if (envId === null) return globals.value
    return environments.value.find((environment) => environment.id === envId)?.vars ?? []
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
