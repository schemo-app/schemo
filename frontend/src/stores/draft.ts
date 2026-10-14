import { computed, reactive, ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import {
  BodyKind,
  RowKind,
  type Auth,
  type DraftID,
  type Record as RequestRecord,
  type Capture,
  type Scripts,
} from '../../bindings/schemo/internal/domain'
import { type Format, type Reading } from '../../bindings/schemo/internal/command'
import { freshInspector, type InspectorState } from '../lib/inspector'
import {
  TextField,
  type Copy,
  type Preview,
  type RowPatch,
  type Seed,
  type State,
  type TextResult,
} from '../../bindings/schemo/internal/usecase/draft'
import type { Level } from '../../bindings/schemo/internal/usecase/scripting'
import {
  CommandService,
  DraftService,
  RecordsService,
  ScriptingService,
} from '../../bindings/schemo/internal/transport/wails'
import { t } from '../i18n'
import { FLUSH_MS, discard, drain, settle, typeInto, type DraftBuffer } from '../lib/draftBuffer'
import { takeAnswer } from '../composables/request/earlyAnswers'
import { markStep } from '../composables/useGettingStarted'
import { NO_AUTH, type ChipName } from '../lib/requestSource'
import { callOrToast } from './calls'

export interface DraftTarget {
  id: () => DraftID | null
  state: () => State | null
  keep: (state: State) => void
  touched?: () => void
  filled?: () => void
  sent?: () => void
  finished: (record: RequestRecord) => Promise<void>
  scriptsLevel: () => string | null
}

export function useDraft(target: DraftTarget) {
  const buffer = reactive<DraftBuffer>({
    urlText: '',
    bodyText: '',
    urlRev: 0,
    bodyRev: 0,
    bufferedUrl: false,
    bufferedBody: false,
  })

  // A flush that fires after the buffer was drained or dropped finds nothing drain and writes nothing.
  const flushLater = useDebounceFn(() => void flush(), FLUSH_MS)

  const draft = computed(() => target.state()?.draft ?? null)

  const url = computed(() => buffer.urlText)
  const body = computed(() => buffer.bodyText)
  const method = computed(() => draft.value?.method ?? 'GET')
  const params = computed(() => draft.value?.params ?? [])
  const headers = computed(() => draft.value?.headers ?? [])
  const pathParams = computed(() => draft.value?.pathParams ?? [])
  const auth = computed<Auth>(() => draft.value?.auth ?? NO_AUTH)
  const environmentId = computed(() => draft.value?.environmentId ?? '')
  const bodyKind = computed(() => draft.value?.bodyKind ?? BodyKind.BodyRaw)
  const form = computed(() => draft.value?.form ?? [])
  const bodyFile = computed(() => draft.value?.bodyFile ?? '')
  const projected = computed(() => target.state()?.projected ?? [])
  const auto = computed(() => target.state()?.auto ?? [])
  const token = computed(() => target.state()?.token ?? null)
  const missing = computed(() => target.state()?.preview?.missing ?? [])
  const enabledParamsCount = computed(() => params.value.filter((p) => p.enabled && p.name.trim()).length)
  const enabledHeadersCount = computed(
    () => headers.value.filter((h) => h.enabled && h.name.trim()).length
  )
  const hasBody = computed(() => {
    if (!draft.value) return false
    switch (draft.value.bodyKind) {
      case BodyKind.BodyForm:
        return form.value.some((row) => row.enabled && row.name.trim())
      case BodyKind.BodyBinary:
        return bodyFile.value !== ''
      default:
        return buffer.bodyText.trim().length > 0
    }
  })

  const typing = computed(() => buffer.bufferedUrl || buffer.bufferedBody)

  // Whether anything has changed is asked of the draft itself: what stands now against what stood
  // when it was opened or last written home — its base. The buffers count as the text they hold, so
  // a keystroke that is still on its way shows at once, and one that took the text back where it
  // was takes the mark back with it.
  const shape = computed(() => {
    const d = draft.value
    if (!d) return ''
    const fields = Object.entries(d.auth?.fields ?? {})
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .flat()
      .join('\u0000')
    return JSON.stringify({
      method: d.method,
      url: buffer.urlText,
      body: buffer.bodyText,
      bodyKind: d.bodyKind,
      form: d.form,
      bodyFile: d.bodyFile,
      params: d.params,
      headers: d.headers,
      pathParams: d.pathParams,
      auth: [d.auth?.type, fields],
      environmentId: d.environmentId,
    })
  })

  const base = ref('')
  const changed = computed(() => base.value !== '' && shape.value !== base.value)

  /** Takes where the draft stands as the base nothing has changed from: an open, a write home. */
  function remember() {
    base.value = shape.value
  }

  function receive(state: State) {
    target.keep(state)
    if (!buffer.bufferedUrl) buffer.urlText = state.draft.url
    if (!buffer.bufferedBody) buffer.bodyText = state.draft.body
  }

  function load(state: State) {
    abandon()
    target.keep(state)
    buffer.urlText = state.draft.url
    buffer.bodyText = state.draft.body
    remember()
  }

  function abandon() {
    discard(buffer)
    buffer.urlRev += 1
    buffer.bodyRev += 1
  }

  function applyText(result: TextResult) {
    if (settle(buffer, result)) receive(result)
  }

  async function change(call: (id: DraftID) => Promise<State>): Promise<boolean> {
    const id = target.id()
    if (!id) return false
    const state = await callOrToast(call(id), 'request.editFailed')
    if (!state) return false
    receive(state)
    target.touched?.()
    return true
  }

  function type(field: TextField, text: string) {
    typeInto(buffer, field, text)
    void flushLater()
    target.touched?.()
  }

  const setUrl = (text: string) => type(TextField.FieldURL, text)
  const setBody = (text: string) => type(TextField.FieldBody, text)

  async function flush(): Promise<boolean> {
    const parts = drain(buffer)
    const id = target.id()
    if (!id) return parts.length === 0
    for (const part of parts) {
      const result = await callOrToast(DraftService.SetText(id, part), 'request.editFailed')
      if (!result) return false
      applyText(result)
    }
    return !buffer.bufferedUrl && !buffer.bufferedBody
  }

  async function copyAs(format: Format): Promise<Copy | null> {
    const id = target.id()
    if (!id) return null
    await flush()
    return (await callOrToast(CommandService.ExportDraft(id, format), 'response.copyFailed')) ?? null
  }

  async function refreshPreview() {
    const id = target.id()
    if (!id) return
    let fresh: Preview
    try {
      fresh = await DraftService.Preview(id)
    } catch {
      return
    }
    const current = target.state()
    // The draft may have been closed or replaced while this was out.
    if (target.id() === id && current) target.keep({ ...current, preview: fresh })
  }

  const edit = (call: (id: DraftID) => Promise<State>) => change(call).then(() => undefined)

  const setMethod = (value: string) => edit((id) => DraftService.SetMethod(id, value))
  const setAuth = (value: Auth) => edit((id) => DraftService.SetAuth(id, value))
  const setEnvironmentOverride = (env: string) =>
    edit((id) => DraftService.SetEnvironmentOverride(id, env))
  const obtainAuth = () => edit((id) => DraftService.ObtainAuth(id))
  const forgetAuth = () => edit((id) => DraftService.ForgetAuth(id))
  const setBodyKind = (kind: BodyKind) => edit((id) => DraftService.SetBodyKind(id, kind))
  const setBodyFile = (path: string) => edit((id) => DraftService.SetBodyFile(id, path))
  const removeRow = (kind: RowKind, row: string) => edit((id) => DraftService.RemoveRow(id, kind, row))
  const patchRow = (kind: RowKind, row: string, patch: RowPatch) =>
    edit((id) => DraftService.PatchRow(id, kind, row, patch))
  const toggleRow = (kind: RowKind, row: string, enabled: boolean) => patchRow(kind, row, { enabled })
  const moveRow = (kind: RowKind, row: string, position: number) =>
    edit((id) => DraftService.MoveRow(id, kind, row, position))

  async function pickBodyFile(): Promise<string> {
    const call = DraftService.PickBodyFile(t('files.bodyFile'), t('files.allFiles'))
    return (await callOrToast(call, 'files.pickFailed')) ?? ''
  }

  async function addRow(kind: RowKind): Promise<string> {
    const id = target.id()
    if (!id) return ''
    const added = await callOrToast(DraftService.AddRow(id, kind), 'request.editFailed')
    if (!added) return ''
    receive(added)
    target.touched?.()
    return added.id
  }

  async function replace(seed: Seed) {
    if (!target.id()) return
    abandon()
    if (await change((id) => DraftService.Replace(id, seed))) target.filled?.()
  }

  async function pasteCommand(text: string): Promise<Reading | null> {
    const id = target.id()
    if (!id) return null
    abandon()
    const pasted = await callOrToast(DraftService.PasteCommand(id, text), 'request.editFailed')
    if (!pasted) return null
    if (pasted.state) {
      receive(pasted.state)
      target.touched?.()
      target.filled?.()
    }
    return pasted.reading
  }

  const loading = ref(false)
  const pendingId = ref<string | null>(null)
  const mine = ref<string[]>([])

  async function send() {
    await flush()
    const id = target.id()
    if (!id) return
    loading.value = true
    let sent: string
    try {
      sent = await RecordsService.Send(id)
    } catch (error) {
      failSend()
      throw error
    }
    target.sent?.()
    claim(sent)
    markStep('send')
    if (loading.value) pendingId.value = sent
  }

  function claim(id: string) {
    mine.value = [id, ...mine.value].slice(0, 20)
    const early = takeAnswer(id)
    if (!early) return
    if ('failed' in early) failSend()
    else void finishSend(early.record)
  }

  async function cancel() {
    const id = pendingId.value
    failSend()
    if (id) await callOrToast(RecordsService.Cancel(id), 'request.sendFailed')
  }

  function failSend() {
    pendingId.value = null
    loading.value = false
  }

  async function finishSend(record: RequestRecord) {
    failSend()
    await target.finished(record)
  }

  const scriptsFor = ref<string | null>(null)
  const scripts = ref<Scripts | null>(null)
  const chain = ref<Level[]>([])

  async function readChain(id: string, own: Scripts | null) {
    const levels = await callOrToast(ScriptingService.Chain(id), 'request.readFailed')
    if (levels === undefined || target.scriptsLevel() !== id) return
    scriptsFor.value = id
    scripts.value = own
    chain.value = levels ?? []
  }

  async function loadScripts() {
    const id = target.scriptsLevel()
    if (!id) return
    const own = await callOrToast(ScriptingService.Scripts(id), 'request.readFailed')
    if (own !== undefined) await readChain(id, own)
  }

  async function saveScripts(pre: string, post: string, off: { pre: boolean; post: boolean }) {
    const id = target.scriptsLevel()
    if (!id) return
    const written = pre.trim() || post.trim()
      ? { pre, post, preOff: off.pre && !!pre.trim(), postOff: off.post && !!post.trim() }
      : null
    const saved = await callOrToast(ScriptingService.SaveScripts(id, written), 'request.editFailed')
    if (saved !== undefined) await readChain(id, saved)
  }

  const captures = computed<Capture[]>(() => scripts.value?.captures ?? [])

  async function saveCaptures(list: Capture[]) {
    const id = target.scriptsLevel()
    if (!id) return
    const saved = await callOrToast(ScriptingService.SaveCaptures(id, list), 'request.editFailed')
    if (saved !== undefined) await readChain(id, saved)
  }

  function inheritedScript(scope: 'pre' | 'post'): { text: string; name: string } | null {
    const own = target.scriptsLevel()
    for (let i = chain.value.length - 1; i >= 0; i -= 1) {
      const level = chain.value[i]
      if (level.nodeId === own) continue
      if (scope === 'pre' ? level.scripts?.preOff : level.scripts?.postOff) continue
      const text = scope === 'pre' ? level.scripts?.pre : level.scripts?.post
      if (text?.trim()) return { text, name: level.name }
    }
    return null
  }

  const openChip = ref<ChipName | null>('params')
  const inspector = ref<InspectorState>(freshInspector())

  function setOpenChip(chip: ChipName | null) {
    openChip.value = chip
  }

  function setInspector(partial: Partial<{ open: boolean; width: number }>) {
    inspector.value = { ...inspector.value, ...partial }
  }

  return {
    typing,
    changed,
    remember,
    url,
    body,
    method,
    params,
    headers,
    pathParams,
    auth,
    environmentId,
    bodyKind,
    form,
    bodyFile,
    projected,
    auto,
    token,
    missing,
    enabledParamsCount,
    enabledHeadersCount,
    hasBody,
    receive,
    load,
    abandon,
    applyText,
    setUrl,
    setBody,
    flush,
    copyAs,
    refreshPreview,
    setMethod,
    setAuth,
    setEnvironmentOverride,
    obtainAuth,
    forgetAuth,
    setBodyKind,
    setBodyFile,
    pickBodyFile,
    addRow,
    removeRow,
    patchRow,
    toggleRow,
    moveRow,
    replace,
    pasteCommand,
    loading,
    pendingId,
    mine,
    send,
    claim,
    cancel,
    failSend,
    finishSend,
    scriptsFor,
    scripts,
    chain,
    captures,
    loadScripts,
    saveScripts,
    saveCaptures,
    inheritedScript,
    openChip,
    inspector,
    setOpenChip,
    setInspector,
  }
}
