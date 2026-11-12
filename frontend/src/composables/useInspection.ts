import { shallowRef, watch } from 'vue'
import { tryOnScopeDispose, useMemoize } from '@vueuse/core'
import { Inspector } from '../../bindings/schemo/internal/request'
import {
  NodeKind,
  type Diagnosis,
  type NodeView,
  type Suggestion,
  type TypeFormat,
} from '../../bindings/schemo/internal/domain'
import type { InlineReadings } from '../lib/reading'

/** Whether a node holds other nodes rather than a value. */
export const isContainer = (view: NodeView): boolean =>
  view.kind === NodeKind.NodeObject ||
  view.kind === NodeKind.NodeArray ||
  view.kind === NodeKind.NodeElement

/** Reads and inspects a JSON response on first use. */
export function useInspection(body: () => string) {
  const node = shallowRef<NodeView | null>(null)
  const diagnosis = shallowRef<Diagnosis | null>(null)
  const inlineReadings = shallowRef<InlineReadings>({})

  let revision = 0
  let readingsRevision = -1
  let selection = 0
  let diagnosing = 0
  const openDocument = useMemoize((text: string) =>
    Inspector.Open(text, 'application/json')
  )

  // A handle Go has forgotten is offered again, once.
  async function ask<T>(call: (id: string) => Promise<T>): Promise<T | null> {
    const current = revision
    const text = body()
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const { id } = await openDocument(text)
        if (current !== revision || !id) return null
        const answer = await call(id)
        return current === revision ? answer : null
      } catch {
        if (current !== revision) return null
        openDocument.clear()
      }
    }
    return null
  }

  /** Selects the node at a JSONPath. */
  async function pickPath(path: string): Promise<boolean> {
    const current = ++selection
    const view = await ask((id) => Inspector.Node(id, path))
    if (current !== selection) return false
    node.value = view
    return view !== null
  }

  async function loadInlineReadings(): Promise<void> {
    const current = revision
    if (readingsRevision === current) return
    readingsRevision = current
    const readings = await ask(Inspector.InlineReadings)
    if (current !== revision) return
    if (readings) inlineReadings.value = readings
    else readingsRevision = -1
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

  function clearSelection(): void {
    node.value = null
    selection++
  }

  /** Forgets the readings, selection and handle. */
  function reset(): void {
    clearSelection()
    inlineReadings.value = {}
    diagnosis.value = null
    revision++
    diagnosing++
    openDocument.clear()
  }

  watch(body, reset, { flush: 'sync' })
  tryOnScopeDispose(reset)

  return { node, diagnosis, inlineReadings, loadInlineReadings, diagnose, pickPath, clearSelection, valueOf, bodyOf, suggestFor, typesOf, reset }
}

export type Inspection = ReturnType<typeof useInspection>
