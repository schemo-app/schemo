import { ref, shallowRef, watch } from 'vue'
import { tryOnScopeDispose, useMemoize } from '@vueuse/core'
import { Inspector } from '../../bindings/schemo/internal/request'
import {
  NodeKind,
  type Diagnosis,
  type DocKind,
  type NodeView,
  type Suggestion,
  type TypeFormat,
} from '../../bindings/schemo/internal/domain'

/** Whether a node holds other nodes rather than a value. */
export const isContainer = (view: NodeView): boolean =>
  view.kind === NodeKind.NodeObject ||
  view.kind === NodeKind.NodeArray ||
  view.kind === NodeKind.NodeElement

/** The body the inspector reads. */
interface Body {
  text: string
  contentType: string
}

/** The selected position of a response and what Go says about it. The body goes to Go once, on first use. */
export function useInspection(body: () => Body) {
  const node = shallowRef<NodeView | null>(null)
  // Null until the body is offered; false when it is not a document.
  const readable = ref<boolean | null>(null)
  // What the body was read as; types are for JSON only.
  const kind = ref<DocKind | ''>('')

  const diagnosis = shallowRef<Diagnosis | null>(null)

  let revision = 0
  let selection = 0
  let diagnosing = 0
  const openDocument = useMemoize((text: string, contentType: string) =>
    Inspector.Open(text, contentType)
  )

  async function open(): Promise<string> {
    const { text, contentType } = body()
    const current = revision
    const answer = await openDocument(text, contentType)
    if (current !== revision) return ''
    readable.value = answer.id !== ''
    kind.value = answer.kind
    return answer.id
  }

  // A handle Go has forgotten is offered again, once.
  async function ask<T>(call: (id: string) => Promise<T>): Promise<T | null> {
    const current = revision
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const id = await open()
        if (current !== revision) return null
        const answer = id ? await call(id) : null
        return current === revision ? answer : null
      } catch {
        if (current !== revision) return null
        openDocument.clear()
      }
    }
    return null
  }

  async function select(read: () => Promise<NodeView | null>): Promise<boolean> {
    const current = ++selection
    const version = revision
    const view = await read()
    if (current !== selection || version !== revision) return false
    node.value = view
    return view !== null
  }

  /** Selects what is at an offset in UTF-16 units. */
  async function pickOffset(offset: number): Promise<boolean> {
    return select(() => ask((id) => Inspector.At(id, offset)))
  }

  /** Selects what an address names: JSONPath, XPath, cell reference. */
  async function pickPath(path: string): Promise<boolean> {
    return select(() => ask((id) => Inspector.Node(id, path)))
  }

  /** Offers the body without selecting, to learn whether it is a document. */
  async function ensure(): Promise<void> {
    await ask(async () => null)
  }

  /** What a copy of the node holds. */
  function valueOf(view: NodeView): Promise<string | null> {
    return ask((id) => Inspector.Text(id, view.path))
  }

  /** Collection requests a value belongs in, address filled in. */
  async function suggestFor(view: NodeView): Promise<Suggestion[]> {
    return (await ask((id) => Inspector.Suggest(id, view.path))) ?? []
  }

  /** The node as a request body, without server-owned members when `bare`. */
  function bodyOf(view: NodeView, bare: boolean): Promise<string | null> {
    return ask((id) => Inspector.Body(id, view.path, bare))
  }

  async function typesOf(view: NodeView, format: TypeFormat): Promise<string | null> {
    return ask((id) => Inspector.Types(id, view.path, format))
  }

  /** Reads an error response (4xx/5xx only) for what is wrong with the request. */
  async function diagnose(status: number): Promise<void> {
    const myTurn = ++diagnosing
    diagnosis.value = null
    if (status < 400) return
    const found = await ask((id) => Inspector.Diagnose(id, status))
    // Drop the answer if the body changed meanwhile.
    if (myTurn === diagnosing && found && found.format) diagnosis.value = found
  }

  /** Forgets the selection and the handle. */
  function reset(): void {
    node.value = null
    diagnosis.value = null
    revision++
    selection++
    diagnosing++
    readable.value = null
    kind.value = ''
    openDocument.clear()
  }

  watch(() => [body().text, body().contentType], reset, { flush: 'sync' })
  tryOnScopeDispose(reset)

  return { node, diagnosis, readable, kind, diagnose, pickOffset, pickPath, ensure, valueOf, bodyOf, suggestFor, typesOf, reset }
}

export type Inspection = ReturnType<typeof useInspection>
