import { computed } from 'vue'
import { INDENT_UNIT } from '../lib/layout'
import { useSetting } from './useSettings'

const units: Record<string, string> = INDENT_UNIT

/** What the code editors are set to: the whitespace a Tab puts in, and the setting Go formats by. */
export function useIndent() {
  const indent = useSetting('indent')
  const unit = computed(() => units[indent.value] ?? INDENT_UNIT.two)

  // Inserted through the field's own command so the caret's undo stack keeps this the way it keeps
  // typing, and the field's own input event carries the change to whoever is listening.
  function insert() {
    document.execCommand('insertText', false, unit.value)
  }

  return { indent, unit, insert }
}
