import { computed, ref, shallowRef } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import {
  BodyKind,
  RowKind,
  type Auth,
  type Draft,
  type FormRow,
  type Record as RequestRecord,
  type Capture,
  type Row,
  type Scripts,
} from '../../bindings/schemo/internal/domain'
import { type Format, type Reading } from '../../bindings/schemo/internal/convert/command'
import {
  Drafts,
  type Copy,
  type Preview,
} from '../../bindings/schemo/internal/request'
import { Scripts as ScriptsService, type Level } from '../../bindings/schemo/internal/script'
import { History } from '../../bindings/schemo/internal/request'
import { t } from '../i18n'
import { freshInspector, type InspectorState } from '../lib/inspector'
import { followUrl, mintRowId, writeParams } from '../lib/draftRows'
import { FLUSH_MS } from './useDeferredWrite'
import { takeAnswer } from '../lib/earlyAnswers'
import { markStep } from './useGettingStarted'
import { NO_AUTH, type ChipName } from '../lib/requestSource'
import { callOrToast } from '../lib/calls'

/** What the editor is told about the tab it edits for. */
export interface DraftTarget {
  /** Whether the draft is a tab's, written down a pause after each edit. */
  persisted: () => boolean
  touched?: () => void
  filled?: () => void
  sent?: () => void
  finished: (record: RequestRecord) => Promise<void>
  scriptsLevel: () => string | null
}

/** Fields a row patch may change; a path row's name is its address's. */
export interface RowPatch {
  name?: string
  value?: string
  enabled?: boolean
  description?: string
  src?: string
  file?: boolean
}

const PREVIEW_MS = 150

/**
 * The request being composed. The window owns the draft: every edit is applied here, at once, and
 * the whole draft goes to Go to be written down (a tab's, after a pause), previewed (what the
 * levels above, the authorization and the app add) or sent.
 */
