import { onBeforeUnmount, onMounted, shallowRef, watch, type Ref, type WatchSource } from 'vue'
import type { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { useSetting } from './useSettings'

/** The reader settings every editor obeys. */
export interface ReaderLook {
  numbers: boolean
  wrap: boolean
}

export interface EditorViewOptions {
  host: Readonly<Ref<HTMLElement | null>>
  text: () => string
  build: (doc: string, look: ReaderLook) => EditorState
  /** Extra sources that rebuild the state, besides the reader settings. */
  rebuildOn?: WatchSource[]
  /** Runs after the view is created and after every rebuild. */
  ready?: () => void
}

/**
 * A CodeMirror view mounted in `host`. A settings change rebuilds the state from the current text:
 * history and folds go, which is fine for something that is not an edit.
 */
export function useEditorView({ host, text, build, rebuildOn = [], ready }: EditorViewOptions) {
  const view = shallowRef<EditorView | null>(null)
  const wrap = useSetting('wrapLines')
  const numbers = useSetting('lineNumbers')

  const stateOf = (doc: string) => build(doc, { numbers: numbers.value, wrap: wrap.value })

  function rebuild(doc?: string) {
    const current = view.value
    if (!current) return
    current.setState(stateOf(doc ?? current.state.doc.toString()))
    ready?.()
  }

  onMounted(() => {
    if (!host.value) return
    view.value = new EditorView({ state: stateOf(text()), parent: host.value })
    ready?.()
  })

  onBeforeUnmount(() => {
    view.value?.destroy()
    view.value = null
  })

  watch([wrap, numbers, ...rebuildOn], () => rebuild())

  return { view, rebuild }
}
