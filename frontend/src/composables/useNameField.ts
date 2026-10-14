import { nextTick, ref, type Ref } from 'vue'
import { useSheetNotice } from './useSheetNotice'
import { describeFailure } from '../i18n'

interface NameFieldOptions {
  input: Readonly<Ref<{ select(): void } | null>>
  /** The thing being renamed, or null where nothing is — the globals have no name to change. */
  target: () => { id: string; name: string } | null
  /** The write: what makes a name a refusal — empty, long, worn by another — is Go's to say. */
  save: (id: string, wanted: string) => Promise<unknown>
}

/**
 * The name field of a detail pane: Enter and leaving the field both commit, Escape puts the stored
 * name back, and a refused one keeps the field — with what was typed still in it — so it can be fixed.
 */
export function useNameField({ input, target, save }: NameFieldOptions) {
  const { setNotice, clearNotice } = useSheetNotice()
  const name = ref('')
  const invalid = ref(false)
  let shown = ''

  /** Shows a name in the field: moving between rows is not an edit. */
  function show(text: string) {
    shown = text
    name.value = text
    invalid.value = false
  }

  function refuse(message: string) {
    invalid.value = true
    setNotice(message)
    void nextTick(() => input.value?.select())
  }

  async function commit() {
    const one = target()
    if (!one) return
    const wanted = name.value.trim()

    if (wanted === one.name) {
      invalid.value = false
      clearNotice()
      return
    }
    invalid.value = false
    clearNotice()
    try {
      await save(one.id, wanted)
    } catch (error) {
      refuse(describeFailure(error))
    }
  }

  function revert() {
    show(shown)
    clearNotice()
  }

  return { name, invalid, show, commit, revert }
}