export function useDraft(target: DraftTarget) {
  const current = shallowRef<Draft | null>(null)
  const preview = shallowRef<Preview | null>(null)

  const draft = computed(() => current.value)
  const url = computed(() => current.value?.url ?? '')
  const body = computed(() => current.value?.body ?? '')
  const method = computed(() => current.value?.method ?? 'GET')
  const params = computed(() => current.value?.params ?? [])
  const headers = computed(() => current.value?.headers ?? [])
  const pathParams = computed(() => current.value?.pathParams ?? [])
  const auth = computed<Auth>(() => current.value?.auth ?? NO_AUTH)
  const environmentId = computed(() => current.value?.environmentId ?? '')
  const bodyKind = computed(() => current.value?.bodyKind ?? BodyKind.BodyRaw)
  const form = computed(() => current.value?.form ?? [])
  const bodyFile = computed(() => current.value?.bodyFile ?? '')
  const projected = computed(() => preview.value?.projected ?? [])
  const auto = computed(() => preview.value?.auto ?? [])
  const token = computed(() => preview.value?.token ?? null)
  const inherited = computed(() => preview.value?.inherited ?? null)
  const missing = computed(() => preview.value?.missing ?? [])
  const enabledParamsCount = computed(() => params.value.filter((param) => param.enabled && param.name.trim()).length)
  const enabledHeadersCount = computed(
    () => headers.value.filter((header) => header.enabled && header.name.trim()).length
  )
  const hasBody = computed(() => {
    const d = current.value
    if (!d) return false
    switch (d.bodyKind) {
      case BodyKind.BodyForm:
        return form.value.some((row) => row.enabled && row.name.trim())
      case BodyKind.BodyBinary:
        return bodyFile.value !== ''
      default:
        return d.body.trim().length > 0
    }
  })

  // Whether anything changed is asked of the draft itself: what stands now against what stood when
  // it was opened or last written home. The two texts are compared as they are, since a body can
  // be megabytes and this is asked on every keystroke; the rest is compared serialised.
  interface Standing {
    structure: string
    url: string
    body: string
  }

  function standingOf(d: Draft | null): Standing | null {
    if (!d) return null
    const fields = Object.entries(d.auth?.fields ?? {})
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .flat()
      .join('\u0000')
    return {
      structure: JSON.stringify({
        method: d.method,
        bodyKind: d.bodyKind,
        form: d.form,
        bodyFile: d.bodyFile,
        params: d.params,
        headers: d.headers,
        pathParams: d.pathParams,
        auth: [d.auth?.type, fields],
        environmentId: d.environmentId,
      }),
      url: d.url,
      body: d.body,
    }
  }

  const standing = computed(() => standingOf(current.value))
  const base = shallowRef<Standing | null>(null)
  const changed = computed(() => {
    const was = base.value
    const now = standing.value
    if (!was || !now) return false
    return now.structure !== was.structure || now.url !== was.url || now.body !== was.body
  })

  /** Takes where the draft stands as the base nothing has changed from: an open, a write home. */
  function remember() {
    base.value = standing.value
  }

  // A tab's draft is written a pause after the last edit; `flush` writes it at once. The write is
  // chained so two saves never cross, and the draft written is the one standing when it leaves.
  let dirty = false
  let writing: Promise<boolean> = Promise.resolve(true)
  const saveLater = useDebounceFn(() => void flush(), FLUSH_MS)

  function flush(): Promise<boolean> {
    writing = writing.then(async () => {
      if (!dirty || !current.value || !target.persisted()) return true
      dirty = false
      const ok = await callOrToast(Drafts.Save(current.value).then(() => true), 'request.editFailed')
      return ok === true
    })
    return writing
  }

  // The preview is asked a moment after the edits stop; an answer for a draft that has since moved
  // on is dropped.
  let asked = 0
  const previewLater = useDebounceFn(() => void refreshPreview(), PREVIEW_MS)

  async function refreshPreview() {
    const d = current.value
    if (!d) return
    const ask = ++asked
    let fresh: Preview
    try {
      fresh = await Drafts.Preview(d)
    } catch {
      return
    }
    if (ask === asked && current.value?.id === d.id) preview.value = fresh
  }

  /** Opens a draft as the one being edited, with nothing unsaved in it. */
  function load(d: Draft) {
    dirty = false
    asked += 1
    current.value = followUrl(d)
    preview.value = null
    remember()
    void refreshPreview()
  }

  /** Forgets the draft: a closed tab writes nothing more and asks for nothing more. */
  function abandon() {
    dirty = false
    asked += 1
    current.value = null
  }

  function apply(next: Draft) {
    current.value = next
    dirty = true
    target.touched?.()
    if (target.persisted()) void saveLater()
    void previewLater()
  }

  function edit(change: (d: Draft) => Draft) {
    const d = current.value
    if (d) apply(change(d))
  }

  const setUrl = (text: string) => edit((d) => followUrl({ ...d, url: text }))
  const setBody = (text: string) => edit((d) => ({ ...d, body: text }))
  const setMethod = async (value: string) => edit((d) => ({ ...d, method: value.trim() }))
  const setAuth = async (value: Auth) => edit((d) => ({ ...d, auth: value }))
  const setEnvironmentOverride = async (env: string) => edit((d) => ({ ...d, environmentId: env.trim() }))
  const setBodyKind = async (kind: BodyKind) => edit((d) => ({ ...d, bodyKind: kind }))
  const setBodyFile = async (path: string) => edit((d) => ({ ...d, bodyFile: path.trim() }))

  type ListKey = 'params' | 'headers' | 'form' | 'pathParams'
  const listOf: Record<RowKind, ListKey> = {
    [RowKind.RowParams]: 'params',
    [RowKind.RowHeaders]: 'headers',
    [RowKind.RowForm]: 'form',
    [RowKind.RowPathParams]: 'pathParams',
    [RowKind.$zero]: 'params',
  }

  // A parameter list that changed writes the address again: the address is what goes out, and the
  // rows are only how it is edited.
  function withList(d: Draft, kind: RowKind, rows: (Row | FormRow)[]): Draft {
    const next = { ...d, [listOf[kind]]: rows } as Draft
    return kind === RowKind.RowParams ? writeParams(next) : next
  }

  async function addRow(kind: RowKind): Promise<string> {
    if (kind === RowKind.RowPathParams || !current.value) return ''
    const id = mintRowId()
    edit((d) => withList(d, kind, [...(d[listOf[kind]] ?? []), { id, name: '', value: '', enabled: true }]))
    return id
  }

  async function removeRow(kind: RowKind, row: string) {
    if (kind === RowKind.RowPathParams) return
    edit((d) => withList(d, kind, (d[listOf[kind]] ?? []).filter((one) => one.id !== row)))
  }

  async function patchRow(kind: RowKind, row: string, patch: RowPatch) {
    // A path row's name is its address's and it is always on: only its value and description move.
    const allowed: RowPatch =
      kind === RowKind.RowPathParams ? { value: patch.value, description: patch.description } : patch
    const defined = Object.fromEntries(Object.entries(allowed).filter(([, v]) => v !== undefined))
    edit((d) =>
      withList(
        d,
        kind,
        (d[listOf[kind]] ?? []).map((one) => (one.id === row ? { ...one, ...defined } : one))
      )
    )
  }

  const toggleRow = (kind: RowKind, row: string, enabled: boolean) => patchRow(kind, row, { enabled })

  async function moveRow(kind: RowKind, row: string, position: number) {
    if (kind === RowKind.RowPathParams) return
    edit((d) => {
      const rows = [...(d[listOf[kind]] ?? [])]
      const at = rows.findIndex((one) => one.id === row)
      if (at < 0) return d
      const [moved] = rows.splice(at, 1)
      rows.splice(Math.min(Math.max(position, 0), rows.length), 0, moved)
      return withList(d, kind, rows)
    })
  }

  async function pickBodyFile(): Promise<string> {
    const call = Drafts.PickBodyFile(t('files.bodyFile'), t('files.allFiles'))
    return (await callOrToast(call, 'files.pickFailed')) ?? ''
  }

  /** A pasted command replaces the request; the draft keeps its id and its environment. */
  async function pasteCommand(text: string): Promise<Reading | null> {
    const pasted = await callOrToast(Drafts.Paste(text), 'request.editFailed')
    if (!pasted) return null
    const d = current.value
    if (pasted.draft && d) {
      apply(followUrl({ ...pasted.draft, id: d.id, environmentId: d.environmentId }))
      target.filled?.()
    }
    return pasted.reading
  }

  async function copyAs(format: Format): Promise<Copy | null> {
    const d = current.value
    if (!d) return null
    return (await callOrToast(Drafts.CopyAs(d, format), 'response.copyFailed')) ?? null
  }

  async function obtainAuth() {
    const d = current.value
    if (!d) return
    await callOrToast(Drafts.ObtainAuth(d), 'request.editFailed')
    await refreshPreview()
  }

  async function forgetAuth() {
    const d = current.value
    if (!d) return
    await callOrToast(Drafts.ForgetAuth(d), 'request.editFailed')
    await refreshPreview()
  }

  const loading = ref(false)
  const pendingSendId = ref<string | null>(null)
  const sentIds = ref<string[]>([])

  async function send(): Promise<void> {
    const d = current.value
    if (!d) return
    loading.value = true
    let sent: string
    try {
      sent = await Drafts.Send(d)
    } catch (error) {
      failSend()
      throw error
    }
    target.sent?.()
    claim(sent)
    markStep('send')
    if (loading.value) pendingSendId.value = sent
  }

  function claim(id: string) {
    sentIds.value = [id, ...sentIds.value].slice(0, 20)
    const early = takeAnswer(id)
    if (!early) return
    if ('failed' in early) failSend()
    else void finishSend(early.record)
  }

  async function cancel() {
    const id = pendingSendId.value
    failSend()
    if (id) await callOrToast(History.Cancel(id), 'request.sendFailed')
  }

  function failSend() {
    pendingSendId.value = null
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
    const levels = await callOrToast(ScriptsService.Chain(id), 'request.readFailed')
    if (levels === undefined || target.scriptsLevel() !== id) return
    scriptsFor.value = id
    scripts.value = own
    chain.value = levels ?? []
  }

  async function loadScripts() {
    const id = target.scriptsLevel()
    if (!id) return
    const own = await callOrToast(ScriptsService.Scripts(id), 'request.readFailed')
    if (own !== undefined) await readChain(id, own)
  }

  async function saveScripts(pre: string, post: string, off: { pre: boolean; post: boolean }) {
    const id = target.scriptsLevel()
    if (!id) return
    const written = pre.trim() || post.trim()
      ? { pre, post, preOff: off.pre && !!pre.trim(), postOff: off.post && !!post.trim() }
      : null
    const saved = await callOrToast(ScriptsService.SaveScripts(id, written), 'request.editFailed')
    if (saved !== undefined) await readChain(id, saved)
  }

  const captures = computed<Capture[]>(() => scripts.value?.captures ?? [])

  async function saveCaptures(list: Capture[]) {
    const id = target.scriptsLevel()
    if (!id) return
    const saved = await callOrToast(ScriptsService.SaveCaptures(id, list), 'request.editFailed')
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
    current: draft,
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
    inherited,
    missing,
    enabledParamsCount,
    enabledHeadersCount,
    hasBody,
    load,
    abandon,
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
    pasteCommand,
    loading,
    pendingSendId,
    sentIds,
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
