import { nextTick, ref, shallowRef, type ComponentPublicInstance } from 'vue'

/** Composable for managing inline text input fields (renaming, creation) with key bindings and focus handling. */
export function useInlineName<T>(write: (target: T, name: string) => Promise<void>) {
  const target = shallowRef<T | null>(null)
  const draft = ref('')
  const invalid = ref(false)
  const input = ref<HTMLInputElement | null>(null)

  /** Template ref callback to bind the input element. */
  function setInput(el: Element | ComponentPublicInstance | null): void {
    input.value = (el as HTMLInputElement | null) ?? null
  }

  /** Opens the inline edit input for the given target and selects the initial text. */
  function open(at: T, initial: string): void {
    target.value = at
    draft.value = initial
    invalid.value = false

    nextTick(() => {
      requestAnimationFrame(() => input.value?.focus())
      input.value?.select()
    })
  }

  /** Validates and commits the entered name, invoking the write callback on success. */
  async function commit(): Promise<void> {
    const at = target.value
    if (at === null) return

    const name = draft.value.trim()
    if (!name) {
      invalid.value = true
      input.value?.focus()
      return
    }

    target.value = null
    invalid.value = false
    await write(at, name)
  }

  /** Cancels the edit operation and resets target state. */
  function cancel(): void {
    target.value = null
    invalid.value = false
  }

  /** Keydown handler for inline inputs handling Enter (commit) and Escape (cancel). */
  function onKeydown(e: KeyboardEvent): void {
    e.stopPropagation()
    if (e.key === 'Enter') {
      e.preventDefault()
      void commit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancel()
    }
  }

  return { target, draft, invalid, input, setInput, open, commit, cancel, onKeydown }
}